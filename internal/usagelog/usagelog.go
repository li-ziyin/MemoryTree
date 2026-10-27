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
