package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Returned entries are recorded once per session per tool; misses and
// repeated calls do not inflate the log.
func TestUsageLogRecordsReturns(t *testing.T) {
	srv, root := newWriteTestServer(t)
	usageFile := filepath.Join(root, ".index", "entry-usage.jsonl")

	env, terr := srv.memorySearch(context.Background(), json.RawMessage(`{"query":"用户画像"}`))
	if terr != nil {
		t.Fatalf("search: %v", terr)
	}
	if env.Meta.Total < 1 {
		t.Fatalf("search found nothing; fixture changed?")
	}
	if _, terr := srv.memoryGet(context.Background(), json.RawMessage(`{"id":"`+sseEntryID+`"}`)); terr != nil {
		t.Fatalf("get: %v", terr)
	}
	// Same search again: still one line for this session.
	if _, terr := srv.memorySearch(context.Background(), json.RawMessage(`{"query":"用户画像"}`)); terr != nil {
		t.Fatalf("search retry: %v", terr)
	}

	data, err := os.ReadFile(usageFile)
	if err != nil {
		t.Fatalf("usage log missing: %v", err)
	}
	log := string(data)
	if got := strings.Count(log, `"source":"search"`); got != 1 {
		t.Errorf("search lines = %d, want 1 (session dedupe):\n%s", got, log)
	}
	if got := strings.Count(log, `"source":"get"`); got != 1 {
		t.Errorf("get lines = %d, want 1:\n%s", got, log)
	}
	if !strings.Contains(log, userEntryID) || !strings.Contains(log, sseEntryID) {
		t.Errorf("returned entry IDs missing:\n%s", log)
	}

	// brief records only the entries it actually included.
	before := log
	if _, terr := srv.memoryBrief(context.Background(), json.RawMessage(`{"task":"用户画像与 SSE"}`)); terr != nil {
		t.Fatalf("brief: %v", terr)
	}
	data, err = os.ReadFile(usageFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("usage log emptied by brief")
	}
	if !strings.Contains(string(data), `"source":"brief"`) {
		t.Errorf("brief lines missing after call:\n%s", data)
	}
	if string(data) == before && !strings.Contains(before, `"source":"brief"`) {
		t.Errorf("brief recorded nothing:\n%s", data)
	}
}
