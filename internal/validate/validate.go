package validate

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/li-ziyin/memorytree/internal/format"
	"github.com/li-ziyin/memorytree/internal/ids"
	"github.com/li-ziyin/memorytree/internal/model"
)

type nodeRec struct {
	node      *model.Node
	file      string
	dir       string
	digest    string
	parent    *nodeRec
	children  []*nodeRec
	tags      map[string]struct{}
	effective map[string]struct{}
}

type entryRec struct {
	entry   *model.Entry
	file    string
	hostDir string
	digest  string
	body    string
	host    *nodeRec
	tags    map[string]struct{}
}

type validator struct {
	root               string
	res                *Result
	repo               *model.Repo
	defaultSensitivity string
	nodes              []*nodeRec
	entries            []*entryRec
	nodesByID          map[string]*nodeRec
	entriesByID        map[string]*entryRec
	nodesByDir         map[string]*nodeRec
	idPath             map[string]string
	repoDigest         string
	fatal              bool
}

// Validate checks a repository rooted at root. A non-nil error means the
// repository could not be inspected at all; content problems are returned in
// Result.Issues with stable codes.
func Validate(root string) (*Result, error) {
	res, _, err := ValidateWithSnapshot(root)
	return res, err
}

// ValidateWithSnapshot validates a repository and, when there are no blocking
// issues, also returns a read-only view of its nodes and entries together with
// a fingerprint of every managed file. The snapshot is nil when validation
// fails.
func ValidateWithSnapshot(root string) (*Result, *Snapshot, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, nil, err
	}
	if !info.IsDir() {
		return nil, nil, fmt.Errorf("%s is not a directory", abs)
	}

	v := &validator{
		root:               abs,
		res:                &Result{Root: abs},
		defaultSensitivity: model.SensitivityPrivate,
		nodesByID:          make(map[string]*nodeRec),
		entriesByID:        make(map[string]*entryRec),
		nodesByDir:         make(map[string]*nodeRec),
		idPath:             make(map[string]string),
	}

	v.loadRepo()
	if !v.fatal {
		v.scan()
		v.linkNodes()
		v.checkNodes()
		v.checkEntries()
		v.checkSupersession()
	}
	v.res.sortIssues()
	if !v.res.OK() {
		return v.res, nil, nil
	}
	return v.res, v.snapshot(), nil
}

func (v *validator) rel(p string) string {
	r, err := filepath.Rel(v.root, p)
	if err != nil {
		return p
	}
	return filepath.ToSlash(r)
}

func (v *validator) loadRepo() {
	file := filepath.Join(v.root, "memory.yaml")
	data, err := format.ReadPlain(file)
	if err != nil {
		v.res.errf(CodeFile, "memory.yaml", "cannot read repository metadata: %v", err)
		v.fatal = true
		return
	}

	var repo model.Repo
	if err := format.Decode(data, &repo); err != nil {
		v.res.errf(CodeFile, "memory.yaml", "cannot parse repository metadata: %v", err)
		v.fatal = true
		return
	}
	v.repo = &repo
	v.repoDigest = digest(data)

	if repo.Schema != model.RepoSchema {
		v.res.errf(CodeSchema, "memory.yaml", "schema must be %q, got %q", model.RepoSchema, repo.Schema)
	}
	if repo.FormatVersion != model.FormatVersion {
		if repo.FormatVersion > model.FormatVersion {
			v.res.errf(CodeFormatUnsupported, "memory.yaml",
				"format_version %d is newer than the supported version %d", repo.FormatVersion, model.FormatVersion)
		} else {
			v.res.errf(CodeSchema, "memory.yaml",
				"format_version must be %d, got %d", model.FormatVersion, repo.FormatVersion)
		}
		v.fatal = true
		return
	}
	if !ids.IsUUIDv7(repo.RepoID) {
		v.res.errf(CodeIDFormat, "memory.yaml", "repo_id %q is not a canonical UUIDv7", repo.RepoID)
	}
	if !validTimestamp(repo.CreatedAt) {
		v.res.errf(CodeTime, "memory.yaml", "created_at %q is not RFC 3339", repo.CreatedAt)
	}
	if repo.DefaultSensitivity != "" {
		if model.SensitivityRank(repo.DefaultSensitivity) < 0 {
			v.res.errf(CodeSchema, "memory.yaml",
				"default_sensitivity %q is not public, internal, or private", repo.DefaultSensitivity)
		} else {
			v.defaultSensitivity = repo.DefaultSensitivity
		}
	}
}

