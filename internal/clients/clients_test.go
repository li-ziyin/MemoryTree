package clients

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	exe  = `C:\tools\memoryd.exe`
	data = `D:\memdata`
)

func readJSON(t testing.TB, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return m
}

func dig(t testing.TB, m map[string]any, keys ...string) map[string]any {
	t.Helper()
	for _, k := range keys {
		child, ok := m[k].(map[string]any)
		if !ok {
			t.Fatalf("key path %v: %q is not an object: %v", keys, k, m[k])
		}
		m = child
	}
	return m
}

func TestInstallZcode(t *testing.T) {
	home := t.TempDir()
	cfgDir := filepath.Join(home, ".zcode", "cli")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(cfgDir, "config.json")
	if err := os.WriteFile(cfg, []byte(`{"plugins":{"enabledPlugins":{"mimosa":true}}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	path, err := Install(home, "zcode", exe, data)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if path != cfg {
		t.Fatalf("path = %q, want %q", path, cfg)
	}
	m := readJSON(t, cfg)
	if _, ok := m["plugins"].(map[string]any); !ok {
		t.Fatal("existing plugins config lost")
	}
	entry := dig(t, m, "mcp", "servers", "memorytree")
	if entry["command"] != exe {
		t.Fatalf("command = %v", entry["command"])
	}
	args, ok := entry["args"].([]any)
	if !ok || len(args) != 2 || args[0] != "mcp" || args[1] != data {
		t.Fatalf("args = %v", entry["args"])
	}
	for k := range entry {
		if k != "command" && k != "args" {
			t.Fatalf("zcode schema is strict; unexpected key %q", k)
		}
	}

	// Idempotent second install must not duplicate or fail.
	if _, err := Install(home, "zcode", exe, data); err != nil {
		t.Fatalf("second install: %v", err)
	}
	if entry2 := dig(t, readJSON(t, cfg), "mcp", "servers", "memorytree"); entry2["command"] != exe {
		t.Fatal("second install changed the entry")
	}
}

func TestInstallZcodeRemovesCleanly(t *testing.T) {
	home := t.TempDir()
	if _, err := Install(home, "zcode", exe, data); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".zcode", "cli", "config.json")
	if err := os.WriteFile(path, []byte(`{"mcp":{"servers":{"memorytree":{"command":"x"},"other":{"command":"y"}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, removed, err := Remove(home, "zcode")
	if err != nil || !removed {
		t.Fatalf("remove: removed=%v err=%v", removed, err)
	}
	m := readJSON(t, path)
	servers := dig(t, m, "mcp", "servers")
	if _, ok := servers["memorytree"]; ok {
		t.Fatal("memorytree entry still present")
	}
	if _, ok := servers["other"]; !ok {
		t.Fatal("unrelated server lost")
	}
}

func TestInstallQoderCreatesFile(t *testing.T) {
	home := t.TempDir()
	path, err := Install(home, "qoder", exe, data)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	entry := dig(t, readJSON(t, path), "mcpServers", "memorytree")
	if entry["command"] != exe {
		t.Fatalf("command = %v", entry["command"])
	}
}

func TestInstallWorkbuddyShape(t *testing.T) {
	home := t.TempDir()
	path, err := Install(home, "workbuddy", exe, data)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	entry := dig(t, readJSON(t, path), "mcpServers", "memorytree")
	if entry["type"] != "stdio" {
		t.Fatalf("type = %v", entry["type"])
	}
	if entry["timeout"] != float64(60000) {
		t.Fatalf("timeout = %v", entry["timeout"])
	}
}

func TestInstallOpencodeArrayCommand(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "opencode.json")
	if err := os.WriteFile(cfg, []byte(`{"model":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	path, err := Install(home, "opencode", exe, data)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	m := readJSON(t, path)
	if m["model"] != "x" {
		t.Fatal("existing config lost")
	}
	entry := dig(t, m, "mcp", "memorytree")
	cmd, ok := entry["command"].([]any)
	if !ok || len(cmd) != 3 || cmd[0] != exe || cmd[1] != "mcp" || cmd[2] != data {
		t.Fatalf("command array = %v", entry["command"])
	}
	if entry["enabled"] != true {
		t.Fatalf("enabled = %v", entry["enabled"])
	}
}

func TestInstallOpencodeRejectsCommentedConfig(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "opencode.jsonc")
	if err := os.WriteFile(cfg, []byte("{\n // comment\n \"model\": \"x\"\n}"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Install(home, "opencode", exe, data)
	if err == nil {
		t.Fatal("commented config must not be rewritten blindly")
	}
	if !strings.Contains(err.Error(), "manual") {
		t.Fatalf("error must point at manual edit: %v", err)
	}
}

func TestInstallDeepseekAppendsPatch(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".dsh", "profiles", "desktop")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	patch := filepath.Join(dir, "cordis.patch.yml")
	existing := "# comment\n- id: ui-chat\n  name: \"@deepseek-ai/dsh-client-ui-chat\"\n  config:\n    transcriptView: standard\n"
	if err := os.WriteFile(patch, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}

	path, err := Install(home, "deepseek", exe, data)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(raw)
	if !strings.HasPrefix(content, "# comment") {
		t.Fatal("existing patch content must be preserved verbatim")
	}
	for _, want := range []string{"id: mcp-memorytree", "name: \"@deepseek-ai/dsh-mcp-client\"", "serverName: memorytree", "transport: stdio", "command: " + filepath.ToSlash(exe)} {
		if !strings.Contains(content, want) {
			t.Fatalf("patch missing %q:\n%s", want, content)
		}
	}

	// Second install is a no-op, not a duplicate.
	if _, err := Install(home, "deepseek", exe, data); err != nil {
		t.Fatalf("second install: %v", err)
	}
	raw2, _ := os.ReadFile(path)
	if strings.Count(string(raw2), "id: mcp-memorytree") != 1 {
		t.Fatal("second install duplicated the entry")
	}

	// Remove deletes only our entry.
	_, removed, err := Remove(home, "deepseek")
	if err != nil || !removed {
		t.Fatalf("remove: removed=%v err=%v", removed, err)
	}
	raw3, _ := os.ReadFile(path)
	if strings.Contains(string(raw3), "mcp-memorytree") {
		t.Fatal("entry not removed")
	}
	if !strings.Contains(string(raw3), "ui-chat") {
		t.Fatal("unrelated entry lost")
	}
}

func TestUnknownClientErrors(t *testing.T) {
	if _, err := Install(t.TempDir(), "nope", exe, data); err == nil {
		t.Fatal("unknown client must error")
	}
	if _, _, err := Remove(t.TempDir(), "nope"); err == nil {
		t.Fatal("unknown client must error")
	}
}

// The session-end capture hook merges into config.json idempotently:
// enabling configuration-file hooks, adding exactly one Stop entry, and
// removing cleanly while leaving foreign hooks alone.
func TestZCodeHookInstallAndRemove(t *testing.T) {
	home := t.TempDir()
	cfgDir := filepath.Join(home, ".zcode", "cli")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cfgDir, "config.json")
	foreign := `{"mcp":{"servers":{"other":{"command":"y"}}},"hooks":{"enabled":true,"events":{"Stop":[{"hooks":[{"type":"command","command":"notify-send done"}]}]}}}`
	if err := os.WriteFile(path, []byte(foreign), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := InstallHooks(home, "zcode", exe, data); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallHooks(home, "zcode", exe, data); err != nil {
		t.Fatal(err) // idempotent
	}
	m := readJSON(t, path)
	hooks := dig(t, m, "hooks")
	if hooks["enabled"] != true {
		t.Fatalf("hooks.enabled = %v, want true", hooks["enabled"])
	}
	stop := hooks["events"].(map[string]any)["Stop"].([]any)
	if len(stop) != 2 {
		t.Fatalf("Stop groups = %d, want 2 (foreign + ours)", len(stop))
	}
	captures := 0
	for _, g := range stop {
		for _, e := range g.(map[string]any)["hooks"].([]any) {
			entry := e.(map[string]any)
			args, _ := entry["args"].([]any)
			if len(args) > 0 && args[0] == "capture-session" {
				captures++
				if entry["type"] != "process" || entry["command"] != exe {
					t.Fatalf("capture entry = %v", entry)
				}
			}
		}
	}
	if captures != 1 {
		t.Fatalf("capture entries = %d, want 1 after double install", captures)
	}
	if _, ok := dig(t, m, "mcp", "servers")["other"]; !ok {
		t.Fatal("mcp section damaged by hook install")
	}

	_, removed, err := RemoveHooks(home, "zcode")
	if err != nil || !removed {
		t.Fatalf("remove: %v removed=%v", err, removed)
	}
	m = readJSON(t, path)
	stop = dig(t, m, "hooks", "events")["Stop"].([]any)
	if len(stop) != 1 || !strings.Contains(string(mustJSON(t, m)), "notify-send done") {
		t.Fatalf("foreign hook damaged by removal: %v", stop)
	}
	_, removed, err = RemoveHooks(home, "zcode")
	if err != nil || removed {
		t.Fatalf("second remove should be a no-op: %v removed=%v", err, removed)
	}
}

func mustJSON(t testing.TB, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// A fresh install into a config without any hooks scaffolding works, and
// removing our last entry takes the scaffolding with it.
func TestZCodeHookInstallFromScratch(t *testing.T) {
	home := t.TempDir()
	if _, err := InstallHooks(home, "zcode", exe, data); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".zcode", "cli", "config.json")
	events := dig(t, readJSON(t, path), "hooks", "events")
	if _, ok := events["Stop"]; !ok {
		t.Fatal("Stop event missing")
	}
	_, removed, err := RemoveHooks(home, "zcode")
	if err != nil || !removed {
		t.Fatalf("remove: %v removed=%v", err, removed)
	}
	m := readJSON(t, path)
	if _, ok := m["hooks"]; ok {
		t.Fatalf("empty hooks scaffolding left behind: %v", m["hooks"])
	}
}

func TestOpenCodeHookInstallAndRemove(t *testing.T) {
	home := t.TempDir()
	path, err := InstallHooks(home, "opencode", exe, data)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "memorytree-capture.js" || !strings.Contains(path, filepath.Join(".config", "opencode", "plugins")) {
		t.Fatalf("plugin path = %s", path)
	}
	script, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"session.idle", "capture-session", "Bun.spawn", "memorytree capture failed"} {
		if !strings.Contains(string(script), want) {
			t.Errorf("plugin script missing %q:\n%s", want, script)
		}
	}
	_, removed, err := RemoveHooks(home, "opencode")
	if err != nil || !removed {
		t.Fatalf("remove: %v removed=%v", err, removed)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("plugin file survived removal")
	}
	_, removed, err = RemoveHooks(home, "opencode")
	if err != nil || removed {
		t.Fatalf("second remove should be a no-op: %v removed=%v", err, removed)
	}
}

func TestHookInstallUnsupportedClient(t *testing.T) {
	if _, err := InstallHooks(t.TempDir(), "qoder", exe, data); err == nil {
		t.Fatal("qoder hook install should fail (unsupported)")
	}
	if _, _, err := RemoveHooks(t.TempDir(), "deepseek"); err == nil {
		t.Fatal("deepseek hook remove should fail (unsupported)")
	}
}
