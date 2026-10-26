package mcpserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/li-ziyin/memorytree/internal/index"
)

const (
	userEntryID = "019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b05"
	sseEntryID  = "019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b06"
	unknownID   = "019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1bff"
)

func newTestServer(t testing.TB) *Server {
	return newTestServerWith(t, nil)
}

func newTestServerWith(t testing.TB, setup func(root string)) *Server {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(filepath.Join("..", "..", "spec", "examples"))); err != nil {
		t.Fatalf("copy example: %v", err)
	}
	if setup != nil {
		setup(root)
	}
	if _, err := index.Build(root, index.BuildOptions{}); err != nil {
		t.Fatalf("build index: %v", err)
	}
	ix, err := index.Open(root)
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	t.Cleanup(func() { ix.Close() })
	srv, err := New(ix, Options{Version: "test"})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return srv
}

func serveTest(t *testing.T, srv *Server, requests ...string) []map[string]any {
	t.Helper()
	input := strings.Join(requests, "\n")
	if input != "" {
		input += "\n"
	}
	var out bytes.Buffer
	if err := srv.Serve(strings.NewReader(input), &out); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	dec := json.NewDecoder(&out)
	var responses []map[string]any
	for {
		var resp map[string]any
		err := dec.Decode(&resp)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("decode response %q: %v", out.String(), err)
		}
		responses = append(responses, resp)
	}
	return responses
}

func rawRequest(t *testing.T, id int, method string, params any) string {
	t.Helper()
	msg := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		msg["params"] = params
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	return string(data)
}

func callTool(t *testing.T, srv *Server, name string, args any) map[string]any {
	t.Helper()
	responses := serveTest(t, srv, rawRequest(t, 2, "tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	}))
	if len(responses) != 1 {
		t.Fatalf("tools/call returned %d responses", len(responses))
	}
	return mustMap(t, responses[0]["result"], "result")
}

func mustMap(t *testing.T, value any, name string) map[string]any {
	t.Helper()
	m, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s = %#v, want object", name, value)
	}
	return m
}

func mustSlice(t *testing.T, value any, name string) []any {
	t.Helper()
	s, ok := value.([]any)
	if !ok {
		t.Fatalf("%s = %#v, want array", name, value)
	}
	return s
}

func toolError(t *testing.T, result map[string]any) *ToolError {
	t.Helper()
	env := mustMap(t, result["structuredContent"], "structuredContent")
	raw := mustMap(t, env["error"], "error")
	return &ToolError{
		Code:       raw["code"].(string),
		Message:    raw["message"].(string),
		Suggestion: stringOrEmpty(raw["suggestion"]),
	}
}

func stringOrEmpty(value any) string {
	s, _ := value.(string)
	return s
}

func TestInitializeAndToolsList(t *testing.T) {
	srv := newTestServer(t)
	responses := serveTest(t, srv,
		rawRequest(t, 1, "initialize", map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "test", "version": "1"},
		}),
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		rawRequest(t, 2, "tools/list", nil),
	)
	if len(responses) != 2 {
		t.Fatalf("got %d responses, want 2 (notification must be silent)", len(responses))
	}

	initResult := mustMap(t, responses[0]["result"], "initialize result")
	if initResult["protocolVersion"] != "2025-06-18" {
		t.Errorf("protocolVersion = %v", initResult["protocolVersion"])
	}
	serverInfo := mustMap(t, initResult["serverInfo"], "serverInfo")
	if serverInfo["name"] != "memoryd" || serverInfo["version"] != "test" {
		t.Errorf("serverInfo = %+v", serverInfo)
	}
	toolsCapability := mustMap(t, mustMap(t, initResult["capabilities"], "capabilities")["tools"], "tools capability")
	if toolsCapability["listChanged"] != false {
		t.Errorf("tools capability = %+v", toolsCapability)
	}

	listResult := mustMap(t, responses[1]["result"], "tools/list result")
	tools := mustSlice(t, listResult["tools"], "tools")
	if len(tools) != 6 {
		t.Fatalf("got %d tools, want 6", len(tools))
	}
	want := map[string]bool{
		"memory_brief": false, "memory_search": false, "memory_get": false, "memory_tree": false,
		"memory_add": false, "memory_supersede": false,
	}
	readOnly := map[string]bool{
		"memory_brief": true, "memory_search": true, "memory_get": true, "memory_tree": true,
		"memory_add": false, "memory_supersede": false,
	}
	for _, raw := range tools {
		tool := mustMap(t, raw, "tool")
		name := tool["name"].(string)
		if _, ok := want[name]; !ok {
			t.Errorf("unexpected tool %q", name)
			continue
		}
		want[name] = true
		schema := mustMap(t, tool["inputSchema"], "inputSchema")
		if schema["type"] != "object" || schema["additionalProperties"] != false {
			t.Errorf("%s inputSchema = %+v", name, schema)
		}
		annotations := mustMap(t, tool["annotations"], "annotations")
		if annotations["readOnlyHint"] != readOnly[name] || annotations["openWorldHint"] != false {
			t.Errorf("%s annotations = %+v", annotations, schema)
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("missing tool %q", name)
		}
	}
}

