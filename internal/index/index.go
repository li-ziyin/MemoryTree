// Package index builds and queries the disposable SQLite search index of a
// MemoryTree data repository. Markdown files remain the source of truth: the
// index holds no unique data and can always be rebuilt from a validated
// snapshot.
package index

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/li-ziyin/memorytree/internal/validate"
)

const (
	// DirName is the reserved directory that holds the index.
	DirName = ".index"
	// FileName is the SQLite database file inside DirName.
	FileName = "index.sqlite"

	schemaVersion = 2
)

var (
	// ErrInvalidRepository means validation found blocking issues and the
	// index was left untouched.
	ErrInvalidRepository = errors.New("repository has validation errors")
	// ErrNoIndex means no index has been built for the data repository.
	ErrNoIndex = errors.New("search index does not exist")
	// ErrIndexCorrupt means the existing index cannot be read or fails an
	// integrity check. An explicit rebuild is required to replace it.
	ErrIndexCorrupt = errors.New("search index is unreadable or corrupt")
	// ErrIndexIncompatible means the index was built with another schema
	// version by a different memoryd version.
	ErrIndexIncompatible = errors.New("search index schema is not supported")
)

// BuildOptions configures Build.
type BuildOptions struct {
	// Rebuild forces a full rebuild even when the source fingerprint has not
	// changed.
	Rebuild bool
	// Now overrides the clock in tests. Zero means time.Now.
	Now time.Time
}

// BuildResult describes what Build did.
type BuildResult struct {
	Root       string `json:"root"`
	Path       string `json:"path"`
	Generation int64  `json:"generation"`
	Nodes      int    `json:"nodes"`
	Entries    int    `json:"entries"`
	Rebuilt    bool   `json:"rebuilt"`
	UpToDate   bool   `json:"up_to_date"`
	Warnings   int    `json:"warnings"`

	// Validation carries the validation report for error handling. It is not
	// part of the JSON output.
	Validation *validate.Result `json:"-"`
}

// Meta is the index metadata stored alongside the search tables.
type Meta struct {
	SchemaVersion int    `json:"schema_version"`
	RepoID        string `json:"repo_id"`
	Generation    int64  `json:"generation"`
	Fingerprint   string `json:"fingerprint"`
	BuiltAt       string `json:"built_at"`
	Nodes         int    `json:"nodes"`
	Entries       int    `json:"entries"`
}

// Path returns the index database path for a data repository.
func Path(dataDir string) string {
	return filepath.Join(dataDir, DirName, FileName)
}

// Build validates the repository and updates its index. Validation errors are
// returned as ErrInvalidRepository with the report in the result and leave any
// existing index untouched. A changed source fingerprint triggers a full
// rebuild in one transaction; an unchanged fingerprint is a no-op unless
// opts.Rebuild is set.
func Build(dataDir string, opts BuildOptions) (*BuildResult, error) {
	root, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, err
	}
	res, snap, err := validate.ValidateWithSnapshot(root)
	if err != nil {
		return nil, err
	}
	result := &BuildResult{
		Root:       root,
		Path:       Path(root),
		Warnings:   res.Warnings(),
		Validation: res,
	}
	if !res.OK() {
		return result, ErrInvalidRepository
	}

	nextGeneration := int64(1)
	meta, metaErr := readMeta(result.Path)
	switch {
	case metaErr == nil:
		nextGeneration = meta.Generation + 1
		if meta.SchemaVersion != schemaVersion && !opts.Rebuild {
			return result, fmt.Errorf("%w: found schema %d, want %d; run memoryd index --rebuild",
				ErrIndexIncompatible, meta.SchemaVersion, schemaVersion)
		}
		if meta.SchemaVersion == schemaVersion && meta.Fingerprint == snap.Fingerprint && !opts.Rebuild {
			result.Generation = meta.Generation
			result.Nodes = meta.Nodes
			result.Entries = meta.Entries
			result.UpToDate = true
			return result, nil
		}
	case errors.Is(metaErr, fs.ErrNotExist):
		// No index yet.
	default:
		if !opts.Rebuild {
			return result, fmt.Errorf("%w: %v; run memoryd index --rebuild to replace it", ErrIndexCorrupt, metaErr)
		}
		if err := removeIndexFiles(result.Path); err != nil {
			return result, err
		}
	}

	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	builtAt := opts.Now.UTC().Format(time.RFC3339)
	if err := writeIndex(root, snap, nextGeneration, builtAt); err != nil {
		return result, err
	}

	result.Generation = nextGeneration
	result.Nodes = len(snap.Nodes)
	result.Entries = len(snap.Entries)
	result.Rebuilt = true
	return result, nil
}

