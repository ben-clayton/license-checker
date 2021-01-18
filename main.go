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
	"flag"
	"fmt"
	"os"

	"./checker"
)

var (
	wd = flag.String("dir", cwd(), "Project root directory to scan")
)

// cwd returns the current working directory, or an empty string if it cannot
// be determined.
func cwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
}

// main is the entry point for the program.
func main() {
	flag.Parse()
	if err := checker.Check(*wd); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
