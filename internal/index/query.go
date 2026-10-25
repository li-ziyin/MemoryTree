package index

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

var (
	// ErrEntryNotFound means no indexed entry has the requested ID.
	ErrEntryNotFound = errors.New("memory entry not found")
)

// Node is one indexed tree node with its effective tags.
type Node struct {
	ID          string   `json:"id"`
	ParentID    string   `json:"parent_id,omitempty"`
	Kind        string   `json:"kind"`
	Slug        string   `json:"slug"`
	Path        string   `json:"path"`
	Title       string   `json:"title"`
	Summary     string   `json:"summary"`
	Tags        []string `json:"tags"`
	Status      string   `json:"status"`
	Sensitivity string   `json:"sensitivity"`
	Depth       int      `json:"depth"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

// EntrySource is the provenance of an indexed entry.
type EntrySource struct {
	Agent      string `json:"agent"`
	Kind       string `json:"kind"`
	Session    string `json:"session,omitempty"`
	CapturedAt string `json:"captured_at,omitempty"`
	Ref        string `json:"ref,omitempty"`
}

// Entry is a complete indexed memory entry, including its Markdown body.
type Entry struct {
	ID           string      `json:"id"`
	NodeID       string      `json:"node_id"`
	NodePath     string      `json:"node_path"`
	NodeTitle    string      `json:"node_title"`
	Type         string      `json:"type"`
	Status       string      `json:"status"`
	Sensitivity  string      `json:"sensitivity"`
	Title        string      `json:"title"`
	Summary      string      `json:"summary"`
	Tags         []string    `json:"tags"`
	Body         string      `json:"body"`
	Source       EntrySource `json:"source"`
	Supersedes   []string    `json:"supersedes"`
	SupersededBy *string     `json:"superseded_by"`
	Related      []string    `json:"related"`
	CreatedAt    string      `json:"created_at"`
	UpdatedAt    string      `json:"updated_at"`
	Path         string      `json:"path"`
}

// EntryCount is the number of entries directly under one node.
type EntryCount struct {
	Total  int `json:"total"`
	Active int `json:"active"`
}

// Nodes returns every node ordered by slug path.
func (ix *Index) Nodes(ctx context.Context) ([]Node, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	rows, err := ix.db.QueryContext(ctx, `SELECT id, COALESCE(parent_id, ''), kind, slug, path,
		title, summary, tags, status, sensitivity, depth, created_at, updated_at
		FROM nodes ORDER BY path`)
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	defer rows.Close()

	nodes := make([]Node, 0, 64)
	for rows.Next() {
		var node Node
		var tags string
		if err := rows.Scan(
			&node.ID, &node.ParentID, &node.Kind, &node.Slug, &node.Path,
			&node.Title, &node.Summary, &tags, &node.Status, &node.Sensitivity,
			&node.Depth, &node.CreatedAt, &node.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("list nodes: %w", err)
		}
		node.Tags = strings.Fields(tags)
		if node.Tags == nil {
			node.Tags = []string{}
		}
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	return nodes, nil
}

// Entry returns one complete entry by ID.
func (ix *Index) Entry(ctx context.Context, id string) (*Entry, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var entry Entry
	var tags, supersedes, related string
	var supersededBy sql.NullString
	err := ix.db.QueryRowContext(ctx, `SELECT e.id, e.node_id, e.node_path, n.title,
		e.type, e.status, e.sensitivity, e.title, e.summary, e.tags, e.body,
		e.source_agent, e.source_kind, e.source_session, e.captured_at, e.source_ref,
		e.supersedes, e.superseded_by, e.related, e.created_at, e.updated_at, e.path
		FROM entries e JOIN nodes n ON n.id = e.node_id
		WHERE e.id = ?`, id).Scan(
		&entry.ID, &entry.NodeID, &entry.NodePath, &entry.NodeTitle,
		&entry.Type, &entry.Status, &entry.Sensitivity, &entry.Title, &entry.Summary,
		&tags, &entry.Body,
		&entry.Source.Agent, &entry.Source.Kind, &entry.Source.Session,
		&entry.Source.CapturedAt, &entry.Source.Ref,
		&supersedes, &supersededBy, &related, &entry.CreatedAt, &entry.UpdatedAt, &entry.Path,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrEntryNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load entry: %w", err)
	}
	entry.Tags = fieldsOrEmpty(tags)
	entry.Supersedes = fieldsOrEmpty(supersedes)
	entry.Related = fieldsOrEmpty(related)
	if supersededBy.Valid && supersededBy.String != "" {
		id := supersededBy.String
		entry.SupersededBy = &id
	}
	return &entry, nil
}

// EntryCounts returns entry counts keyed by node ID.
func (ix *Index) EntryCounts(ctx context.Context) (map[string]EntryCount, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	rows, err := ix.db.QueryContext(ctx, `SELECT node_id, COUNT(*),
		COALESCE(SUM(CASE WHEN status = 'active' THEN 1 ELSE 0 END), 0)
		FROM entries GROUP BY node_id`)
	if err != nil {
		return nil, fmt.Errorf("count entries: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]EntryCount)
	for rows.Next() {
		var nodeID string
		var count EntryCount
		if err := rows.Scan(&nodeID, &count.Total, &count.Active); err != nil {
			return nil, fmt.Errorf("count entries: %w", err)
		}
		counts[nodeID] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("count entries: %w", err)
	}
	return counts, nil
}

func fieldsOrEmpty(value string) []string {
	fields := strings.Fields(value)
	if fields == nil {
		return []string{}
	}
	return fields
}
