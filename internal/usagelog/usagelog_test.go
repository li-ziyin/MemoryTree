package usagelog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var idA = "019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b06"
var idB = "019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b07"

func TestRecordAndAggregate(t *testing.T) {
	root := t.TempDir()
	day := func(d int) time.Time { return time.Date(2026, 10, d, 9, 0, 0, 0, time.UTC) }
	if err := Record(root, []string{idA}, "search", day(1)); err != nil {
		t.Fatal(err)
	}
	if err := Record(root, []string{idA, idB}, "brief", day(2)); err != nil {
		t.Fatal(err)
	}
	agg, err := Aggregate(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(agg) != 2 {
		t.Fatalf("aggregated %d entries, want 2", len(agg))
	}
	if agg[idA].Count != 2 || agg[idA].Last != "2026-10-02T09:00:00Z" {
		t.Errorf("idA usage = %+v", agg[idA])
	}
	if agg[idB].Count != 1 {
		t.Errorf("idB usage = %+v", agg[idB])
	}
}

func TestAggregateWithoutLogIsEmpty(t *testing.T) {
	agg, err := Aggregate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(agg) != 0 {
		t.Fatalf("want empty, got %+v", agg)
	}
}

func TestRecordRotatesPastMaxBytes(t *testing.T) {
	old := maxBytes
	maxBytes = 200
	defer func() { maxBytes = old }()
	root := t.TempDir()
	for i := 0; i < 8; i++ {
		if err := Record(root, []string{idA}, "search", time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(path(root) + ".1"); err != nil {
		t.Fatalf("no rotation file: %v", err)
	}
	// Rotation is bounded discarding: the current file never grows past
	// the cap plus one record's headroom, and some history survives in .1.
	info, err := os.Stat(path(root))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > maxBytes+256 {
		t.Errorf("current log = %d bytes, past the cap", info.Size())
	}
	agg, err := Aggregate(root)
	if err != nil {
		t.Fatal(err)
	}
	if agg[idA].Count < 1 {
		t.Errorf("count = %d, want at least the last window", agg[idA].Count)
	}
}

// A rotated .1 file is aggregated together with the current one.
func TestAggregateReadsRotatedFile(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(path(root)), 0o700); err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	var lines strings.Builder
	for i := 0; i < 2; i++ {
		lines.WriteString(`{"ts":"` + day.AddDate(0, 0, i).UTC().Format(time.RFC3339) + `","id":"` + idB + `","source":"search"}` + "\n")
	}
	if err := os.WriteFile(path(root)+".1", []byte(lines.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Record(root, []string{idB}, "get", day.AddDate(0, 0, 2)); err != nil {
		t.Fatal(err)
	}
	agg, err := Aggregate(root)
	if err != nil {
		t.Fatal(err)
	}
	if agg[idB].Count != 3 {
		t.Errorf("count = %d, want 3 across both files", agg[idB].Count)
	}
	if agg[idB].Last != day.AddDate(0, 0, 2).UTC().Format(time.RFC3339) {
		t.Errorf("last = %s", agg[idB].Last)
	}
}

func TestRecordToleratesHandEdits(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(path(root)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path(root), []byte("not json\n\n{\"id\":\"\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Record(root, []string{idA}, "get", time.Now()); err != nil {
		t.Fatal(err)
	}
	agg, err := Aggregate(root)
	if err != nil {
		t.Fatal(err)
	}
	if agg[idA].Count != 1 {
		t.Errorf("count = %d, want 1", agg[idA].Count)
	}
	if strings.Contains(func() string {
		raw, _ := os.ReadFile(path(root))
		return string(raw)
	}(), "not json") == false {
		t.Errorf("tolerated lines were rewritten")
	}
}

func TestHeatNeverPenalizesCold(t *testing.T) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	if got := (Usage{}).Heat(now); got != 1 {
		t.Errorf("empty usage heat = %v, want 1", got)
	}
	if got := (Usage{Count: 0, Last: now.Format(time.RFC3339)}).Heat(now); got != 1 {
		t.Errorf("zero-count heat = %v, want 1", got)
	}
}

func TestHeatSaturates(t *testing.T) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	recent := Usage{Count: 10, Last: now.Add(-time.Hour).Format(time.RFC3339)}
	huge := Usage{Count: 1_000_000, Last: now.Add(-time.Hour).Format(time.RFC3339)}
	r, h := recent.Heat(now), huge.Heat(now)
	if r <= 1 || h <= r {
		t.Fatalf("heat must grow with returns: recent=%v huge=%v", r, h)
	}
	// The multiplier is bounded by 1+alpha*ln(1+count); even a million
	// returns stay under 3.1 so relevance keeps dominating.
	if h > 3.1 {
		t.Errorf("million-return heat = %v, want <= 3.1", h)
	}
}

func TestHeatDecaysWithHalfLife(t *testing.T) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	fresh := Usage{Count: 40, Last: now.Format(time.RFC3339)}
	stale := Usage{Count: 40, Last: now.Add(-90 * 24 * time.Hour).Format(time.RFC3339)}
	veryStale := Usage{Count: 40, Last: now.Add(-360 * 24 * time.Hour).Format(time.RFC3339)}
	f, s, v := fresh.Heat(now), stale.Heat(now), veryStale.Heat(now)
	if s >= f {
		t.Fatalf("90-day-old heat %v must be below fresh %v", s, f)
	}
	// 90 days is one half-life of the log term.
	want := 1 + (f-1)/2
	if s < want-1e-9 || s > want+1e-9 {
		t.Errorf("90-day heat = %v, want half of fresh excess %v", s, want)
	}
	if v >= s {
		t.Errorf("year-old heat %v must be below 90-day %v", v, s)
	}
	if v <= 1 {
		t.Errorf("year-old heat %v must stay above 1", v)
	}
}

func TestHeatToleratesBadTimestamps(t *testing.T) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	if got := (Usage{Count: 5, Last: "yesterday"}).Heat(now); got <= 1 {
		t.Errorf("malformed last = %v, want full boost for count", got)
	}
	if got := (Usage{Count: 5, Last: now.Add(time.Hour).Format(time.RFC3339)}).Heat(now); got <= 1 {
		t.Errorf("future last clamped to full boost, got %v", got)
	}
}

func TestHeatsAggregatesLog(t *testing.T) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	root := t.TempDir()
	if err := Record(root, []string{idA, idB, idA}, "search", now); err != nil {
		t.Fatal(err)
	}
	heats, err := Heats(root, now)
	if err != nil {
		t.Fatal(err)
	}
	if heats[idA] <= heats[idB] {
		t.Errorf("double-returned a (%v) must outrank single b (%v)", heats[idA], heats[idB])
	}
	if _, ok := heats["missing"]; ok {
		t.Errorf("never-returned entries must be absent")
	}
}
