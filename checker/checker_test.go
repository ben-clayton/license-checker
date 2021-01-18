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

package checker_test

import (
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	checker "."
)

var testcases = filepath.Join(sourceDirectory(), "testcases")

func TestGood(t *testing.T) {
	for _, test := range []string{
		"good-basic",
		"good-filter",
	} {
		if err := checker.Check(filepath.Join(testcases, test)); err != nil {
			t.Errorf("Unexpected checker failure for '%v': %v", test, err)
		}
	}
}

func TestBad(t *testing.T) {
	for _, test := range []struct {
		dir    string
		expect string
	}{
		{"bad-no-config", "Failed to load config file"},
		{"bad-missing-license", "src/missing-license.cpp has no license"},
	} {
		err := checker.Check(filepath.Join(testcases, test.dir))
		if !strings.Contains(err.Error(), test.expect) {
			t.Errorf("Unexpected checker failure for '%v': %v", test.dir, err)
		}
	}
}

// sourceDirectory returns the path to the directory that holds this .go file
func sourceDirectory() string {
	_, filename, _, ok := runtime.Caller(1)
	if !ok {
		panic("runtime.Caller(1) failed")
	}
	return path.Dir(filename)
}
