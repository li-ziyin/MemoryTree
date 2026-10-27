// Package projects maps working directories to project nodes through the
// optional projects.yaml in the data repository. It resolves the `project`
// input of memory_brief when the caller passes a working directory instead.
package projects

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/li-ziyin/memorytree/internal/format"
	"gopkg.in/yaml.v3"
)

// Schema is the required schema value of projects.yaml.
const Schema = "memorytree.projects/v1"

// FileName is the file read from the data repository root.
const FileName = "projects.yaml"

const maxFileBytes = 64 << 10

// Entry maps one absolute working-directory path to a project node reference
// (slug path or UUIDv7).
type Entry struct {
	Path string `yaml:"path"`
	Node string `yaml:"node"`
}

// Map holds loaded entries ordered by descending path length, so the first
// prefix hit is the longest one.
type Map struct {
	entries []Entry
}

// Len reports the number of loaded entries.
func (m *Map) Len() int { return len(m.entries) }

type doc struct {
	Schema   string  `yaml:"schema"`
	Projects []Entry `yaml:"projects"`
}

// Load reads projects.yaml from the data repository. A missing file is an
// empty map, not an error; anything unreadable or invalid is.
func Load(dataDir string) (*Map, error) {
	path := filepath.Join(dataDir, FileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &Map{}, nil
		}
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(raw) > maxFileBytes {
		return nil, fmt.Errorf("%s: larger than %d bytes", path, maxFileBytes)
	}
	var d doc
	if err := format.Decode(raw, &d); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := rejectUnknownKeys(raw, path); err != nil {
		return nil, err
	}
	if d.Schema != Schema {
		return nil, fmt.Errorf("%s: schema must be %q", path, Schema)
	}
	seen := make(map[string]bool, len(d.Projects))
	for i, e := range d.Projects {
		expanded, err := normalizePath(e.Path)
		if err != nil {
			return nil, fmt.Errorf("%s: projects[%d].path %q: %w", path, i, e.Path, err)
		}
		if strings.TrimSpace(e.Node) == "" {
			return nil, fmt.Errorf("%s: projects[%d].node is empty", path, i)
		}
		if seen[expanded] {
			return nil, fmt.Errorf("%s: projects[%d].path %q is listed twice", path, i, e.Path)
		}
		seen[expanded] = true
		d.Projects[i].Path = expanded
	}
	sort.SliceStable(d.Projects, func(a, b int) bool {
		return len(d.Projects[a].Path) > len(d.Projects[b].Path)
	})
	return &Map{entries: d.Projects}, nil
}

// rejectUnknownKeys fails on any key other than schema/projects at the top
// level and path/node per entry, so a typo'd mapping cannot silently disable
// resolution.
func rejectUnknownKeys(raw []byte, path string) error {
	var probe map[string]any
	if err := yaml.Unmarshal(raw, &probe); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	for k := range probe {
		if k != "schema" && k != "projects" {
			return fmt.Errorf("%s: unknown key %q (want schema, projects)", path, k)
		}
	}
	list, ok := probe["projects"].([]any)
	if !ok {
		return nil
	}
	for i, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: projects[%d] must be a mapping", path, i)
		}
		for k := range m {
			if k != "path" && k != "node" {
				return fmt.Errorf("%s: projects[%d] has unknown key %q (want path, node)", path, i, k)
			}
		}
	}
	return nil
}

// Match resolves dir to the node of the longest matching entry. dir may start
// with ~; relative paths never match. Comparison is case-insensitive on
// Windows and exact elsewhere.
func (m *Map) Match(dir string) (string, bool) {
	abs, err := normalizePath(dir)
	if err != nil {
		return "", false
	}
	for _, e := range m.entries {
		if pathPrefix(abs, e.Path) {
			return e.Node, true
		}
	}
	return "", false
}

func normalizePath(p string) (string, error) {
	if p == "" {
		return "", errors.New("empty path")
	}
	if p == "~" || strings.HasPrefix(p, "~"+string(filepath.Separator)) || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		p = filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	clean := filepath.Clean(p)
	if !filepath.IsAbs(clean) {
		return "", errors.New("must be absolute or start with ~")
	}
	return clean, nil
}

// pathPrefix reports whether dir equals base or lies directly below it at any
// depth, honouring the platform's case sensitivity.
func pathPrefix(dir, base string) bool {
	if len(dir) < len(base) {
		return false
	}
	if runtime.GOOS == "windows" {
		if !strings.EqualFold(dir[:len(base)], base) {
			return false
		}
	} else if dir[:len(base)] != base {
		return false
	}
	return len(dir) == len(base) || dir[len(base)] == filepath.Separator || dir[len(base)] == '/'
}