func TestInitializeNegotiatesKnownVersion(t *testing.T) {
	srv := newTestServer(t)
	responses := serveTest(t, srv, rawRequest(t, 1, "initialize", map[string]any{
		"protocolVersion": "2099-01-01",
	}))
	result := mustMap(t, responses[0]["result"], "initialize result")
	if result["protocolVersion"] != latestProtocolVersion {
		t.Errorf("protocolVersion = %v, want %s", result["protocolVersion"], latestProtocolVersion)
	}
}

func TestMemorySearchTool(t *testing.T) {
	srv := newTestServer(t)

	result := callTool(t, srv, "memory_search", map[string]any{"query": "SSE"})
	if result["isError"] == true {
		t.Fatalf("memory_search failed: %+v", toolError(t, result))
	}
	env := mustMap(t, result["structuredContent"], "structuredContent")
	data := mustSlice(t, env["data"], "data")
	if len(data) != 1 {
		t.Fatalf("search returned %d items, want 1", len(data))
	}
	item := mustMap(t, data[0], "hit")
	if item["id"] != sseEntryID || item["trust"] != trustUntrusted {
		t.Errorf("hit = %+v", item)
	}
	meta := mustMap(t, env["meta"], "meta")
	if meta["total"] != float64(1) || meta["index_generation"] != float64(1) {
		t.Errorf("meta = %+v", meta)
	}
	if meta["tokens_estimated"].(float64) <= 0 {
		t.Errorf("tokens_estimated = %v", meta["tokens_estimated"])
	}

	short := callTool(t, srv, "memory_search", map[string]any{"query": "解码"})
	shortData := mustSlice(t, mustMap(t, short["structuredContent"], "structuredContent")["data"], "data")
	if len(shortData) != 1 || mustMap(t, shortData[0], "hit")["id"] != sseEntryID {
		t.Errorf("short query result = %+v", shortData)
	}

	users := callTool(t, srv, "memory_search", map[string]any{"types": []string{"user"}})
	userData := mustSlice(t, mustMap(t, users["structuredContent"], "structuredContent")["data"], "data")
	if len(userData) != 1 || mustMap(t, userData[0], "hit")["id"] != userEntryID {
		t.Errorf("user filter result = %+v", userData)
	}

	bad := callTool(t, srv, "memory_search", map[string]any{"statuses": []string{"bogus"}})
	if bad["isError"] != true {
		t.Fatalf("invalid status did not fail")
	}
	if terr := toolError(t, bad); terr.Code != "INVALID_ARGUMENTS" {
		t.Errorf("error code = %s", terr.Code)
	}
}

