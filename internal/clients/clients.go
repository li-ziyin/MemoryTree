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
