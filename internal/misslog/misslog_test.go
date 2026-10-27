package misslog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func missPath(root string) string {
	return filepath.Join(root, ".index", "search-misses.jsonl")
}

func TestRecordAppendsJSONLines(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	if err := Record(root, "harmonyos arkts", now); err != nil {
		t.Fatal(err)
	}
	if err := Record(root, "docker 迁移", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(missPath(root))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2: %q", len(lines), data)
	}
	if !strings.Contains(lines[0], `"query":"harmonyos arkts"`) || !strings.Contains(lines[0], "2026-10-27T09:00:00Z") {
		t.Errorf("first line = %s", lines[0])
	}
	if !strings.Contains(lines[1], "docker 迁移") {
		t.Errorf("second line = %s", lines[1])
	}
}

func TestRecordRotatesWhenLarge(t *testing.T) {
	root := t.TempDir()
	old := maxBytes
	maxBytes = 32
	defer func() { maxBytes = old }()

	if err := Record(root, "first query that is long", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := Record(root, "second query also long", time.Now()); err != nil {
		t.Fatal(err)
	}
	rotated, err := os.ReadFile(missPath(root) + ".1")
	if err != nil {
		t.Fatalf("rotated file missing: %v", err)
	}
	if !strings.Contains(string(rotated), "first query") {
		t.Errorf("rotated = %s", rotated)
	}
	current, err := os.ReadFile(missPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(current), "second query") || strings.Contains(string(current), "first query") {
		t.Errorf("current = %s", current)
	}
}

func TestTopAggregatesCounts(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	for i, q := range []string{"alpha", "alpha", "alpha", "beta", "beta", "gamma"} {
		if err := Record(root, q, now.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	top, err := Top(root, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(top) != 3 {
		t.Fatalf("top = %+v, want 3 entries", top)
	}
	if top[0].Query != "alpha" || top[0].Count != 3 {
		t.Errorf("top[0] = %+v, want alpha x3", top[0])
	}
	if top[1].Query != "beta" || top[1].Count != 2 {
		t.Errorf("top[1] = %+v, want beta x2", top[1])
	}
	if top[0].Last != "2026-10-27T09:02:00Z" {
		t.Errorf("last = %s, want the newest timestamp", top[0].Last)
	}

	only, err := Top(root, 1)
	if err != nil || len(only) != 1 || only[0].Query != "alpha" {
		t.Fatalf("Top(1) = %+v (%v)", only, err)
	}
}

func TestTopWithoutLogReturnsNothing(t *testing.T) {
	top, err := Top(t.TempDir(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(top) != 0 {
		t.Fatalf("top = %+v, want none", top)
	}
}
