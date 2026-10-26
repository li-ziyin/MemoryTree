package store

import (
	"fmt"
	"path/filepath"

	"github.com/li-ziyin/memorytree/internal/docfile"
	"github.com/li-ziyin/memorytree/internal/format"
	"github.com/li-ziyin/memorytree/internal/gitio"
	"github.com/li-ziyin/memorytree/internal/model"
	"github.com/li-ziyin/memorytree/internal/validate"
)

// rewriteEntry loads an entry file, applies mutate, and writes it back
// atomically with the body preserved. rel is the repository-relative POSIX
// path from a validation snapshot.
func rewriteEntry(root, rel string, mutate func(*model.Entry)) (string, error) {
	full := filepath.Join(root, filepath.FromSlash(rel))
	doc, err := format.ReadFile(full)
	if err != nil {
		return "", err
	}
	var entry model.Entry
	if err := format.Decode(doc.Meta, &entry); err != nil {
		return "", err
	}
	mutate(&entry)
	data, err := docfile.MarshalFrontmatter(&entry, doc.Body)
	if err != nil {
		return "", err
	}
	if err := writeFileAtomic(full, data); err != nil {
		return "", err
	}
	return full, nil
}

func entryByID(snap *validate.Snapshot, id string) (validate.EntryInfo, bool) {
	for _, e := range snap.Entries {
		if e.ID == id {
			return e, true
		}
	}
	return validate.EntryInfo{}, false
}

// supersedeOldEntries flips every superseded target of a just-written entry
// to status superseded and points superseded_by at it (V-SUP-2/3/5). It
// returns the repository-relative paths it rewrote.
func supersedeOldEntries(root string, snap *validate.Snapshot, newID string, supersedes []string) ([]string, error) {
	var rewritten []string
	for _, id := range supersedes {
		target, ok := entryByID(snap, id)
		if !ok {
			return nil, inputf("V-SUP-1", "supersedes target %s vanished during the write", id)
		}
		if _, err := rewriteEntry(root, target.Path, func(e *model.Entry) {
			e.Status = model.EntrySuperseded
			newIDCopy := newID
			e.SupersededBy = &newIDCopy
			e.UpdatedAt = now().Format(timeFormat)
		}); err != nil {
			return nil, fmt.Errorf("rewrite superseded entry %s: %w", id, err)
		}
		rewritten = append(rewritten, target.Path)
	}
	return rewritten, nil
}

// SupersedeResult describes one SupersedeEntry call.
type SupersedeResult struct {
	OldPath    string `json:"old_path"`
	ByID       string `json:"by_id"`
	ByPath     string `json:"by_path"`
	Generation int64  `json:"generation"`
	Committed  bool   `json:"committed"`
}

// SupersedeEntry links two existing entries: old becomes superseded and
// superseded_by points at by, whose supersedes list gains old (V-SUP-2).
func SupersedeEntry(dataDir, oldID, byID string) (*SupersedeResult, error) {
	root, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, err
	}
	if err := gitio.Preflight(root); err != nil {
		return nil, err
	}
	lock, err := acquireWriteLock(root)
	if err != nil {
		return nil, err
	}
	defer lock.release()

	_, snap, err := validateSnapshot(root)
	if err != nil {
		return nil, err
	}

	old, ok := entryByID(snap, oldID)
	if !ok {
		return nil, inputf("V-SUP-1", "entry %s does not exist", oldID)
	}
	by, ok := entryByID(snap, byID)
	if !ok {
		return nil, inputf("V-SUP-1", "entry %s does not exist", byID)
	}
	if oldID == byID {
		return nil, inputf("V-SUP-1", "an entry cannot supersede itself")
	}
	if old.Status != model.StatusActive {
		return nil, inputf("V-SUP-5", "entry %s is %s; superseded entries are terminal", oldID, old.Status)
	}
	if old.SupersededBy != nil {
		return nil, inputf("V-SUP-2", "entry %s is already superseded by %s", oldID, *old.SupersededBy)
	}
	if by.Status != model.StatusActive {
		return nil, inputf("V-SUP-4", "superseding entry %s must be active, is %s", byID, by.Status)
	}
	for _, id := range by.Supersedes {
		if id == oldID {
			return nil, inputf("V-SUP-2", "entry %s already supersedes %s", byID, oldID)
		}
	}

	oldPath, err := rewriteEntry(root, old.Path, func(e *model.Entry) {
		e.Status = model.EntrySuperseded
		idCopy := byID
		e.SupersededBy = &idCopy
		e.UpdatedAt = now().Format(timeFormat)
	})
	if err != nil {
		return nil, fmt.Errorf("rewrite superseded entry: %w", err)
	}
	byPath, err := rewriteEntry(root, by.Path, func(e *model.Entry) {
		e.Supersedes = append(e.Supersedes, oldID)
		e.UpdatedAt = now().Format(timeFormat)
	})
	if err != nil {
		return nil, fmt.Errorf("rewrite superseding entry: %w", err)
	}
	gen, err := finishWrite(root, []string{old.Path, by.Path},
		fmt.Sprintf("memoryd: supersede %s by %s", oldID, byID))
	if err != nil {
		return nil, err
	}
	return &SupersedeResult{OldPath: oldPath, ByID: byID, ByPath: byPath, Generation: gen, Committed: true}, nil
}
