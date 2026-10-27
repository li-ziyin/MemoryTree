// Package reposync implements memoryd sync: fetch + rebase + push for the
// data repository under the single-writer lock, so a sync never interleaves
// with a memory_add or gardener apply. The tree must be clean; conflicts
// are never auto-resolved — a conflicted rebase is left in place for the
// user to finish with git.
package reposync

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/li-ziyin/memorytree/internal/gitio"
	"github.com/li-ziyin/memorytree/internal/index"
	"github.com/li-ziyin/memorytree/internal/store"
)

var (
	// ErrDirtyTree means the work tree has uncommitted changes; sync refuses
	// so a rebase can never race half-finished work.
	ErrDirtyTree = errors.New("work tree has uncommitted changes")
	// ErrNoUpstream means the current branch has no configured upstream.
	ErrNoUpstream = errors.New("the current branch has no upstream")
	// ErrRebaseInProgress means an earlier rebase or merge is unfinished.
	ErrRebaseInProgress = errors.New("a rebase or merge is already in progress")
	// ErrConflict means the rebase hit conflicts; they are left for the
	// user to resolve with git.
	ErrConflict = errors.New("sync stopped on conflicts")
)

// maxPushAttempts bounds the fetch+rebase+push cycle when the remote keeps
// moving between fetch and push (two agents syncing at once).
const maxPushAttempts = 3

// Result reports what one sync did.
type Result struct {
	Remote    string `json:"remote"`
	Branch    string `json:"branch"`
	Upstream  string `json:"upstream"`
	Attempts  int    `json:"attempts"`
	Rebased   bool   `json:"rebased"`
	Pushed    bool   `json:"pushed"`
	UpToDate  bool   `json:"up_to_date"`
	IndexGen  int64  `json:"index_generation"`
	Conflict  bool   `json:"conflict,omitempty"`
	GitOutput string `json:"-"`
}

// Sync converges the data repository with its upstream: fetch, rebase the
// current branch onto the upstream, push. Every step runs under the
// single-writer lock. Validation of the tree is the caller's business —
// sync moves bytes; git history stays the audit trail.
func Sync(dataDir string) (*Result, error) {
	root, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, err
	}
	if err := gitio.Preflight(root); err != nil {
		return nil, err
	}

	if kind, err := gitio.InProgress(root); err != nil {
		return nil, err
	} else if kind != "" {
		return nil, fmt.Errorf("%w (%s); finish it with git rebase --continue/--abort or git merge, then re-run sync", ErrRebaseInProgress, kind)
	}
	if dirty, err := gitio.Status(root); err != nil {
		return nil, err
	} else if len(dirty) > 0 {
		return nil, fmt.Errorf("%w: %d path(s) changed (first: %s); commit them, and run memoryd doctor --fix to recover any uncommitted managed write", ErrDirtyTree, len(dirty), dirty[0].Path)
	}

	remote, upstream, err := gitio.Upstream(root)
	if err != nil {
		return nil, fmt.Errorf("%w; set one with: git -C %s push -u <remote> HEAD", ErrNoUpstream, root)
	}
	branch, err := gitio.Branch(root)
	if err != nil {
		return nil, err
	}
	if branch == "" {
		return nil, fmt.Errorf("%w; HEAD is detached", ErrNoUpstream)
	}

	res := &Result{Remote: remote, Branch: branch, Upstream: upstream}
	err = store.WithWriteLock(root, func() error {
		for attempt := 1; ; attempt++ {
			res.Attempts = attempt
			if out, err := gitio.Fetch(root, remote); err != nil {
				return fmt.Errorf("fetch %s failed: %w: %s", remote, err, out)
			}
			behind, err := behindCount(root, branch, upstream)
			if err != nil {
				return err
			}
			if behind > 0 {
				if out, err := gitio.Rebase(root, upstream); err != nil {
					if kind, kerr := gitio.InProgress(root); kerr == nil && kind == "rebase" {
						res.Conflict = true
						return fmt.Errorf("%w: rebase onto %s stopped; resolve the files, then `git -C %s rebase --continue` (or --abort) and re-run sync: %s",
							ErrConflict, upstream, root, firstLine(out))
					}
					return err
				}
				res.Rebased = true
			}
			published, rejected, perr := push(root, remote, branch)
			if perr != nil {
				return perr
			}
			res.Pushed = published
			// The remote moved again between fetch and push (rejected
			// non-fast-forward); redo the cycle a bounded number of times.
			// An up-to-date push is a quiet success with nothing published.
			if rejected && attempt < maxPushAttempts {
				continue
			}
			if rejected {
				return fmt.Errorf("push kept being rejected by a moving %s after %d attempts; retry shortly", remote, attempt)
			}
			res.UpToDate = !res.Rebased && !published
			return nil
		}
	})
	if err != nil {
		return res, err
	}

	// Files may have changed under the index; refresh it like a write does.
	if build, err := index.Build(root, index.BuildOptions{}); err == nil {
		res.IndexGen = build.Generation
	}
	return res, nil
}

// behindCount returns how many commits the upstream is ahead of the local
// branch (how many the rebase will replay onto).
func behindCount(root, branch, upstream string) (int, error) {
	out, err := gitio.RunRevListCount(root, branch+".."+upstream)
	if err != nil {
		return 0, fmt.Errorf("git rev-list --count %s..%s: %w: %s", branch, upstream, err, out)
	}
	n := 0
	for _, c := range strings.TrimSpace(out) {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("rev-list count %q is not a number", out)
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

// push publishes the branch and reports whether anything was published and
// whether the remote rejected it as non-fast-forward (it moved ahead, worth
// another fetch+rebase cycle). Any other failure is an error.
func push(root, remote, branch string) (published, rejected bool, err error) {
	out, err := gitio.Push(root, remote, branch)
	if err == nil {
		return !strings.Contains(out, "Everything up-to-date"), false, nil
	}
	if strings.Contains(out, "non-fast-forward") || strings.Contains(out, "fetch first") || strings.Contains(out, "behind") {
		return false, true, nil
	}
	return false, false, fmt.Errorf("push %s %s failed: %w: %s", remote, branch, err, out)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