// Index is a read-only handle to a built index.
type Index struct {
	db   *sql.DB
	path string
}

// Open opens an existing index for querying. It fails with ErrNoIndex when no
// index has been built and with ErrIndexCorrupt when the file cannot be read.
func Open(dataDir string) (*Index, error) {
	root, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, err
	}
	path := Path(root)
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrNoIndex, path)
		}
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%w: index file is a symbolic link", ErrIndexCorrupt)
	}

	db, err := openDB(path, true)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrIndexCorrupt, err)
	}
	meta, err := metaFromDB(context.Background(), db)
	if err != nil {
		db.Close()
		return nil, err
	}
	if meta.SchemaVersion != schemaVersion {
		db.Close()
		return nil, fmt.Errorf("%w: found schema %d, want %d; run memoryd index --rebuild",
			ErrIndexIncompatible, meta.SchemaVersion, schemaVersion)
	}
	return &Index{db: db, path: path}, nil
}

// Close releases the database handle.
func (ix *Index) Close() error {
	return ix.db.Close()
}

// Path returns the database path this index was opened from.
func (ix *Index) Path() string {
	return ix.path
}

// Meta returns the index metadata.
func (ix *Index) Meta() (*Meta, error) {
	return metaFromDB(context.Background(), ix.db)
}

func readMeta(path string) (*Meta, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%w: index file is a symbolic link", ErrIndexCorrupt)
	}
	db, err := openDB(path, true)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrIndexCorrupt, err)
	}
	defer db.Close()
	return metaFromDB(context.Background(), db)
}

func metaFromDB(ctx context.Context, db *sql.DB) (*Meta, error) {
	var check string
	if err := db.QueryRowContext(ctx, "PRAGMA quick_check(1)").Scan(&check); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrIndexCorrupt, err)
	}
	if check != "ok" {
		return nil, fmt.Errorf("%w: %s", ErrIndexCorrupt, check)
	}

	rows, err := db.QueryContext(ctx, "SELECT meta_key, meta_value FROM meta")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrIndexCorrupt, err)
	}
	defer rows.Close()

	values := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrIndexCorrupt, err)
		}
		values[key] = value
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrIndexCorrupt, err)
	}

	meta := &Meta{
		RepoID:      values["repo_id"],
		Fingerprint: values["fingerprint"],
		BuiltAt:     values["built_at"],
	}
	if meta.SchemaVersion, err = strconv.Atoi(values["schema_version"]); err != nil {
		return nil, fmt.Errorf("%w: missing or invalid schema_version", ErrIndexCorrupt)
	}
	if meta.Generation, err = strconv.ParseInt(values["generation"], 10, 64); err != nil || meta.Generation < 1 {
		return nil, fmt.Errorf("%w: missing or invalid generation", ErrIndexCorrupt)
	}
	if meta.Nodes, err = strconv.Atoi(values["nodes"]); err != nil || meta.Nodes < 0 {
		return nil, fmt.Errorf("%w: missing or invalid node count", ErrIndexCorrupt)
	}
	if meta.Entries, err = strconv.Atoi(values["entries"]); err != nil || meta.Entries < 0 {
		return nil, fmt.Errorf("%w: missing or invalid entry count", ErrIndexCorrupt)
	}
	if meta.RepoID == "" {
		return nil, fmt.Errorf("%w: missing repo_id", ErrIndexCorrupt)
	}
	if raw, err := hex.DecodeString(meta.Fingerprint); err != nil || len(raw) != 32 {
		return nil, fmt.Errorf("%w: invalid source fingerprint", ErrIndexCorrupt)
	}
	return meta, nil
}

func openDB(path string, readOnly bool) (*sql.DB, error) {
	params := url.Values{}
	params.Set("_busy_timeout", "5000")
	if readOnly {
		params.Set("_query_only", "1")
	}
	db, err := sql.Open("sqlite", sqliteDSN(path, params))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func sqliteDSN(path string, params url.Values) string {
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	u := url.URL{Scheme: "file", Path: p, RawQuery: params.Encode()}
	return u.String()
}

func removeIndexFiles(path string) error {
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(path + suffix); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}
