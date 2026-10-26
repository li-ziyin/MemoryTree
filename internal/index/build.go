package index

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/li-ziyin/memorytree/internal/validate"
)

var dropStatements = []string{
	`DROP TABLE IF EXISTS entries_fts_chars`,
	`DROP TABLE IF EXISTS entries_fts`,
	`DROP TABLE IF EXISTS entry_tags`,
	`DROP TABLE IF EXISTS entries`,
	`DROP TABLE IF EXISTS nodes`,
	`DROP TABLE IF EXISTS meta`,
}

var schemaStatements = []string{
	`CREATE TABLE meta (
		meta_key   TEXT NOT NULL PRIMARY KEY,
		meta_value TEXT NOT NULL
	) WITHOUT ROWID`,

	`CREATE TABLE nodes (
		id          TEXT NOT NULL PRIMARY KEY,
		parent_id   TEXT,
		kind        TEXT NOT NULL,
		slug        TEXT NOT NULL,
		path        TEXT NOT NULL UNIQUE,
		title       TEXT NOT NULL,
		summary     TEXT NOT NULL,
		tags        TEXT NOT NULL,
		status      TEXT NOT NULL,
		sensitivity TEXT NOT NULL,
		depth       INTEGER NOT NULL,
		created_at  TEXT NOT NULL,
		updated_at  TEXT NOT NULL
	) WITHOUT ROWID`,

	`CREATE TABLE entries (
		rowid         INTEGER PRIMARY KEY,
		id            TEXT NOT NULL UNIQUE,
		node_id       TEXT NOT NULL REFERENCES nodes(id),
		node_path     TEXT NOT NULL,
		type          TEXT NOT NULL,
		status        TEXT NOT NULL,
		sensitivity   TEXT NOT NULL,
		title         TEXT NOT NULL,
		summary       TEXT NOT NULL,
		tags          TEXT NOT NULL,
		body          TEXT NOT NULL,
		source_agent  TEXT NOT NULL,
		source_kind   TEXT NOT NULL,
		source_session TEXT NOT NULL,
		captured_at   TEXT NOT NULL,
		source_ref    TEXT NOT NULL,
		supersedes    TEXT NOT NULL,
		superseded_by TEXT,
		related       TEXT NOT NULL,
		created_at    TEXT NOT NULL,
		updated_at    TEXT NOT NULL,
		path          TEXT NOT NULL UNIQUE
	)`,

	`CREATE INDEX entries_node ON entries(node_id)`,
	`CREATE INDEX entries_status ON entries(status)`,

	`CREATE TABLE entry_tags (
		entry_id TEXT NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
		tag      TEXT NOT NULL,
		PRIMARY KEY (entry_id, tag)
	) WITHOUT ROWID`,
	`CREATE INDEX entry_tags_tag ON entry_tags(tag, entry_id)`,

	`CREATE VIRTUAL TABLE entries_fts USING fts5(
		title,
		summary,
		tags,
		body,
		content='entries',
		content_rowid='rowid',
		tokenize='trigram'
	)`,

	// Rune-split mirror of the entry text. unicode61 tokenizes each CJK
	// character separately, so a phrase over space-joined runes is an exact
	// substring match and one- or two-character queries — the common Chinese
	// lookup — get bm25 ranking instead of a table scan.
	`CREATE VIRTUAL TABLE entries_fts_chars USING fts5(
		title,
		summary,
		tags,
		body,
		tokenize='unicode61'
	)`,
}

