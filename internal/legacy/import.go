// Package legacy imports an existing flat directory of Agent memory files
// into a MemoryTree repository as one project node. The import deliberately
// keeps the source memories flat; atomisation and re-parenting are left to the
// gardener workflow.
package legacy

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/li-ziyin/memorytree/internal/docfile"
	"github.com/li-ziyin/memorytree/internal/format"
	"github.com/li-ziyin/memorytree/internal/ids"
	"github.com/li-ziyin/memorytree/internal/model"
)

var (
	ErrAlreadyImported = errors.New("legacy/shared-memory already exists in the target repository")
	ErrNotInitialized  = errors.New("target is not a MemoryTree repository; run memoryd init first")
)

const (
	categorySlug = "legacy"
	projectSlug  = "shared-memory"
)

// Options configures Import.
type Options struct {
	From  string
	To    string
	Agent string
	Now   time.Time
}

// Stats reports what an import did.
type Stats struct {
	Imported int
	Skipped  int
	Project  string
}

type legacyMeta struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Metadata    struct {
		NodeType        string `yaml:"node_type"`
		Type            string `yaml:"type"`
		OriginSessionID string `yaml:"originSessionId"`
	} `yaml:"metadata"`
}

type planned struct {
	file    string
	title   string
	summary string
	typ     string
	session string
	created time.Time
	updated time.Time
	body    []byte
}

