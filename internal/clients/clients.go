// Package clients writes the memorytree MCP server entry into the local
// configuration of known MCP-capable agent clients. Each writer is idempotent:
// running it twice leaves one entry, and Remove deletes only that entry.
package clients

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type clientSpec struct {
	path   func(home string) string
	apply  func(path, exe, dataDir string) error
	remove func(path string) (bool, error)
}

// Known lists the supported client names.
func Known() []string {
	return []string{"deepseek", "opencode", "qoder", "workbuddy", "zcode"}
}

// HookKnown lists the clients with session-end capture support.
func HookKnown() []string {
	return []string{"opencode", "zcode"}
}

// Install writes (or overwrites) the memorytree MCP server entry for the
// named client and returns the configuration file path. The configuration
// file is backed up next to itself before it is modified.
func Install(home, name, exe, dataDir string) (string, error) {
	spec, ok := registry[name]
	if !ok {
		return "", fmt.Errorf("unknown client %q; supported: %s", name, strings.Join(Known(), ", "))
	}
	path := spec.path(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := spec.apply(path, exe, dataDir); err != nil {
		return "", err
	}
	return path, nil
}

// Remove deletes the memorytree entry from the named client. The second
// return value reports whether an entry was found and removed.
func Remove(home, name string) (string, bool, error) {
	spec, ok := registry[name]
	if !ok {
		return "", false, fmt.Errorf("unknown client %q; supported: %s", name, strings.Join(Known(), ", "))
	}
	path := spec.path(home)
	removed, err := spec.remove(path)
	return path, removed, err
}

// InstallHooks wires the client's session-end hook so a finished session
// leaves a capture in the inbox without any manual action. ZCode gets a
// Stop hook merged into its config.json (configuration-file hooks must
// also set hooks.enabled); OpenCode gets an auto-discovered plugin file
// under its plugins directory. Both are idempotent.
func InstallHooks(home, client, exe, dataDir string) (string, error) {
	switch client {
	case "zcode":
		return installZCodeHooks(home, exe, dataDir)
	case "opencode":
		return installOpenCodePlugin(home, exe, dataDir)
	default:
		return "", fmt.Errorf("client %q has no session-end capture support yet; supported: %s", client, strings.Join(HookKnown(), ", "))
	}
}

// RemoveHooks undoes InstallHooks; the second return reports whether a
// hook installation was found and removed.
func RemoveHooks(home, client string) (string, bool, error) {
	switch client {
	case "zcode":
		return removeZCodeHooks(home)
	case "opencode":
		return removeOpenCodePlugin(home)
	default:
		return "", false, fmt.Errorf("client %q has no session-end capture support yet; supported: %s", client, strings.Join(HookKnown(), ", "))
	}
}

var registry = map[string]clientSpec{
	// ZCode's server schema is strict: unknown keys drop the server, so the
	// entry stays minimal (command + args).
	"zcode": {
		path: func(home string) string { return filepath.Join(home, ".zcode", "cli", "config.json") },
		apply: directJSONApply(func(exe, data string) ([]string, map[string]any) {
			return []string{"mcp", "servers", "memorytree"},
				map[string]any{"command": exe, "args": []string{"mcp", data}}
		}),
		remove: jsonRemove("mcp", "servers", "memorytree"),
	},
	"qoder": {
		path: func(home string) string { return filepath.Join(home, ".qoder", "mcp.json") },
		apply: directJSONApply(func(exe, data string) ([]string, map[string]any) {
			return []string{"mcpServers", "memorytree"},
				map[string]any{"command": exe, "args": []string{"mcp", data}}
		}),
		remove: jsonRemove("mcpServers", "memorytree"),
	},
	// WorkBuddy's user MCP file (~/.workbuddy/mcp.json) is what its UI's
	// "add MCP" dialog writes; type/timeout match the entries it creates.
	"workbuddy": {
		path: func(home string) string { return filepath.Join(home, ".workbuddy", "mcp.json") },
		apply: directJSONApply(func(exe, data string) ([]string, map[string]any) {
			return []string{"mcpServers", "memorytree"},
				map[string]any{"type": "stdio", "command": exe, "args": []string{"mcp", data}, "timeout": 60000}
		}),
		remove: jsonRemove("mcpServers", "memorytree"),
	},
	"opencode": {
		path:   opencodePath,
		apply:  opencodeApply,
		remove: jsonRemove("mcp", "memorytree"),
	},
	// DeepSeek Harness manages its desktop profile itself; the user patch
	// layer (cordis.patch.yml) is the sanctioned place for extra plugin
	// entries, so the entry is appended there as text to keep every existing
	// comment and entry byte-identical.
	"deepseek": {
		path: func(home string) string {
			return filepath.Join(home, ".dsh", "profiles", "desktop", "cordis.patch.yml")
		},
		apply:  deepseekApply,
		remove: deepseekRemove,
	},
}

// --- JSON-backed clients ---

func directJSONApply(build func(exe, data string) ([]string, map[string]any)) func(path, exe, dataDir string) error {
	return func(path, exe, dataDir string) error {
		m, err := readJSONObject(path)
		if err != nil {
			return err
		}
		keys, entry := build(exe, dataDir)
		setNested(m, keys, entry)
		return writeJSONObject(path, m)
	}
}

func jsonRemove(keys ...string) func(path string) (bool, error) {
	return func(path string) (bool, error) {
		m, err := readJSONObject(path)
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if !deleteNested(m, keys) {
			return false, nil
		}
		return true, writeJSONObject(path, m)
	}
}

func readJSONObject(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %v", path, err)
	}
	return m, nil
}