func TestMemoryGetTool(t *testing.T) {
	srv := newTestServer(t)

	result := callTool(t, srv, "memory_get", map[string]any{"id": sseEntryID})
	if result["isError"] == true {
		t.Fatalf("memory_get failed: %+v", toolError(t, result))
	}
	env := mustMap(t, result["structuredContent"], "structuredContent")
	entry := mustMap(t, env["data"], "entry")
	if entry["id"] != sseEntryID || entry["trust"] != trustUntrusted {
		t.Errorf("entry = %+v", entry)
	}
	if !strings.Contains(entry["body"].(string), "按原始字节解码") {
		t.Errorf("body = %q", entry["body"])
	}
	warnings := mustSlice(t, mustMap(t, env["meta"], "meta")["warnings"], "warnings")
	if len(warnings) == 0 {
		t.Errorf("private entry should produce a warning")
	}

	missing := callTool(t, srv, "memory_get", map[string]any{"id": unknownID})
	if missing["isError"] != true {
		t.Fatalf("unknown ID did not fail")
	}
	if terr := toolError(t, missing); terr.Code != "MEMORY_NOT_FOUND" {
		t.Errorf("error code = %s", terr.Code)
	}

	invalid := callTool(t, srv, "memory_get", map[string]any{"id": "not-a-uuid"})
	if invalid["isError"] != true {
		t.Fatalf("invalid ID did not fail")
	}
	if terr := toolError(t, invalid); terr.Code != "INVALID_ARGUMENTS" {
		t.Errorf("error code = %s", terr.Code)
	}
}

func TestMemoryGetExcerpt(t *testing.T) {
	const longID = "019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b09"
	marker := "紫色风铃"
	longBody := strings.Repeat("这是填充内容，不含关键词。", 80) + "\n\n" +
		marker + "只在中间出现一次。\n\n" + strings.Repeat("尾部填充内容。", 80)

	srv := newTestServerWith(t, func(root string) {
		content := fmt.Sprintf(`---
schema: memorytree.entry/v1
id: %s
node: 019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b02
type: user
status: active
sensitivity: private
title: long-note
summary: 测试用长条目。
tags_delta: []
source:
  agent: test
  kind: manual
supersedes: []
superseded_by: null
related: []
created_at: "2026-10-21T00:00:00Z"
updated_at: "2026-10-21T00:00:00Z"
---

%s
`, longID, longBody)
		path := filepath.Join(root, "global", "memories", "2026-10-21-long-note.md")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write long entry: %v", err)
		}
	})

	result := callTool(t, srv, "memory_get", map[string]any{
		"id":        longID,
		"query":     marker,
		"max_chars": 300,
	})
	if result["isError"] == true {
		t.Fatalf("memory_get excerpt failed: %+v", toolError(t, result))
	}
	env := mustMap(t, result["structuredContent"], "structuredContent")
	entry := mustMap(t, env["data"], "entry")
	body := entry["body"].(string)
	if !strings.Contains(body, marker) {
		t.Errorf("excerpt misses the marker: %q", body)
	}
	if entry["excerpted"] != true {
		t.Errorf("excerpted = %v, want true", entry["excerpted"])
	}
	if full := entry["full_chars"].(float64); full <= 300 {
		t.Errorf("full_chars = %v, want > 300", full)
	}
	if n := utf8.RuneCountInString(body); n > 300 {
		t.Errorf("excerpt is %d runes, want <= 300", n)
	}
	if !warningsContain(mustSlice(t, mustMap(t, env["meta"], "meta")["warnings"], "warnings"), "excerpt") {
		t.Errorf("missing excerpt warning")
	}

	full := callTool(t, srv, "memory_get", map[string]any{"id": longID})
	fullEntry := mustMap(t, mustMap(t, full["structuredContent"], "structuredContent")["data"], "entry")
	if body := fullEntry["body"].(string); !strings.Contains(body, marker) || utf8.RuneCountInString(body) <= 300 {
		t.Errorf("full body was not returned")
	}
	if _, ok := fullEntry["excerpted"]; ok {
		t.Errorf("full get should not be marked excerpted")
	}

	fallback := callTool(t, srv, "memory_get", map[string]any{
		"id":        longID,
		"query":     "不存在的词",
		"max_chars": 300,
	})
	if fallback["isError"] == true {
		t.Fatalf("memory_get fallback failed: %+v", toolError(t, fallback))
	}
	fallbackEnv := mustMap(t, fallback["structuredContent"], "structuredContent")
	fallbackEntry := mustMap(t, fallbackEnv["data"], "entry")
	if strings.Contains(fallbackEntry["body"].(string), marker) {
		t.Errorf("fallback excerpt should come from the beginning")
	}
	if !warningsContain(mustSlice(t, mustMap(t, fallbackEnv["meta"], "meta")["warnings"], "warnings"), "not found") {
		t.Errorf("missing query-not-found warning")
	}

	bad := callTool(t, srv, "memory_get", map[string]any{
		"id":        longID,
		"query":     marker,
		"max_chars": 10,
	})
	if bad["isError"] != true {
		t.Fatalf("short max_chars did not fail")
	}
	if terr := toolError(t, bad); terr.Code != "INVALID_ARGUMENTS" {
		t.Errorf("error code = %s", terr.Code)
	}
}

