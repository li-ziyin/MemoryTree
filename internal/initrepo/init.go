// Package initrepo creates a new, empty MemoryTree data repository.
package initrepo

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/li-ziyin/memorytree/internal/docfile"
	"github.com/li-ziyin/memorytree/internal/ids"
	"github.com/li-ziyin/memorytree/internal/model"
)

// ErrNotEmpty is returned when the target directory already holds files.
var ErrNotEmpty = errors.New("target directory is not empty")

// Options configures Init.
type Options struct {
	// Root is the data repository directory. It is created if missing.
	Root string
	// Title is the root node title. Defaults to "MemoryTree".
	Title string
	// Now overrides the clock in tests. Zero means time.Now.
	Now time.Time
}

// Init creates the repository skeleton and returns the absolute root path.
func Init(opts Options) (string, error) {
	if strings.TrimSpace(opts.Root) == "" {
		return "", errors.New("data directory is required")
	}
	abs, err := filepath.Abs(opts.Root)
	if err != nil {
		return "", err
	}

	if info, err := os.Stat(abs); err == nil {
		if !info.IsDir() {
			return "", fmt.Errorf("%s exists and is not a directory", abs)
		}
		entries, err := os.ReadDir(abs)
		if err != nil {
			return "", err
		}
		if len(entries) > 0 {
			return "", fmt.Errorf("%w: %s", ErrNotEmpty, abs)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	stamp := now.UTC().Format(time.RFC3339)
	title := strings.TrimSpace(opts.Title)
	if title == "" {
		title = "MemoryTree"
	}
	if utf8.RuneCountInString(title) > 120 {
		return "", errors.New("title must be 120 characters or fewer")
	}

	if err := os.MkdirAll(filepath.Join(abs, "global", "memories"), 0o700); err != nil {
		return "", err
	}

	repo := model.Repo{
		Schema:             model.RepoSchema,
		FormatVersion:      model.FormatVersion,
		RepoID:             ids.NewUUIDv7(),
		CreatedAt:          stamp,
		DefaultSensitivity: model.SensitivityPrivate,
	}
	if err := docfile.WriteYAML(filepath.Join(abs, "memory.yaml"), repo); err != nil {
		return "", err
	}

	root := model.Node{
		Schema:      model.NodeSchema,
		ID:          ids.NewUUIDv7(),
		Kind:        model.KindRoot,
		Parent:      nil,
		Slug:        "",
		Title:       title,
		Summary:     "个人与跨项目记忆的根节点。",
		TagsDelta:   []string{},
		Aliases:     []string{},
		Status:      model.StatusActive,
		Sensitivity: model.SensitivityPrivate,
		CreatedAt:   stamp,
		UpdatedAt:   stamp,
	}
	if err := docfile.WriteFrontmatter(filepath.Join(abs, "node.md"), root, nil); err != nil {
		return "", err
	}

	globalParent := root.ID
	global := model.Node{
		Schema:      model.NodeSchema,
		ID:          ids.NewUUIDv7(),
		Kind:        model.KindCategory,
		Parent:      &globalParent,
		Slug:        model.GlobalSlug,
		Title:       "全局",
		Summary:     "跨项目通用的用户画像、偏好与长期约定。",
		TagsDelta:   []string{model.GlobalSlug},
		Aliases:     []string{},
		Status:      model.StatusActive,
		Sensitivity: model.SensitivityPrivate,
		CreatedAt:   stamp,
		UpdatedAt:   stamp,
	}
	if err := docfile.WriteFrontmatter(filepath.Join(abs, "global", "node.md"), global, nil); err != nil {
		return "", err
	}

	if err := os.WriteFile(filepath.Join(abs, ".gitignore"),
		[]byte(".index/\n.inbox/\n.archive/\n.DS_Store\nThumbs.db\n"), 0o600); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(abs, ".gitattributes"),
		[]byte("* text=auto eol=lf\n"), 0o600); err != nil {
		return "", err
	}
	return abs, nil
}