func writeJSONObject(path string, m map[string]any) error {
	if err := backup(path); err != nil {
		return err
	}
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o600)
}

func setNested(m map[string]any, keys []string, value any) {
	for _, k := range keys[:len(keys)-1] {
		child, ok := m[k].(map[string]any)
		if !ok {
			child = map[string]any{}
			m[k] = child
		}
		m = child
	}
	m[keys[len(keys)-1]] = value
}

func deleteNested(m map[string]any, keys []string) bool {
	for _, k := range keys[:len(keys)-1] {
		child, ok := m[k].(map[string]any)
		if !ok {
			return false
		}
		m = child
	}
	last := keys[len(keys)-1]
	if _, ok := m[last]; !ok {
		return false
	}
	delete(m, last)
	return true
}

func backup(path string) error {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return os.WriteFile(path+".memoryd-bak", raw, 0o600)
}

// --- opencode (opencode.json / opencode.jsonc) ---

func opencodePath(home string) string {
	dir := filepath.Join(home, ".config", "opencode")
	for _, name := range []string{"opencode.jsonc", "opencode.json"} {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return filepath.Join(dir, "opencode.json")
}

func opencodeApply(path, exe, dataDir string) error {
	m, err := readJSONObject(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			m = map[string]any{}
		} else {
			// Most likely a JSONC file with comments; rewriting it blindly
			// would destroy them.
			return fmt.Errorf("%s cannot be rewritten automatically (is it JSONC with comments?); add the mcp.memorytree entry manually", path)
		}
	}
	setNested(m, []string{"mcp", "memorytree"}, map[string]any{
		"type": "local", "command": []string{exe, "mcp", dataDir}, "enabled": true,
	})
	return writeJSONObject(path, m)
}

// --- DeepSeek Harness (cordis.patch.yml) ---

const dshEntryID = "mcp-memorytree"

func deepseekApply(path, exe, dataDir string) error {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		raw = nil
	} else if err != nil {
		return err
	}
	content := string(raw)
	if strings.Contains(content, "id: "+dshEntryID) {
		return nil // already installed
	}
	block := fmt.Sprintf("- id: %s\n  name: \"@deepseek-ai/dsh-mcp-client\"\n  config:\n    serverName: memorytree\n    transport: stdio\n    command: %s\n    args: [mcp, %s]\n",
		dshEntryID, filepath.ToSlash(exe), filepath.ToSlash(dataDir))
	if len(content) > 0 && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if err := backup(path); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content+block), 0o600)
}

func deepseekRemove(path string) (bool, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	lines := strings.Split(string(raw), "\n")
	start := -1
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimLeft(l, " \t"), "- id: "+dshEntryID) {
			start = i
			break
		}
	}
	if start < 0 {
		return false, nil
	}
	end := len(lines)
	for j := start + 1; j < len(lines); j++ {
		if strings.HasPrefix(strings.TrimLeft(lines[j], " \t"), "- ") {
			end = j
			break
		}
	}
	kept := append(append([]string(nil), lines[:start]...), lines[end:]...)
	if err := backup(path); err != nil {
		return false, err
	}
	if err := os.WriteFile(path, []byte(strings.Join(kept, "\n")), 0o600); err != nil {
		return false, err
	}
	return true, nil
}

// --- session-end hooks (ZCode Stop hook, OpenCode session.idle plugin) ---

// captureArgsTag marks our hook entries inside a client's hooks array.
const captureArgsTag = "capture-session"

