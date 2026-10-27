package depcheck

import (
	"strings"
	"testing"
)

// The shipped allowlist must exactly match the live go.mod; a failure here
// means the dependency graph changed and THIRD_PARTY_NOTICES.md needs a
// conscious review before the change can land.
func TestGoModMatchesAllowlist(t *testing.T) {
	gomod, notices, err := readRepoFiles()
	if err != nil {
		t.Fatal(err)
	}
	mods := parseRequireLines(gomod)
	if len(mods) == 0 {
		t.Fatal("no require entries found in go.mod — parser broken?")
	}

	for path, tier := range mods {
		if _, ok := allowed[path]; !ok {
			t.Errorf("go.mod module %s is not on the allowlist; verify its license, add it to allowed in depcheck.go, and record it in THIRD_PARTY_NOTICES.md", path)
			continue
		}
		if !strings.Contains(notices, path) {
			t.Errorf("module %s (%s) is missing from THIRD_PARTY_NOTICES.md", path, tier)
		}
	}
	for path := range allowed {
		if _, ok := mods[path]; !ok {
			t.Errorf("allowlisted module %s is no longer in go.mod; remove it from depcheck.go and THIRD_PARTY_NOTICES.md", path)
		}
	}
}

func TestParseRequireLines(t *testing.T) {
	gomod := `module example.com/m

go 1.25

require (
	golang.org/x/text v0.29.0
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

require modernc.org/sqlite v1.59.0
`
	mods := parseRequireLines(gomod)
	want := map[string]string{
		"golang.org/x/text":  "BSD-3-Clause",
		"gopkg.in/yaml.v3":   "indirect",
		"modernc.org/sqlite": "BSD-3-Clause",
	}
	if len(mods) != len(want) {
		t.Fatalf("mods = %v, want %d entries", mods, len(want))
	}
	for path := range want {
		if _, ok := mods[path]; !ok {
			t.Errorf("module %s missing from %v", path, mods)
		}
	}
	// The indirect marker is preserved so callers can distinguish tiers.
	if mods["gopkg.in/yaml.v3"] != "indirect" {
		t.Errorf("indirect marker lost: %v", mods)
	}
}
