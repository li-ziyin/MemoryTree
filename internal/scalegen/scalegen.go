// Package scalegen generates valid synthetic data repositories for
// benchmarks and scale tests. Bodies mix Chinese and ASCII so both the
// trigram and rune-split tokenizers get realistic input.
package scalegen

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/li-ziyin/memorytree/internal/docfile"
	"github.com/li-ziyin/memorytree/internal/format"
	"github.com/li-ziyin/memorytree/internal/ids"
	"github.com/li-ziyin/memorytree/internal/initrepo"
)

const bodyLine = "排障记录：网关返回 502 时先查 docker compose 日志，再确认端口映射与重试策略；" +
	"the gateway retries with exponential backoff and mirrors sessions.\n"

type entryMeta struct {
	Schema      string   `yaml:"schema"`
	ID          string   `yaml:"id"`
	Node        string   `yaml:"node"`
	Type        string   `yaml:"type"`
	Status      string   `yaml:"status"`
	Sensitivity string   `yaml:"sensitivity"`
	Title       string   `yaml:"title"`
	Summary     string   `yaml:"summary"`
	TagsDelta   []string `yaml:"tags_delta"`
	Source      struct {
		Agent      string `yaml:"agent"`
		Kind       string `yaml:"kind"`
		Session    string `yaml:"session"`
		CapturedAt string `yaml:"captured_at"`
	} `yaml:"source"`
	Supersedes   []string `yaml:"supersedes"`
	SupersededBy *string  `yaml:"superseded_by"`
	CreatedAt    string   `yaml:"created_at"`
	UpdatedAt    string   `yaml:"updated_at"`
}

// Generate initializes root and fills global/memories with n valid entries.
// Every hundredth entry carries the token needle-<i%10> in its title so
// benchmarks have known targets.
func Generate(root string, n int) error {
	if _, err := initrepo.Init(initrepo.Options{Root: root, Title: "Scale fixture"}); err != nil {
		return err
	}
	doc, err := format.ReadFile(filepath.Join(root, "global", "node.md"))
	if err != nil {
		return err
	}
	var node struct {
		ID string `yaml:"id"`
	}
	if err := format.Decode(doc.Meta, &node); err != nil {
		return err
	}
	memDir := filepath.Join(root, "global", "memories")
	if err := os.MkdirAll(memDir, 0o700); err != nil {
		return err
	}

	body := []byte(strings.Repeat(bodyLine, 16)) // ~2 KiB per entry
	for i := 0; i < n; i++ {
		title := fmt.Sprintf("规模条目 %d：docker 端口与网关排障 %d", i, i%50)
		if i%100 == 0 {
			title = fmt.Sprintf("金丝雀 needle-%d 规模条目 %d", i/100%10, i)
		}
		m := entryMeta{
			Schema: "memorytree.entry/v1",
			ID:     ids.NewUUIDv7(),
			Node:   node.ID,
			Type:   "project",
			Status: "active",
			// The initrepo skeleton marks the global node private; entries
			// may not be weaker than their host node (V-SENS-1).
			Sensitivity: "private",
			Title:       title,
			Summary:     fmt.Sprintf("第 %d 条合成条目：端口、网关、docker 排障记录。", i),
			TagsDelta:   []string{"scale", fmt.Sprintf("batch-%d", i%10)},
			Supersedes:  []string{},
			CreatedAt:   "2026-01-01T00:00:00Z",
			UpdatedAt:   "2026-01-01T00:00:00Z",
		}
		m.Source.Agent = "scalegen"
		m.Source.Kind = "manual"
		m.Source.Session = "scale"
		m.Source.CapturedAt = "2026-01-01T00:00:00Z"
		path := filepath.Join(memDir, fmt.Sprintf("2026-01-01-scale-%06d.md", i))
		if err := docfile.WriteFrontmatter(path, m, body); err != nil {
			return err
		}
	}
	return nil
}