// installZCodeHooks merges a Stop hook into ~/.zcode/cli/config.json:
// hooks.enabled must be true for configuration-file hooks to run at all,
// and the entry is a type:process argument vector so no shell is involved.
// Template variables ${CLAUDE_SESSION_ID} and ${ZCODE_PROJECT_DIR} are
// expanded by the client; capture-session treats unexpanded leftovers as
// absent.
func installZCodeHooks(home, exe, dataDir string) (string, error) {
	path := filepath.Join(home, ".zcode", "cli", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	m, err := readJSONObject(path)
	if err != nil {
		return "", err
	}
	hooks, _ := m["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	hooks["enabled"] = true
	events, _ := hooks["events"].(map[string]any)
	if events == nil {
		events = map[string]any{}
		hooks["events"] = events
	}
	stop, _ := events["Stop"].([]any)
	stop = append(stripCaptureEntries(stop), map[string]any{
		"hooks": []any{map[string]any{
			"type":    "process",
			"command": exe,
			"args": []string{
				captureArgsTag, "--source", "zcode",
				"--session", "${CLAUDE_SESSION_ID}",
				"--cwd", "${ZCODE_PROJECT_DIR}",
				dataDir,
			},
			"timeoutMs":     60000,
			"statusMessage": "MemoryTree: capturing session summary",
		}},
	})
	events["Stop"] = stop
	m["hooks"] = hooks
	return path, writeJSONObject(path, m)
}

// removeZCodeHooks deletes our Stop entries and the scaffolding it added,
// leaving any other hooks untouched.
func removeZCodeHooks(home string) (string, bool, error) {
	path := filepath.Join(home, ".zcode", "cli", "config.json")
	m, err := readJSONObject(path)
	if errors.Is(err, os.ErrNotExist) {
		return path, false, nil
	}
	if err != nil {
		return path, false, err
	}
	hooks, _ := m["hooks"].(map[string]any)
	if hooks == nil {
		return path, false, nil
	}
	events, _ := hooks["events"].(map[string]any)
	if events == nil {
		return path, false, nil
	}
	stop, _ := events["Stop"].([]any)
	kept := stripCaptureEntries(stop)
	if len(kept) == len(stop) {
		return path, false, nil
	}
	if len(kept) > 0 {
		events["Stop"] = kept
	} else {
		delete(events, "Stop")
	}
	if len(events) == 0 {
		delete(hooks, "events")
	}
	if len(hooks) <= 1 { // only our enabled flag remains
		delete(m, "hooks")
	}
	return path, true, writeJSONObject(path, m)
}

// stripCaptureEntries drops every Stop-group entry whose args start with
// capture-session (an earlier memoryd installation), keeping the rest.
func stripCaptureEntries(stop []any) []any {
	var kept []any
	for _, groupAny := range stop {
		group, ok := groupAny.(map[string]any)
		if !ok {
			kept = append(kept, groupAny)
			continue
		}
		entries, _ := group["hooks"].([]any)
		var keptEntries []any
		for _, entryAny := range entries {
			if entry, ok := entryAny.(map[string]any); ok && argsStartWithCapture(entry["args"]) {
				continue
			}
			keptEntries = append(keptEntries, entryAny)
		}
		if len(keptEntries) == 0 {
			continue // group emptied entirely
		}
		group["hooks"] = keptEntries
		kept = append(kept, group)
	}
	return kept
}

// argsStartWithCapture reports whether a hook entry's args (a []string we
// just built or a []any read back from JSON) begin with the capture tag.
func argsStartWithCapture(raw any) bool {
	switch args := raw.(type) {
	case []string:
		return len(args) > 0 && args[0] == captureArgsTag
	case []any:
		if len(args) == 0 {
			return false
		}
		s, _ := args[0].(string)
		return s == captureArgsTag
	}
	return false
}

// opencodePluginName is the auto-discovered plugin file under
// ~/.config/opencode/plugins/.
const opencodePluginName = "memorytree-capture.js"

func installOpenCodePlugin(home, exe, dataDir string) (string, error) {
	dir := filepath.Join(home, ".config", "opencode", "plugins")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, opencodePluginName)
	script := fmt.Sprintf(`// MemoryTree session-end capture — installed by "memoryd install --hooks opencode".
// Auto-discovered from ~/.config/opencode/plugins/; remove the file (or
// "memoryd install --hooks opencode --remove") to stop capturing.
const EXE = %s
const DATA_DIR = %s

export const MemoryTreeCapture = async ({ directory }) => {
  return {
    event: async ({ event }) => {
      if (event.type !== "session.idle") return
      try {
        const sid = String(event.properties?.sessionID ?? event.properties?.session ?? "")
        const args = [EXE, "capture-session", "--source", "opencode"]
        if (sid) args.push("--session", sid)
        args.push("--cwd", String(directory ?? ""), DATA_DIR)
        const proc = Bun.spawn(args, { stdin: "ignore", stdout: "ignore", stderr: "pipe" })
        await proc.exited
      } catch (e) {
        console.error("memorytree capture failed:", e)
      }
    },
  }
}
`, jsString(exe), jsString(dataDir))
	return path, os.WriteFile(path, []byte(script), 0o644)
}

func removeOpenCodePlugin(home string) (string, bool, error) {
	path := filepath.Join(home, ".config", "opencode", "plugins", opencodePluginName)
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return path, false, nil
		}
		return path, false, err
	}
	return path, true, nil
}

// jsString renders a Go string as a JavaScript string literal.
func jsString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(b)
}
