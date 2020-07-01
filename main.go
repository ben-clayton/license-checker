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

// license-checker is a command line tool that checks project licenses adhere to
// a project rule file.
//
// license-checker is a fairly simple command line wrapper for the
// github.com/google/licensecheck library.
//
// license-checker looks for a config file at <project-root>/license-checker.cfg
// See the Config struct for the config parameters.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/licensecheck"
)

const (
	// Project-relative path for the configuration file.
	licensecheckCfgPath = "license-checker.cfg"
)

// Config is used to parse the JSON configuration file at licensecheckCfgPath.
type Config struct {
	// Paths holds a number of JSON objects that contain either a "includes" or
	// "excludes" key to an array of path patterns.
	// Each path pattern is processed my the path.Match() function
	// (https://golang.org/pkg/path/#Match) to either include or exclude the
	// file path for license scanning.
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

var (
	wd = flag.String("dir", cwd(), "Project root directory to scan")
)

// rule is a search path predicate.
// root is the project root directory.
// absPath is the '/' directory-delimited absolute path to the file.
// cond is the value to return if the rule doesn't either include or exclude.
type rule func(root, absPath string, cond bool) bool

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
		if len(rule.Include) > 0 {
			*l = append(*l, func(root, absPath string, cond bool) bool {
				for _, pattern := range rule.Include {
					if ok, _ := path.Match(path.Join(root, pattern), absPath); ok {
						return true
					}
				}
				return cond
			})
		}
		if len(rule.Exclude) > 0 {
			*l = append(*l, func(root, absPath string, cond bool) bool {
				for _, pattern := range rule.Exclude {
					if ok, _ := path.Match(path.Join(root, pattern), absPath); ok {
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

	res := true
	for _, rule := range c.Paths {
		res = rule(root, absPath, res)
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

// cwd returns the current working directory, or an empty string if it cannot
// be determined.
func cwd() string {
	wd, _ := os.Getwd()
	return wd
}

// main is the entry point for the program.
func main() {
	flag.Parse()
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "%v", err)
		os.Exit(1)
	}
}

// run loads the config, gathers the source files, scans them for their
// licenses, and returns an error if any license violations are found.
func run() error {
	root, err := filepath.Abs(*wd)
	if err != nil {
		return fmt.Errorf("Failed to get absolute working directory: %w", err)
	}

	cfg, err := loadConfig(root)
	if err != nil {
		return fmt.Errorf("Failed to load config file: %w", err)
	}

	files, err := gatherFiles(root, cfg)
	if err != nil {
		return fmt.Errorf("Failed to gather files: %w", err)
	}

	fmt.Printf("Scanning %d files...\n", len(files))

	wg := sync.WaitGroup{}
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

	errs = removeNilErrs(errs)

	if len(errs) > 0 {
		msg := strings.Builder{}
		fmt.Fprintf(&msg, "%d errors:\n", len(errs))
		for _, err := range errs {
			fmt.Fprintf(&msg, "* %v\n", err)
		}
		return fmt.Errorf("%v", msg.String())
	} else {
		fmt.Printf("No license issues found\n")
	}

	return nil
}

// loadConfig attempts to load the config file, returning the config if found,
// otherwise a default-initialized config.
func loadConfig(root string) (Config, error) {
	path := filepath.Join(root, licensecheckCfgPath)
	cfgBody, err := ioutil.ReadFile(path)
	if err != nil {
		return Config{}, nil
	}
	cfg := Config{}
	if err := json.NewDecoder(bytes.NewReader(cfgBody)).Decode(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
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
		case licensecheckCfgPath:
			return nil
		}

		if !cfg.shouldExamine(root, path) {
			if info.IsDir() {
				return filepath.SkipDir
			}
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
	cov, ok := licensecheck.Cover(body, licensecheck.Options{})
	if !ok {
		return fmt.Errorf("%v has no recognisable licenses", path)
	}
	for _, match := range cov.Match {
		if !cfg.allowsLicense(match.Name) {
			return fmt.Errorf("%v uses unsupported license '%v'", path, match.Name)
		}
	}
	_ = cov
	return nil
}

// removeNilErrs returns the slice errs with all the nil errors removed.
func removeNilErrs(errs []error) []error {
	c := 0
	for _, err := range errs {
		if err != nil {
			errs[c] = err
			c++
		}
	}
	return errs[:c]
}