func TestMemoryTreeTool(t *testing.T) {
	srv := newTestServer(t)

	result := callTool(t, srv, "memory_tree", map[string]any{})
	if result["isError"] == true {
		t.Fatalf("memory_tree failed: %+v", toolError(t, result))
	}
	env := mustMap(t, result["structuredContent"], "structuredContent")
	root := mustMap(t, env["data"], "root")
	if root["path"] != "." || root["kind"] != "root" {
		t.Errorf("root = %+v", root)
	}
	children := mustSlice(t, root["children"], "children")
	if len(children) != 2 {
		t.Fatalf("root has %d children, want 2", len(children))
	}
	work := findTreeChild(t, children, "work")
	workChildren := mustSlice(t, work["children"], "work children")
	if len(workChildren) != 1 || mustMap(t, workChildren[0], "agentflow")["path"] != "work/agentflow" {
		t.Errorf("work children = %+v", workChildren)
	}
	agentflow := mustMap(t, workChildren[0], "agentflow")
	if agentflow["entry_count"] != float64(1) || agentflow["active_entry_count"] != float64(1) {
		t.Errorf("agentflow counts = %+v", agentflow)
	}
	if agentflow["trust"] != trustUntrusted {
		t.Errorf("trust = %v", agentflow["trust"])
	}
	if mustMap(t, env["meta"], "meta")["total"] != float64(4) {
		t.Errorf("meta total = %v", mustMap(t, env["meta"], "meta")["total"])
	}

	shallow := callTool(t, srv, "memory_tree", map[string]any{"depth": 1})
	shallowRoot := mustMap(t, mustMap(t, shallow["structuredContent"], "structuredContent")["data"], "root")
	if len(mustSlice(t, shallowRoot["children"], "children")) != 2 {
		t.Errorf("depth 1 should include direct children")
	}
	for _, raw := range mustSlice(t, shallowRoot["children"], "children") {
		child := mustMap(t, raw, "child")
		if len(mustSlice(t, child["children"], "grandchildren")) != 0 {
			t.Errorf("depth 1 included grandchildren under %v", child["path"])
		}
	}

	subtree := callTool(t, srv, "memory_tree", map[string]any{"node": "work/agentflow"})
	subtreeRoot := mustMap(t, mustMap(t, subtree["structuredContent"], "structuredContent")["data"], "subtree")
	if subtreeRoot["path"] != "work/agentflow" {
		t.Errorf("subtree root = %+v", subtreeRoot)
	}

	miss := callTool(t, srv, "memory_tree", map[string]any{"node": "nope"})
	if miss["isError"] != true {
		t.Fatalf("unknown node did not fail")
	}
	if terr := toolError(t, miss); terr.Code != "NODE_NOT_FOUND" {
		t.Errorf("error code = %s", terr.Code)
	}
}

func findTreeChild(t *testing.T, children []any, slug string) map[string]any {
	t.Helper()
	for _, raw := range children {
		child := mustMap(t, raw, "child")
		if child["slug"] == slug {
			return child
		}
	}
	t.Fatalf("child with slug %q not found", slug)
	return nil
}

