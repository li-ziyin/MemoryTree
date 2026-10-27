// Package misslog persists retrieval misses from the MCP layer: every
// memory_search that returns nothing for a non-empty query is appended as
// one JSON line under the disposable .index/ directory. The gardener
// aggregates the log into its analysis package ("a class of queries keeps
// missing"), so repeated misses can drive tree restructuring.
package misslog

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	dirName  = ".index"
	fileName = "search-misses.jsonl"
)

// maxBytes bounds the miss log; a larger file rotates to .1 (replacing any
// previous rotation). A var so tests can shrink it.
var maxBytes int64 = 1 << 20

// entry is one recorded miss.
type entry struct {
	TS    string `json:"ts"`
	Query string `json:"query"`
}

// Miss aggregates one query's miss history.
type Miss struct {
	Query string `json:"query"`
	Count int    `json:"count"`
	Last  string `json:"last"`
}

func path(root string) string {
	return filepath.Join(root, dirName, fileName)
}

// Record appends one miss. Failures are the caller's to ignore: a logging
// problem must never fail the search itself.
func Record(root, query string, now time.Time) error {
	p := path(root)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	if info, err := os.Stat(p); err == nil && info.Size() >= maxBytes {
		_ = os.Remove(p + ".1")
		if err := os.Rename(p, p+".1"); err != nil {
			return err
		}
	}
	line, err := json.Marshal(entry{TS: now.UTC().Format(time.RFC3339), Query: query})
	if err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Top returns the n most-missed queries with their counts and last miss
// time. Missing or unreadable logs yield no misses and no error.
func Top(root string, n int) ([]Miss, error) {
	agg := map[string]*Miss{}
	for _, p := range []string{path(root) + ".1", path(root)} { // oldest first
		if err := readInto(p, agg); err != nil {
			return nil, err
		}
	}
	out := make([]Miss, 0, len(agg))
	for _, m := range agg {
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Query < out[j].Query
	})
	if len(out) > n {
		out = out[:n]
	}
	return out, nil
}

func readInto(p string, agg map[string]*Miss) error {
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
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil || e.Query == "" {
			continue // tolerate hand edits and truncation
		}
		m, ok := agg[e.Query]
		if !ok {
			m = &Miss{Query: e.Query}
			agg[e.Query] = m
		}
		m.Count++
		if e.TS > m.Last {
			m.Last = e.TS
		}
	}
	return scanner.Err()
}
