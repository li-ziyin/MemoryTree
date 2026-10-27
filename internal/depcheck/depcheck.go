// Package depcheck keeps the dependency graph in lockstep with the license
// allowlist and THIRD_PARTY_NOTICES.md. It exists so that adding a module
// can never silently change what gets linked into the distributed binary:
// the test in this package fails until the new module's license is verified
// and recorded.
package depcheck

import (
	"os"
	"path/filepath"
	"strings"
)

// allowed maps every module path that may appear in go.mod to its license
// label. The label is documentation for reviewers; the enforcement is the
// presence of the key. Keep it in sync with THIRD_PARTY_NOTICES.md — the
// test fails if either side drifts.
var allowed = map[string]string{
	"golang.org/x/text":                "BSD-3-Clause",
	"gopkg.in/yaml.v3":                 "MIT / Apache-2.0 (dual)",
	"modernc.org/sqlite":               "BSD-3-Clause",
	"github.com/dustin/go-humanize":    "MIT",
	"github.com/google/uuid":           "BSD-3-Clause",
	"github.com/mattn/go-isatty":       "MIT",
	"github.com/ncruces/go-strftime":   "MIT",
	"github.com/remyoudompheng/bigfft": "MIT",
	"golang.org/x/sys":                 "BSD-3-Clause",
	"modernc.org/libc":                 "BSD-3-Clause",
	"modernc.org/mathutil":             "BSD-3-Clause",
	"modernc.org/memory":               "Apache-2.0",
}

// parseRequireLines extracts the module paths from the require blocks of a
// go.mod file. The value is the license tier marker "indirect" for indirect
// requires and "direct" otherwise; callers that only need the module set
// can ignore it. Comments and replace/exclude/retract sections are skipped.
func parseRequireLines(gomod string) map[string]string {
	mods := map[string]string{}
	lines := strings.Split(gomod, "\n")
	inRequire := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "require (":
			inRequire = true
		case inRequire && trimmed == ")":
			inRequire = false
		case inRequire:
			if path, indirect := requireEntry(trimmed); path != "" {
				mods[path] = indirect
			}
		case strings.HasPrefix(trimmed, "require "):
			if path, indirect := requireEntry(strings.TrimPrefix(trimmed, "require ")); path != "" {
				mods[path] = indirect
			}
		}
	}
	return mods
}

func requireEntry(entry string) (path string, tier string) {
	indirect := strings.Contains(entry, "// indirect")
	entry = strings.TrimSpace(strings.SplitN(entry, "//", 2)[0])
	fields := strings.Fields(entry)
	if len(fields) < 2 || !strings.Contains(fields[0], ".") && !strings.Contains(fields[0], "/") {
		return "", ""
	}
	if indirect {
		return fields[0], "indirect"
	}
	return fields[0], "direct"
}

// readRepoFiles loads go.mod and THIRD_PARTY_NOTICES.md from the module
// root, located by walking up from the working directory.
func readRepoFiles() (gomod, notices string, err error) {
	root, err := findModuleRoot()
	if err != nil {
		return "", "", err
	}
	gomodBytes, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", "", err
	}
	noticesBytes, err := os.ReadFile(filepath.Join(root, "THIRD_PARTY_NOTICES.md"))
	if err != nil {
		return "", "", err
	}
	return string(gomodBytes), string(noticesBytes), nil
}

func findModuleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}
