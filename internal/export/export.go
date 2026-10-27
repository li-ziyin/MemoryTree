// Package export writes a sensitivity-filtered copy of a data repository:
// a share view for humans and other tools, not a full repository. The
// Markdown tree stays byte-identical for what is included; anything whose
// declared sensitivity is above the chosen level is left out entirely.
package export

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/li-ziyin/memorytree/internal/model"
	"github.com/li-ziyin/memorytree/internal/validate"
)

// Report describes one export.
type Report struct {
	Root           string `json:"root"`
	Out            string `json:"out"`
	Level          string `json:"level"`
	Nodes          int    `json:"nodes"`
	Entries        int    `json:"entries"`
	NodesSkipped   int    `json:"nodes_skipped"`
	EntriesSkipped int    `json:"entries_skipped"`
}

// Export validates the repository and copies the subtree of files whose
// sensitivity is at or below maxLevel into outDir. outDir must be empty or
// absent; nothing outside it is touched. Excluded entries can leave
// dangling supersedes/related references in the copy — the notes file
// says so, because a share view is not a validatable repository.
func Export(dataDir, outDir, maxLevel string) (*Report, error) {
	maxRank := model.SensitivityRank(maxLevel)
	if maxRank < 0 {
		return nil, fmt.Errorf("unknown sensitivity level %q; use public, internal, or private", maxLevel)
	}
	root, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, err
	}
	res, snap, err := validate.ValidateWithSnapshot(root)
	if err != nil {
		return nil, err
	}
	if !res.OK() {
		return nil, fmt.Errorf("repository has %d validation error(s); fix them before exporting", res.Errors())
	}
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return nil, err
	}
	if notEmpty(outDir) {
		return nil, fmt.Errorf("target directory %s is not empty; refusing to touch it", outDir)
	}

	report := &Report{Root: root, Out: outDir, Level: maxLevel}
	skippedPrefixes := []string{}
	for _, n := range snap.Nodes { // sorted by path: parents precede children
		if rank := model.SensitivityRank(n.Sensitivity); rank > maxRank || underAny(n.Path, skippedPrefixes) {
			report.NodesSkipped++
			skippedPrefixes = append(skippedPrefixes, n.Path+"/")
			continue
		}
		if err := copyFile(filepath.Join(root, filepath.FromSlash(n.Path), "node.md"),
			filepath.Join(outDir, filepath.FromSlash(n.Path), "node.md")); err != nil {
			return nil, err
		}
		report.Nodes++
		for _, e := range snap.Entries {
			if e.NodeID != n.ID {
				continue
			}
			if model.SensitivityRank(e.Sensitivity) > maxRank {
				report.EntriesSkipped++
				continue
			}
			if err := copyFile(filepath.Join(root, filepath.FromSlash(e.Path)),
				filepath.Join(outDir, filepath.FromSlash(e.Path))); err != nil {
				return nil, err
			}
			report.Entries++
		}
	}
	if err := copyFile(filepath.Join(root, "memory.yaml"), filepath.Join(outDir, "memory.yaml")); err != nil {
		return nil, err
	}
	notes := fmt.Sprintf(`# Export notes

- Origin repository: %s (repo_id %s)
- Max sensitivity level: %s
- Exported: %d nodes, %d entries; skipped: %d nodes, %d entries
- Generated at: %s

This copy is a **share view**, not a repository: entries excluded by the
sensitivity filter can leave dangling supersedes/related references in the
included files. Do not run memoryd validate or write tools against it.
`, root, snap.Repo.RepoID, maxLevel, report.Nodes, report.Entries,
		report.NodesSkipped, report.EntriesSkipped, time.Now().UTC().Format(time.RFC3339))
	if err := os.WriteFile(filepath.Join(outDir, "EXPORT-NOTES.md"), []byte(notes), 0o600); err != nil {
		return nil, err
	}
	return report, nil
}

func underAny(path string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

func notEmpty(dir string) bool {
	entries, err := os.ReadDir(dir)
	return err != nil || len(entries) > 0
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o600)
}
