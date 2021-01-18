// Copyright 2020 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package checker checks file header licenses adhere to a project rule file.
//
// checker is a fairly simple wrapper for the github.com/google/licensecheck
// library.
package checker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"../match"
	"github.com/google/licensecheck"
)

// Check loads the config file with the filename ConfigFileName in dir, and then
// scans all files for license correctness. Any license violations are returned
// as an error.
func Check(dir string) error {
	root, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("Failed to get absolute working directory: %w", err)
	}

	cfgs, err := loadConfigs(root)
	if err != nil {
		return fmt.Errorf("Failed to load config file: %w", err)
	}

	for _, cfg := range cfgs {
		errs := runConfig(cfg, root)
		if len(errs) > 0 {
			msg := strings.Builder{}
			fmt.Fprintf(&msg, "%d errors:\n", len(errs))
			for _, err := range errs {
				fmt.Fprintf(&msg, "* %v\n", err)
			}
			return fmt.Errorf("%v", msg.String())
		}
	}

	fmt.Printf("No license issues found\n")

	return nil
}

var (
	// ConfigFileName is the configuration filename to load.
	ConfigFileName = "license-checker.cfg"
)

// Configs is a slice of Config.
type Configs []Config

// Config is used to parse the JSON configuration file at ConfigFileName.
type Config struct {
	// Paths holds a number of JSON objects that contain either a "includes" or
	// "excludes" key to an array of path patterns.
	// Each path pattern is considered in turn to either include or exclude the
	// file path for license scanning. Pattern use forward-slashes '/' for
	// directory separators, and may use the following wildcards:
	//  ?  - matches any single non-separator character
	//  *  - matches any sequence of non-separator characters
	//  ** - matches any sequence of characters including separators
	//
	// Rules are processed in the order in which they are declared, with later
	// rules taking precedence over earlier rules.
	//
	// All files are included before the first rule is evaluated.
	//
	// Example:
	//
	// {
	//   "paths": [
	// 	  { "exclude": [ "out/*", "build/*" ] },
	// 	  { "include": [ "out/foo.txt" ] }
	//   ],
	// }
	Paths searchRules

	// Licenses is an array of permitted license types.
	// Licenses found that are not in this list will cause an error.
	//
	// Example:
	//
	// {
	//   "licenses": [ "Apache-2.0-Header", "MIT" ]
	// }
	Licenses []string
}

// rule is a search path predicate.
// root is the project relative path.
// cond is the value to return if the rule doesn't either include or exclude.
type rule func(path string, cond bool) bool

// searchRules is a ordered list of search rules.
// searchRules is its own type as it has to perform custom JSON unmarshalling.
type searchRules []rule

// UnmarshalJSON unmarshals the array of rules in the form:
// { "include": [ ... ] } or { "exclude": [ ... ] }
func (l *searchRules) UnmarshalJSON(body []byte) error {
	type parsed struct {
		Include []string
		Exclude []string
	}

	p := []parsed{}
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&p); err != nil {
		return err
	}

	*l = searchRules{}
	for _, rule := range p {
		rule := rule
		switch {
		case len(rule.Include) > 0 && len(rule.Exclude) > 0:
			return fmt.Errorf("Rule cannot contain both include and exclude")
		case len(rule.Include) > 0:
			tests := make([]match.Test, len(rule.Include))
			for i, pattern := range rule.Include {
				test, err := match.New(pattern)
				if err != nil {
					return err
				}
				tests[i] = test
			}
			*l = append(*l, func(path string, cond bool) bool {
				for _, test := range tests {
					if test(path) {
						return true
					}
				}
				return cond
			})
		case len(rule.Exclude) > 0:
			tests := make([]match.Test, len(rule.Exclude))
			for i, pattern := range rule.Exclude {
				test, err := match.New(pattern)
				if err != nil {
					return err
				}
				tests[i] = test
			}
			*l = append(*l, func(path string, cond bool) bool {
				for _, test := range tests {
					if test(path) {
						return false
					}
				}
				return cond
			})
		}
	}
	return nil
}

// shouldExamine returns true if the file at absPath should be scanned.
func (c Config) shouldExamine(root, absPath string) bool {
	root = filepath.ToSlash(root)       // Canonicalize
	absPath = filepath.ToSlash(absPath) // Canonicalize
	relPath, err := filepath.Rel(root, absPath)
	if err != nil {
		return false
	}

	res := true
	for _, rule := range c.Paths {
		res = rule(relPath, res)
	}

	return res
}

// allowsLicense returns true if the license type with the given name is
// permitted.
func (c Config) allowsLicense(name string) bool {
	for _, l := range c.Licenses {
		if l == name {
			return true
		}
	}
	return false
}

// runConfig gathers the source files listed in the config, scans them for their
// licenses, and returns an error if any license violations are found.
func runConfig(cfg Config, root string) []error {
	files, err := gatherFiles(root, cfg)
	if err != nil {
		return []error{fmt.Errorf("Failed to gather files: %w", err)}
	}

	fmt.Printf("Scanning %d files...\n", len(files))

	var wg sync.WaitGroup
	errs := make([]error, len(files))
	for i, file := range files {
		i, file := i, file
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = examine(root, file, cfg)
		}()
	}
	wg.Wait()

	return removeNilErrs(errs)
}

// loadConfigs loads a config file at root.
func loadConfigs(root string) (Configs, error) {
	path := filepath.Join(root, ConfigFileName)
	cfgBody, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	d := json.NewDecoder(bytes.NewReader(cfgBody))
	cfgs := Configs{}
	if strings.HasPrefix(strings.TrimLeft(string(cfgBody), " \n\t"), "{") {
		// Single config
		cfg := Config{}
		if err := d.Decode(&cfg); err != nil {
			return nil, err
		}
		cfgs = append(cfgs, cfg)
	} else {
		// Multiple configs
		if err := d.Decode(&cfgs); err != nil {
			return nil, err
		}
	}
	return cfgs, nil
}

// gatherFiles walks all files and subdirectories from root, returning those
// that Config.shouldExamine() returns true for.
func gatherFiles(root string, cfg Config) ([]string, error) {
	files := []string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			rel = path
		}

		switch rel {
		case ".git":
			return filepath.SkipDir
		case ConfigFileName:
			return nil
		}

		if !cfg.shouldExamine(root, path) {
			return nil
		}

		if !info.IsDir() {
			files = append(files, rel)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// examine checks the file at path for any license violations.
// examine will return an error if no license is found, or the license is not
// accepted by the config.
func examine(root, path string, cfg Config) error {
	body, err := ioutil.ReadFile(filepath.Join(root, path))
	if err != nil {
		return fmt.Errorf("Failed to read file '%v': %w", path, err)
	}
	cov := licensecheck.Scan(body)
	if len(cov.Match) == 0 {
		return fmt.Errorf("%v has no license", path)
	}
	for _, match := range cov.Match {
		if !cfg.allowsLicense(match.ID) {
			return fmt.Errorf("%v uses unsupported license '%v'", path, match.ID)
		}
	}
	return nil
}

// removeNilErrs returns a new slice with all the non-nil errors of errs
// removed.
func removeNilErrs(errs []error) []error {
	var out []error
	for _, err := range errs {
		if err != nil {
			out = append(out, err)
		}
	}
	return out
}