func writeIndex(root string, snap *validate.Snapshot, generation int64, builtAt string) (err error) {
	dir, err := ensureIndexDir(root)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, FileName)

	existed := true
	info, statErr := os.Lstat(path)
	switch {
	case statErr == nil:
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s must not be a symbolic link", path)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s is not a regular file", path)
		}
		if err := os.Chmod(path, 0o600); err != nil {
			return err
		}
	case errors.Is(statErr, fs.ErrNotExist):
		existed = false
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		if err != nil {
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
	default:
		return statErr
	}

	db, err := openDB(path, false)
	if err != nil {
		if !existed {
			_ = removeIndexFiles(path)
		}
		return err
	}
	defer func() {
		if closeErr := db.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
		if err != nil && !existed {
			_ = removeIndexFiles(path)
		}
	}()

	ctx := context.Background()
	var journalMode string
	if err := db.QueryRowContext(ctx, "PRAGMA journal_mode=WAL").Scan(&journalMode); err != nil {
		return err
	}
	if !strings.EqualFold(journalMode, "wal") {
		return fmt.Errorf("cannot enable WAL journal mode (got %q)", journalMode)
	}
	if _, err := db.ExecContext(ctx, "PRAGMA synchronous=FULL"); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
		return err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	for _, stmt := range dropStatements {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	for _, stmt := range schemaStatements {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if err := insertSnapshot(ctx, tx, snap, generation, builtAt); err != nil {
		return err
	}
	if err := checkIndex(ctx, tx, len(snap.Entries)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func ensureIndexDir(root string) (string, error) {
	dir := filepath.Join(root, DirName)
	info, err := os.Lstat(dir)
	switch {
	case err == nil:
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("%s must not be a symbolic link", dir)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("%s is not a directory", dir)
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			return "", err
		}
		return dir, nil
	case errors.Is(err, fs.ErrNotExist):
		if err := os.Mkdir(dir, 0o700); err != nil {
			return "", err
		}
		return dir, nil
	default:
		return "", err
	}
}

func insertSnapshot(ctx context.Context, tx *sql.Tx, snap *validate.Snapshot, generation int64, builtAt string) error {
	nodeStmt, err := tx.PrepareContext(ctx, `INSERT INTO nodes
		(id, parent_id, kind, slug, path, title, summary, tags, status, sensitivity, depth, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer nodeStmt.Close()

	for _, n := range snap.Nodes {
		var parentID any
		if n.ParentID != "" {
			parentID = n.ParentID
		}
		if _, err := nodeStmt.ExecContext(ctx,
			n.ID, parentID, n.Kind, n.Slug, n.Path, n.Title, n.Summary,
			strings.Join(n.Tags, " "), n.Status, n.Sensitivity, n.Depth, n.CreatedAt, n.UpdatedAt,
		); err != nil {
			return fmt.Errorf("index node %s: %w", n.ID, err)
		}
	}

	entryStmt, err := tx.PrepareContext(ctx, `INSERT INTO entries
		(id, node_id, node_path, type, status, sensitivity, title, summary, tags, body,
		 source_agent, source_kind, source_session, captured_at, source_ref,
		 supersedes, superseded_by, related, created_at, updated_at, path)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer entryStmt.Close()

	ftsStmt, err := tx.PrepareContext(ctx,
		`INSERT INTO entries_fts(rowid, title, summary, tags, body) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer ftsStmt.Close()

	charsStmt, err := tx.PrepareContext(ctx,
		`INSERT INTO entries_fts_chars(rowid, title, summary, tags, body) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer charsStmt.Close()

	tagStmt, err := tx.PrepareContext(ctx,
		`INSERT INTO entry_tags(entry_id, tag) VALUES (?, ?)`)
	if err != nil {
		return err
	}
	defer tagStmt.Close()

	for _, e := range snap.Entries {
		var supersededBy any
		if e.SupersededBy != nil {
			supersededBy = *e.SupersededBy
		}
		tags := strings.Join(e.Tags, " ")
		res, err := entryStmt.ExecContext(ctx,
			e.ID, e.NodeID, e.NodePath, e.Type, e.Status, e.Sensitivity, e.Title, e.Summary,
			tags, e.Body, e.Source.Agent, e.Source.Kind, e.Source.Session, e.Source.CapturedAt,
			e.Source.Ref, strings.Join(e.Supersedes, " "), supersededBy,
			strings.Join(e.Related, " "), e.CreatedAt, e.UpdatedAt, e.Path,
		)
		if err != nil {
			return fmt.Errorf("index entry %s: %w", e.ID, err)
		}
		rowid, err := res.LastInsertId()
		if err != nil {
			return err
		}
		if _, err := ftsStmt.ExecContext(ctx, rowid, e.Title, e.Summary, tags, e.Body); err != nil {
			return fmt.Errorf("index entry text %s: %w", e.ID, err)
		}
		if _, err := charsStmt.ExecContext(ctx, rowid,
			charSplit(e.Title), charSplit(e.Summary), charSplit(tags), charSplit(e.Body),
		); err != nil {
			return fmt.Errorf("index entry chars %s: %w", e.ID, err)
		}
		for _, tag := range e.Tags {
			if _, err := tagStmt.ExecContext(ctx, e.ID, tag); err != nil {
				return fmt.Errorf("index entry tag %s: %w", e.ID, err)
			}
		}
	}

	metaStmt, err := tx.PrepareContext(ctx,
		`INSERT INTO meta(meta_key, meta_value) VALUES (?, ?)`)
	if err != nil {
		return err
	}
	defer metaStmt.Close()

	metaRows := [][2]string{
		{"schema_version", strconv.Itoa(schemaVersion)},
		{"repo_id", snap.Repo.RepoID},
		{"generation", strconv.FormatInt(generation, 10)},
		{"fingerprint", snap.Fingerprint},
		{"built_at", builtAt},
		{"nodes", strconv.Itoa(len(snap.Nodes))},
		{"entries", strconv.Itoa(len(snap.Entries))},
	}
	for _, row := range metaRows {
		if _, err := metaStmt.ExecContext(ctx, row[0], row[1]); err != nil {
			return err
		}
	}
	return nil
}

type execQueryer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// charSplit inserts one space between every rune so unicode61 indexes each
// CJK character as its own token and phrase queries become substring
// matches.
func charSplit(s string) string {
	runes := []rune(s)
	out := make([]rune, 0, len(runes)*2)
	for i, r := range runes {
		if i > 0 {
			out = append(out, ' ')
		}
		out = append(out, r)
	}
	return string(out)
}

func checkIndex(ctx context.Context, q execQueryer, wantEntries int) error {
	if _, err := q.ExecContext(ctx, `INSERT INTO entries_fts(entries_fts) VALUES('integrity-check')`); err != nil {
		return fmt.Errorf("FTS integrity check failed: %w", err)
	}
	var charsCount int
	if err := q.QueryRowContext(ctx, "SELECT count(*) FROM entries_fts_chars").Scan(&charsCount); err != nil {
		return fmt.Errorf("FTS integrity check failed: %w", err)
	}
	if charsCount != wantEntries {
		return fmt.Errorf("FTS integrity check failed: entries_fts_chars has %d rows, want %d", charsCount, wantEntries)
	}
	rows, err := q.QueryContext(ctx, "PRAGMA quick_check")
	if err != nil {
		return fmt.Errorf("index integrity check failed: %w", err)
	}
	defer rows.Close()

	var problems []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return fmt.Errorf("index integrity check failed: %w", err)
		}
		if line != "ok" {
			problems = append(problems, line)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("index integrity check failed: %w", err)
	}
	if len(problems) > 0 {
		return fmt.Errorf("index integrity check failed: %s", strings.Join(problems, "; "))
	}
	return nil
}