func (v *validator) scan() {
	err := filepath.WalkDir(v.root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			v.res.errf(CodeFile, v.rel(p), "cannot inspect path: %v", walkErr)
			return nil
		}
		rel := v.rel(p)

		if d.Type()&fs.ModeSymlink != 0 {
			v.res.errf(CodeFile, rel, "symbolic links are not allowed in a managed repository")
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			if rel != "." && isReservedDir(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}

		name := d.Name()
		if name == "node.md" {
			v.readNode(p, rel)
			return nil
		}
		if strings.HasSuffix(strings.ToLower(name), ".md") && inMemoriesDir(rel) {
			v.readEntry(p, rel)
		}
		return nil
	})
	if err != nil {
		v.res.errf(CodeFile, ".", "repository walk failed: %v", err)
	}
}

func (v *validator) readNode(p, rel string) {
	rec := &nodeRec{file: rel, dir: path.Dir(rel)}
	v.res.Nodes++

	doc, err := format.ReadFile(p)
	if err != nil {
		v.reportReadError(rel, err)
		return
	}
	rec.digest = digest(doc.Raw)

	var n model.Node
	if err := format.Decode(doc.Meta, &n); err != nil {
		v.res.errf(CodeFile, rel, "cannot parse node frontmatter: %v", err)
		return
	}
	rec.node = &n
	v.nodes = append(v.nodes, rec)
	v.checkNodeFields(rec)

	if n.ID != "" {
		v.recordNodeID(rec)
	}
	if prev, exists := v.nodesByDir[rec.dir]; exists {
		v.res.errf(CodeFile, rel, "multiple node.md files for directory %q (also %s)", rec.dir, prev.file)
		return
	}
	v.nodesByDir[rec.dir] = rec
}

func (v *validator) readEntry(p, rel string) {
	rec := &entryRec{file: rel, hostDir: path.Dir(path.Dir(rel))}
	v.res.Entries++

	doc, err := format.ReadFile(p)
	if err != nil {
		v.reportReadError(rel, err)
		return
	}
	rec.digest = digest(doc.Raw)
	rec.body = string(doc.Body)

	var e model.Entry
	if err := format.Decode(doc.Meta, &e); err != nil {
		v.res.errf(CodeFile, rel, "cannot parse entry frontmatter: %v", err)
		return
	}
	rec.entry = &e
	v.entries = append(v.entries, rec)
	v.checkEntryFields(rec)

	if e.ID != "" {
		v.recordEntryID(rec)
	}
}

func (v *validator) recordNodeID(rec *nodeRec) {
	id := rec.node.ID
	if prev, ok := v.idPath[id]; ok {
		v.res.errf(CodeIDFormat, rec.file, "ID %s is already used by %s", id, prev)
		return
	}
	v.idPath[id] = rec.file
	v.nodesByID[id] = rec
}

func (v *validator) recordEntryID(rec *entryRec) {
	id := rec.entry.ID
	if prev, ok := v.idPath[id]; ok {
		v.res.errf(CodeIDFormat, rec.file, "ID %s is already used by %s", id, prev)
		return
	}
	v.idPath[id] = rec.file
	v.entriesByID[id] = rec
}

func (v *validator) reportReadError(rel string, err error) {
	switch {
	case errors.Is(err, format.ErrBodyTooBig), errors.Is(err, format.ErrTooLarge):
		v.res.errf(CodeBodySize, rel, "%v", err)
	default:
		v.res.errf(CodeFile, rel, "%v", err)
	}
}

func (v *validator) linkNodes() {
	for _, rec := range v.nodes {
		if rec.dir == "." {
			continue
		}
		parentDir := path.Dir(rec.dir)
		parent, ok := v.nodesByDir[parentDir]
		if !ok {
			v.res.errf(CodeParent, rec.file, "containing directory %q has no node.md", parentDir)
			continue
		}
		rec.parent = parent
		parent.children = append(parent.children, rec)
	}
}

func isReservedDir(name string) bool {
	switch name {
	case ".git", ".inbox", ".archive", ".index":
		return true
	}
	return false
}

func inMemoriesDir(rel string) bool {
	parts := strings.Split(rel, "/")
	return len(parts) >= 2 && parts[len(parts)-2] == "memories"
}
