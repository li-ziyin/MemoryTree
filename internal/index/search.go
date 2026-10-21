package index

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/li-ziyin/memorytree/internal/model"
)

const (
	// DefaultLimit is the result cap when SearchOptions.Limit is zero.
	DefaultLimit = 20
	// MaxLimit is the largest accepted result cap.
	MaxLimit = 100
)

// SearchOptions selects and filters entries.
type SearchOptions struct {
	// Query is matched against title, summary, tags, and body. An empty query
	// lists entries that match the filters.
	Query string
	// NodeID restricts results to entries directly under one node.
	NodeID string
	// Types restricts results to the given entry types when non-empty.
	Types []string
	// Statuses restricts results when non-empty; otherwise only active
	// entries are returned.
	Statuses []string
	// Tags requires every listed effective tag.
	Tags []string
	// Limit caps the result count. Zero means DefaultLimit; values above
	// MaxLimit are clamped.
	Limit int
}

// Hit is one search result.
type Hit struct {
	ID          string   `json:"id"`
	NodeID      string   `json:"node_id"`
	NodePath    string   `json:"node_path"`
	NodeTitle   string   `json:"node_title"`
	Type        string   `json:"type"`
	Status      string   `json:"status"`
	Sensitivity string   `json:"sensitivity"`
	Title       string   `json:"title"`
	Summary     string   `json:"summary"`
	Tags        []string `json:"tags"`
	SourceAgent string   `json:"source_agent"`
	SourceKind  string   `json:"source_kind"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
	Path        string   `json:"path"`
}

// Search queries the index. Queries whose tokens are all at least three
// characters long use the FTS5 trigram index; shorter tokens fall back to a
// LIKE scan because trigram indexes cannot match them.
func (ix *Index) Search(ctx context.Context, opts SearchOptions) ([]Hit, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if opts.Limit <= 0 {
		opts.Limit = DefaultLimit
	}
	if opts.Limit > MaxLimit {
		opts.Limit = MaxLimit
	}

	conditions, filterArgs := filterConditions(opts)
	queryText := norm.NFC.String(strings.TrimSpace(opts.Query))
	tokens := strings.Fields(queryText)

	const selectColumns = `SELECT e.id, e.node_id, e.node_path, n.title, e.type, e.status,
		e.sensitivity, e.title, e.summary, e.tags, e.source_agent, e.source_kind,
		e.created_at, e.updated_at, e.path
		FROM %s JOIN nodes n ON n.id = e.node_id`

	var (
		query string
		args  []any
	)
	switch {
	case len(tokens) == 0:
		query = fmt.Sprintf(selectColumns, "entries e")
		query += whereClause(conditions)
		query += " ORDER BY e.updated_at DESC, e.id LIMIT ?"
		args = append(args, filterArgs...)
	case ftsEligible(tokens):
		query = fmt.Sprintf(selectColumns, "entries_fts JOIN entries e ON e.rowid = entries_fts.rowid")
		query += " WHERE entries_fts MATCH ?"
		args = append(args, ftsExpression(tokens))
		for _, condition := range conditions {
			query += " AND " + condition
		}
		args = append(args, filterArgs...)
		query += " ORDER BY bm25(entries_fts), e.updated_at DESC, e.id LIMIT ?"
	default:
		query = fmt.Sprintf(selectColumns, "entries e")
		likeConditions, likeArgs := likeConditions(tokens)
		conditions = append(conditions, likeConditions...)
		query += whereClause(conditions)
		query += " ORDER BY e.updated_at DESC, e.id LIMIT ?"
		args = append(args, filterArgs...)
		args = append(args, likeArgs...)
	}
	args = append(args, opts.Limit)

	rows, err := ix.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("search index: %w", err)
	}
	defer rows.Close()

	hits := make([]Hit, 0, opts.Limit)
	for rows.Next() {
		var hit Hit
		var tags string
		if err := rows.Scan(
			&hit.ID, &hit.NodeID, &hit.NodePath, &hit.NodeTitle, &hit.Type, &hit.Status,
			&hit.Sensitivity, &hit.Title, &hit.Summary, &tags, &hit.SourceAgent,
			&hit.SourceKind, &hit.CreatedAt, &hit.UpdatedAt, &hit.Path,
		); err != nil {
			return nil, fmt.Errorf("search index: %w", err)
		}
		hit.Tags = strings.Fields(tags)
		if hit.Tags == nil {
			hit.Tags = []string{}
		}
		hits = append(hits, hit)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search index: %w", err)
	}
	return hits, nil
}

func filterConditions(opts SearchOptions) ([]string, []any) {
	var conditions []string
	var args []any

	if len(opts.Statuses) == 0 {
		conditions = append(conditions, "e.status = ?")
		args = append(args, model.StatusActive)
	} else {
		conditions = append(conditions, inClause("e.status", len(opts.Statuses)))
		for _, status := range opts.Statuses {
			args = append(args, status)
		}
	}
	if len(opts.Types) > 0 {
		conditions = append(conditions, inClause("e.type", len(opts.Types)))
		for _, typ := range opts.Types {
			args = append(args, typ)
		}
	}
	if opts.NodeID != "" {
		conditions = append(conditions, "e.node_id = ?")
		args = append(args, opts.NodeID)
	}
	for _, tag := range opts.Tags {
		conditions = append(conditions,
			"EXISTS (SELECT 1 FROM entry_tags t WHERE t.entry_id = e.id AND t.tag = ?)")
		args = append(args, tag)
	}
	return conditions, args
}

func inClause(column string, count int) string {
	placeholders := make([]string, count)
	for i := range placeholders {
		placeholders[i] = "?"
	}
	return column + " IN (" + strings.Join(placeholders, ", ") + ")"
}

func whereClause(conditions []string) string {
	if len(conditions) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(conditions, " AND ")
}

func ftsEligible(tokens []string) bool {
	if len(tokens) == 0 {
		return false
	}
	for _, token := range tokens {
		if utf8.RuneCountInString(token) < 3 {
			return false
		}
	}
	return true
}

func ftsExpression(tokens []string) string {
	quoted := make([]string, 0, len(tokens))
	for _, token := range tokens {
		quoted = append(quoted, `"`+strings.ReplaceAll(token, `"`, `""`)+`"`)
	}
	return strings.Join(quoted, " AND ")
}

func likeConditions(tokens []string) ([]string, []any) {
	conditions := make([]string, 0, len(tokens))
	args := make([]any, 0, len(tokens)*3)
	for _, token := range tokens {
		pattern := likePattern(token)
		conditions = append(conditions,
			`(e.title LIKE ? ESCAPE '\' OR e.summary LIKE ? ESCAPE '\' OR e.body LIKE ? ESCAPE '\')`)
		args = append(args, pattern, pattern, pattern)
	}
	return conditions, args
}

func likePattern(s string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
	return "%" + escaped + "%"
}
