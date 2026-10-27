package store

import (
	"fmt"
	"path/filepath"
	"sort"

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

// takeOver moves the head role from target to newID. Format v1 keeps
// supersedes lists only on the active head (V-SUP-4), so the target is
// superseded with an EMPTY list, its predecessor set is inherited by the
// new head, and every predecessor's superseded_by is re-pointed (V-SUP-2).
// Version history chains through superseded_by and git instead. It returns
// the repository-relative paths it rewrote.
func takeOver(root string, snap *validate.Snapshot, newID string, target validate.EntryInfo) ([]string, error) {
	inherit := append([]string(nil), target.Supersedes...)
	rels := []string{target.Path}
	if _, err := rewriteEntry(root, target.Path, func(e *model.Entry) {
		e.Status = model.EntrySuperseded
		idCopy := newID
		e.SupersededBy = &idCopy
		e.Supersedes = []string{}
		e.UpdatedAt = now().Format(timeFormat)
	}); err != nil {
		return nil, fmt.Errorf("rewrite superseded entry %s: %w", target.ID, err)
	}
	for _, mid := range inherit {
		m, ok := entryByID(snap, mid)
		if !ok {
			return nil, inputf("V-SUP-1", "superseded predecessor %s vanished during the write", mid)
		}
		if _, err := rewriteEntry(root, m.Path, func(e *model.Entry) {
			idCopy := newID
			e.SupersededBy = &idCopy
			e.UpdatedAt = now().Format(timeFormat)
		}); err != nil {
			return nil, fmt.Errorf("re-point predecessor %s: %w", mid, err)
		}
		rels = append(rels, m.Path)
	}
	return rels, nil
}

// supersedeOldEntries applies takeOver for every target of a just-written
// entry, then rewrites that entry's own supersedes list to the flattened
// union of its requested targets and everything those targets superseded.
// It returns the repository-relative paths it rewrote.
func supersedeOldEntries(root string, snap *validate.Snapshot, newEntryRel, newID string, requested []string) ([]string, error) {
	flat := make([]string, 0, len(requested)+1)
	seen := make(map[string]bool, len(requested)+1)
	add := func(id string) {
		if !seen[id] {
			seen[id] = true
			flat = append(flat, id)
		}
	}
	var rels []string
	for _, id := range requested {
		target, ok := entryByID(snap, id)
		if !ok {
			return nil, inputf("V-SUP-1", "supersedes target %s vanished during the write", id)
		}
		add(id)
		for _, mid := range target.Supersedes {
			add(mid)
		}
		takeRels, err := takeOver(root, snap, newID, target)
		if err != nil {
			return nil, err
		}
		rels = append(rels, takeRels...)
	}
	sort.Strings(flat)
	if _, err := rewriteEntry(root, newEntryRel, func(e *model.Entry) {
		e.Supersedes = flat
	}); err != nil {
		return nil, fmt.Errorf("rewrite new entry supersedes list: %w", err)
	}
	return append(rels, newEntryRel), nil
}

// SupersedeResult describes one SupersedeEntry call.
type SupersedeResult struct {
	OldPath    string `json:"old_path"`
	ByID       string `json:"by_id"`
	ByPath     string `json:"by_path"`
	Generation int64  `json:"generation"`
	Committed  bool   `json:"committed"`
}

// SupersedeEntry links two existing entries: old becomes superseded, its
// predecessor set is inherited by by, and every predecessor is re-pointed
// (V-SUP-2/4/5).
func SupersedeEntry(dataDir, oldID, byID string) (*SupersedeResult, error) {
	root, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, err
	}
	if err := gitio.Preflight(root); err != nil {
		return nil, err
	}
	lock, err := acquireWriteLockRetried(root)
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

	takeRels, err := takeOver(root, snap, byID, old)
	if err != nil {
		return nil, err
	}
	flat := append(append([]string(nil), by.Supersedes...), old.Supersedes...)
	flat = append(flat, oldID)
	sort.Strings(unique(flat))
	byPath, err := rewriteEntry(root, by.Path, func(e *model.Entry) {
		e.Supersedes = flat
		e.UpdatedAt = now().Format(timeFormat)
	})
	if err != nil {
		return nil, fmt.Errorf("rewrite superseding entry: %w", err)
	}
	oldPath := filepath.Join(root, filepath.FromSlash(old.Path))
	rels := append(takeRels, by.Path)
	gen, err := finishWrite(root, rels,
		fmt.Sprintf("memoryd: supersede %s by %s", oldID, byID))
	if err != nil {
		return nil, err
	}
	return &SupersedeResult{OldPath: oldPath, ByID: byID, ByPath: byPath, Generation: gen, Committed: true}, nil
}

func unique(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := ids[:0]
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}
