package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Every search that returns nothing for a non-empty query is logged once
// per session; hits and empty queries are not.
func TestSearchMissesAreLoggedOnce(t *testing.T) {
	srv, root := newWriteTestServer(t)
	missFile := filepath.Join(root, ".index", "search-misses.jsonl")

	call := func(q string) {
		t.Helper()
		env, terr := srv.memorySearch(context.Background(), json.RawMessage(`{"query":`+strconv.Quote(q)+`}`))
		if terr != nil {
			t.Fatalf("search %q: %v", q, terr)
		}
		if env == nil {
			t.Fatalf("search %q: no envelope", q)
		}
	}

	call("绝对不存在的词条xyz")
	call("绝对不存在的词条xyz") // same miss again: still one line this session
	call("另一个不存在词条abc")

	data, err := os.ReadFile(missFile)
	if err != nil {
		t.Fatalf("miss log missing: %v", err)
	}
	if got := strings.Count(string(data), "绝对不存在的词条xyz"); got != 1 {
		t.Errorf("duplicate miss entries (%d) for one session:\n%s", got, data)
	}
	if !strings.Contains(string(data), "另一个不存在词条abc") {
		t.Errorf("second miss missing:\n%s", data)
	}

	// A hit and an empty query never log.
	before := string(data)
	call("用户画像") // exists in spec/examples
	call("")
	data, err = os.ReadFile(missFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != before {
		t.Errorf("hit or empty query was logged:\n%s", data)
	}
}
