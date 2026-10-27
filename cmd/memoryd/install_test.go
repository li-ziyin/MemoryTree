package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setTempHome(t testing.TB) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func TestRunInstallAndRemove(t *testing.T) {
	home := setTempHome(t)
	data := t.TempDir()
	exe := filepath.Join(t.TempDir(), "memoryd")

	var stdout, stderr bytes.Buffer
	code := run([]string{"install", "--client", "qoder", "--exe", exe, data}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("install code = %d, stderr: %s", code, stderr.String())
	}
	cfg := filepath.Join(home, ".qoder", "mcp.json")
	if _, err := os.Stat(cfg); err != nil {
		t.Fatalf("config not created: %v", err)
	}
	if !strings.Contains(stdout.String(), cfg) {
		t.Fatalf("stdout should name the config path: %s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"install", "--client", "qoder", "--remove"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("remove code = %d, stderr: %s", code, stderr.String())
	}
	raw, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "memorytree") {
		t.Fatalf("entry not removed: %s", raw)
	}
}

func TestRunInstallUsageErrors(t *testing.T) {
	setTempHome(t)
	cases := [][]string{
		{"install"},
		{"install", "--client", "qoder"},                  // missing data dir
		{"install", "--client", "qoder", "a", "b"},        // extra args
		{"install", "--client", "nope", "data"},           // unknown client
		{"install", "--client", "qoder", "--remove", "x"}, // remove takes no data dir
	}
	for _, args := range cases {
		var stdout, stderr bytes.Buffer
		if code := run(args, strings.NewReader(""), &stdout, &stderr); code != 2 {
			t.Errorf("%v: code = %d, want 2; stderr: %s", args, code, stderr.String())
		}
	}
}