func TestMemoryBriefTool(t *testing.T) {
	srv := newTestServer(t)

	result := callTool(t, srv, "memory_brief", map[string]any{
		"task":    "SSE 解码",
		"project": "work/agentflow",
	})
	if result["isError"] == true {
		t.Fatalf("memory_brief failed: %+v", toolError(t, result))
	}
	env := mustMap(t, result["structuredContent"], "structuredContent")
	data := mustMap(t, env["data"], "brief")
	project := mustMap(t, data["project"], "project")
	if project["path"] != "work/agentflow" || project["trust"] != trustUntrusted {
		t.Errorf("project = %+v", project)
	}
	user := mustSlice(t, data["user"], "user")
	updates := mustSlice(t, data["project_updates"], "project_updates")
	related := mustSlice(t, data["related"], "related")
	pending := mustSlice(t, data["pending"], "pending")
	if len(user) != 1 || mustMap(t, user[0], "user item")["id"] != userEntryID {
		t.Errorf("user section = %+v", user)
	}
	if len(updates) != 1 || mustMap(t, updates[0], "update item")["id"] != sseEntryID {
		t.Errorf("project_updates = %+v", updates)
	}
	if len(related) != 0 || len(pending) != 0 {
		t.Errorf("related/pending should be deduplicated: %+v / %+v", related, pending)
	}

	seen := map[string]bool{}
	for _, group := range [][]any{user, updates, related, pending} {
		for _, raw := range group {
			id := mustMap(t, raw, "item")["id"].(string)
			if seen[id] {
				t.Errorf("duplicate ID %s in brief", id)
			}
			seen[id] = true
		}
	}

	meta := mustMap(t, env["meta"], "meta")
	warnings := mustSlice(t, meta["warnings"], "warnings")
	if !warningsContain(warnings, "private-sensitivity") {
		t.Errorf("expected private warning, got %v", warnings)
	}
	if meta["tokens_estimated"].(float64) > 1200 {
		t.Errorf("brief exceeded the budget: %v", meta["tokens_estimated"])
	}
	if meta["total"] != float64(2) {
		t.Errorf("meta total = %v", meta["total"])
	}

	noProject := callTool(t, srv, "memory_brief", map[string]any{"task": "SSE"})
	noProjectEnv := mustMap(t, noProject["structuredContent"], "structuredContent")
	noProjectWarnings := mustSlice(t, mustMap(t, noProjectEnv["meta"], "meta")["warnings"], "warnings")
	if !warningsContain(noProjectWarnings, "no project was specified") {
		t.Errorf("expected no-project warning, got %v", noProjectWarnings)
	}

	badProject := callTool(t, srv, "memory_brief", map[string]any{"task": "SSE", "project": "nope"})
	if badProject["isError"] != true {
		t.Fatalf("unknown project did not fail")
	}
	if terr := toolError(t, badProject); terr.Code != "NODE_NOT_FOUND" {
		t.Errorf("error code = %s", terr.Code)
	}

	emptyTask := callTool(t, srv, "memory_brief", map[string]any{"task": "  "})
	if emptyTask["isError"] != true {
		t.Fatalf("empty task did not fail")
	}
	if terr := toolError(t, emptyTask); terr.Code != "INVALID_ARGUMENTS" {
		t.Errorf("error code = %s", terr.Code)
	}
}

func warningsContain(warnings []any, want string) bool {
	for _, raw := range warnings {
		if strings.Contains(raw.(string), want) {
			return true
		}
	}
	return false
}

func TestProtocolErrors(t *testing.T) {
	srv := newTestServer(t)
	responses := serveTest(t, srv,
		rawRequest(t, 1, "nope", nil),
		`not json`,
		rawRequest(t, 2, "tools/call", map[string]any{"name": "nope", "arguments": map[string]any{}}),
		`[]`,
	)
	if len(responses) != 4 {
		t.Fatalf("got %d responses, want 4", len(responses))
	}
	wantCodes := []float64{-32601, -32700, -32602, -32600}
	for i, want := range wantCodes {
		errObj := mustMap(t, responses[i]["error"], "error")
		if errObj["code"] != want {
			t.Errorf("response %d code = %v, want %v", i, errObj["code"], want)
		}
	}
	if responses[1]["id"] != nil {
		t.Errorf("parse error id = %v, want null", responses[1]["id"])
	}
}