var datePrefix = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})-`)
var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Import reads every *.md file in opts.From except MEMORY.md and writes one
// entry per file under legacy/shared-memory. It parses everything before
// writing anything, so a malformed source file leaves the target untouched.
func Import(opts Options) (*Stats, error) {
	from, err := filepath.Abs(opts.From)
	if err != nil {
		return nil, err
	}
	to, err := filepath.Abs(opts.To)
	if err != nil {
		return nil, err
	}
	if opts.Agent == "" {
		opts.Agent = "zcode"
	}

	rootNode, err := loadRoot(to)
	if err != nil {
		return nil, err
	}

	items, skipped, err := plan(from)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("no legacy markdown files found in %s", from)
	}

	categoryID, err := ensureCategory(to, rootNode.ID, opts.Now)
	if err != nil {
		return nil, err
	}

	projectDir := filepath.Join(to, categorySlug, projectSlug)
	if _, err := os.Stat(filepath.Join(projectDir, "node.md")); err == nil {
		return nil, ErrAlreadyImported
	}
	if err := os.MkdirAll(filepath.Join(projectDir, "memories"), 0o700); err != nil {
		return nil, err
	}

	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	stamp := now.UTC().Format(time.RFC3339)
	project := model.Node{
		Schema:      model.NodeSchema,
		ID:          ids.NewUUIDv7(),
		Kind:        model.KindProject,
		Parent:      &categoryID,
		Slug:        projectSlug,
		Title:       "共享 Agent 记忆（扁平导入）",
		Summary:     "从现有 Agent 共享记忆目录导入的原始条目；尚未原子化，等待园丁整理。",
		TagsDelta:   []string{"shared-memory"},
		Aliases:     []string{},
		Status:      model.StatusActive,
		Sensitivity: model.SensitivityPrivate,
		CreatedAt:   stamp,
		UpdatedAt:   stamp,
	}
	if err := docfile.WriteFrontmatter(filepath.Join(projectDir, "node.md"), project, nil); err != nil {
		return nil, err
	}

	stats := &Stats{Project: projectDir, Skipped: skipped}
	usedNames := make(map[string]bool)
	for _, it := range items {
		name := uniqueFilename(usedNames, it, filepath.Join(projectDir, "memories"))
		entry := model.Entry{
			Schema:       model.EntrySchema,
			ID:           ids.NewUUIDv7(),
			Node:         project.ID,
			Type:         it.typ,
			Status:       model.StatusActive,
			Sensitivity:  model.SensitivityPrivate,
			Title:        it.title,
			Summary:      it.summary,
			TagsDelta:    []string{},
			Source:       model.Source{Agent: opts.Agent, Kind: "import", Session: it.session, CapturedAt: it.created.Format(time.RFC3339)},
			Supersedes:   []string{},
			SupersededBy: nil,
			Related:      []string{},
			CreatedAt:    it.created.UTC().Format(time.RFC3339),
			UpdatedAt:    it.updated.UTC().Format(time.RFC3339),
		}
		if err := docfile.WriteFrontmatter(filepath.Join(projectDir, "memories", name), entry, it.body); err != nil {
			return nil, err
		}
		stats.Imported++
	}
	return stats, nil
}

func loadRoot(to string) (*model.Node, error) {
	doc, err := format.ReadFile(filepath.Join(to, "node.md"))
	if err != nil {
		return nil, ErrNotInitialized
	}
	var n model.Node
	if err := format.Decode(doc.Meta, &n); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotInitialized, err)
	}
	if n.Kind != model.KindRoot {
		return nil, ErrNotInitialized
	}
	return &n, nil
}

func ensureCategory(to, rootID string, now time.Time) (string, error) {
	file := filepath.Join(to, categorySlug, "node.md")
	if _, statErr := os.Stat(file); statErr == nil {
		doc, err := format.ReadFile(file)
		if err != nil {
			return "", err
		}
		var n model.Node
		if err := format.Decode(doc.Meta, &n); err != nil {
			return "", err
		}
		if n.Kind != model.KindCategory || n.Slug != categorySlug {
			return "", fmt.Errorf("%s exists but is not the legacy category", file)
		}
		return n.ID, nil
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return "", statErr
	}

	if now.IsZero() {
		now = time.Now()
	}
	stamp := now.UTC().Format(time.RFC3339)
	if err := os.MkdirAll(filepath.Join(to, categorySlug), 0o700); err != nil {
		return "", err
	}
	parent := rootID
	n := model.Node{
		Schema:      model.NodeSchema,
		ID:          ids.NewUUIDv7(),
		Kind:        model.KindCategory,
		Parent:      &parent,
		Slug:        categorySlug,
		Title:       "旧记忆导入",
		Summary:     "从其他 Agent 的记忆目录导入的内容，未经过重新组织。",
		TagsDelta:   []string{categorySlug},
		Aliases:     []string{},
		Status:      model.StatusActive,
		Sensitivity: model.SensitivityPrivate,
		CreatedAt:   stamp,
		UpdatedAt:   stamp,
	}
	if err := docfile.WriteFrontmatter(file, n, nil); err != nil {
		return "", err
	}
	return n.ID, nil
}

func plan(from string) ([]planned, int, error) {
	dir, err := os.ReadDir(from)
	if err != nil {
		return nil, 0, err
	}
	names := make([]string, 0, len(dir))
	for _, de := range dir {
		if de.IsDir() || !strings.HasSuffix(strings.ToLower(de.Name()), ".md") || de.Name() == "MEMORY.md" {
			continue
		}
		names = append(names, de.Name())
	}
	sort.Strings(names)

	items := make([]planned, 0, len(names))
	skipped := 0
	for _, name := range names {
		file := filepath.Join(from, name)
		doc, err := format.ReadFile(file)
		if err != nil {
			return nil, 0, fmt.Errorf("%s: %w", name, err)
		}
		var meta legacyMeta
		if err := format.Decode(doc.Meta, &meta); err != nil {
			return nil, 0, fmt.Errorf("%s: %w", name, err)
		}

		info, err := os.Stat(file)
		if err != nil {
			return nil, 0, err
		}
		mod := info.ModTime().UTC().Truncate(time.Second)
		created := mod
		if m := datePrefix.FindStringSubmatch(name); m != nil {
			if t, err := time.Parse(time.RFC3339, m[1]+"T00:00:00Z"); err == nil {
				created = t
			}
		}
		updated := mod
		if updated.Before(created) {
			updated = created
		}

		typ := meta.Metadata.Type
		switch typ {
		case model.TypeUser, model.TypeFeedback, model.TypeProject, model.TypeReference:
		default:
			typ = model.TypeReference
		}

		title := firstHeading(doc.Body)
		if title == "" {
			title = meta.Name
		}
		if title == "" {
			title = strings.TrimSuffix(name, filepath.Ext(name))
		}
		title = truncateRunes(collapseSpace(title), 200)

		summary := collapseSpace(meta.Description)
		if summary == "" {
			summary = title
		}
		summary = truncateRunes(summary, 500)
		if summary == "" {
			skipped++
			continue
		}

		items = append(items, planned{
			file:    name,
			title:   title,
			summary: summary,
			typ:     typ,
			session: meta.Metadata.OriginSessionID,
			created: created,
			updated: updated,
			body:    doc.Body,
		})
	}
	return items, skipped, nil
}

func uniqueFilename(used map[string]bool, it planned, dir string) string {
	base := slugify(strings.TrimSuffix(it.file, filepath.Ext(it.file)))
	base = strings.TrimPrefix(base, datePrefix.FindString(it.file))
	base = strings.Trim(base, "-")
	if base == "" {
		base = "memory"
	}
	date := it.created.UTC().Format("2006-01-02")
	for i := 1; ; i++ {
		suffix := ""
		if i > 1 {
			suffix = fmt.Sprintf("-%d", i)
		}
		name := fmt.Sprintf("%s-%s%s.md", date, base, suffix)
		if used[name] {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, name)); os.IsNotExist(err) {
			used[name] = true
			return name
		}
	}
}

func slugify(s string) string {
	s = strings.ToLower(s)
	s = nonSlug.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 64 {
		s = strings.TrimRight(s[:64], "-")
	}
	if s == "" {
		return "memory"
	}
	return s
}

func firstHeading(body []byte) string {
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return ""
}

func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func truncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max-1]) + "…"
}
