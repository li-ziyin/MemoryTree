// Package usagelog persists per-entry return signals from the MCP layer:
// every entry ID that a memory_search, memory_brief, or memory_get actually
// returned is appended as one JSON line under the disposable .index/
// directory. The gardener aggregates the log into its analysis package
// ("this entry has not been returned in months"), so cold entries can be
// proposed for deprecation. Counts are derived data only: they never enter
// the memory files themselves.
package usagelog

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"time"
)

const (
	dirName  = ".index"
	fileName = "entry-usage.jsonl"
)

// maxBytes bounds the usage log; a larger file rotates to .1 (replacing any
// previous rotation). A var so tests can shrink it.
var maxBytes int64 = 1 << 20

// entry is one recorded return.
type entry struct {
	TS     string `json:"ts"`
	ID     string `json:"id"`
	Source string `json:"source"`
}

// Usage aggregates one entry's return history.
type Usage struct {
	Count int    `json:"count"`
	Last  string `json:"last"`
}

func path(root string) string {
	return filepath.Join(root, dirName, fileName)
}

// Record appends one line per id, all stamped with the same now. Failures
// are the caller's to ignore: a logging problem must never fail the read.
func Record(root string, ids []string, source string, now time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	p := path(root)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	if info, err := os.Stat(p); err == nil && info.Size()+int64(len(ids)*160) >= maxBytes {
		_ = os.Remove(p + ".1")
		if err := os.Rename(p, p+".1"); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	ts := now.UTC().Format(time.RFC3339)
	for _, id := range ids {
		line, err := json.Marshal(entry{TS: ts, ID: id, Source: source})
		if err != nil {
			f.Close()
			return err
		}
		if _, err := w.Write(append(line, '\n')); err != nil {
			f.Close()
			return err
		}
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Aggregate returns per-entry return counts and last-return times. A
// missing or unreadable log yields no usage and no error.
func Aggregate(root string) (map[string]Usage, error) {
	agg := map[string]Usage{}
	for _, p := range []string{path(root) + ".1", path(root)} { // oldest first
		if err := readInto(p, agg); err != nil {
			return nil, err
		}
	}
	return agg, nil
}

func readInto(p string, agg map[string]Usage) error {
	f, err := os.Open(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var e entry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil || e.ID == "" {
			continue // tolerate hand edits and truncation
		}
		u := agg[e.ID]
		u.Count++
		if e.TS > u.Last {
			u.Last = e.TS
		}
		agg[e.ID] = u
	}
	return scanner.Err()
}

// Heat ranking parameters. alpha bounds how much usage can matter: the
// multiplier saturates at 1+alpha*ln(1+count), so even a thousand returns
// lift a hit by roughly 2x and relevance still dominates. The half-life
// decays stale popularity so a burst of use years ago fades out.
const (
	heatAlpha    = 0.15
	heatHalfLife = 90 * 24 * time.Hour
)

// Heat returns the entry's ranking multiplier in [1, 1+alpha*ln(1+count)]:
// frequently returned entries get a mild, time-decayed boost; never-returned
// ones get exactly 1 — cold is the absence of a boost, never a penalty.
// Malformed timestamps count as no decay information and yield the full
// boost for the recorded count.
func (u Usage) Heat(now time.Time) float64 {
	if u.Count <= 0 {
		return 1
	}
	decay := 1.0
	if last, err := time.Parse(time.RFC3339, u.Last); err == nil {
		age := now.Sub(last)
		if age < 0 {
			age = 0
		}
		decay = math.Exp2(-age.Hours() / heatHalfLife.Hours())
	}
	return 1 + heatAlpha*math.Log1p(float64(u.Count))*decay
}

// Heats aggregates the log and returns each entry's current ranking
// multiplier. Entries never returned are absent from the map; callers treat
// a missing entry as 1.
func Heats(root string, now time.Time) (map[string]float64, error) {
	agg, err := Aggregate(root)
	if err != nil {
		return nil, err
	}
	heats := make(map[string]float64, len(agg))
	for id, u := range agg {
		heats[id] = u.Heat(now)
	}
	return heats, nil
}
