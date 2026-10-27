// Package bench reruns retrieval questions against a built index and
// reports first-hit rank, giving ranking changes a regression gate.
package bench

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/li-ziyin/memorytree/internal/index"
	"github.com/li-ziyin/memorytree/internal/rank"
	"github.com/li-ziyin/memorytree/internal/tokens"
)

// Question is one retrieval question with its expected gold entry.
type Question struct {
	ID       int    `json:"id"`
	Query    string `json:"query"`
	ExpectID string `json:"expect_id"`
	Limit    int    `json:"limit,omitempty"`
}

// Result is the outcome of one question.
type Result struct {
	ID       int    `json:"id"`
	Query    string `json:"query"`
	Rank     int    `json:"rank"` // 1-based; 0 means the gold entry was not returned
	Hits     int    `json:"hits"`
	Tokens   int    `json:"tokens_estimated"`
	TopID    string `json:"top_id"`
	TopTitle string `json:"top_title"`
}

// Run executes all questions and returns their results in order. A non-nil
// heat re-ranks every result set exactly the way the MCP server does, so
// the regression gate can exercise production ranking against the real
// usage log (memoryd bench --usage) or synthetic bursts in tests.
func Run(dataDir string, questions []Question, heat rank.HeatOf) ([]Result, error) {
	ix, err := index.Open(dataDir)
	if err != nil {
		return nil, err
	}
	defer ix.Close()
	results := make([]Result, 0, len(questions))
	for _, q := range questions {
		limit := q.Limit
		if limit <= 0 {
			limit = 5
		}
		hits, err := ix.Search(context.Background(), index.SearchOptions{Query: q.Query, Limit: limit})
		if err != nil {
			return nil, fmt.Errorf("question %d: %w", q.ID, err)
		}
		if heat != nil {
			hits = rank.Apply(hits, heat)
		}
		rank := 0
		topID, topTitle := "", ""
		for i, h := range hits {
			if i == 0 {
				topID = h.ID
				topTitle = h.Title
			}
			if h.ID == q.ExpectID {
				rank = i + 1
				break
			}
		}
		results = append(results, Result{
			ID: q.ID, Query: q.Query, Rank: rank, Hits: len(hits),
			Tokens: tokens.Estimate(hits), TopID: topID, TopTitle: topTitle,
		})
	}
	return results, nil
}

// LoadQuestions reads a JSONL file; blank lines and #-comments are skipped.
func LoadQuestions(path string) ([]Question, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Question
	sc := bufio.NewScanner(f)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		var q Question
		if err := json.Unmarshal([]byte(text), &q); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, line, err)
		}
		if q.Query == "" || q.ExpectID == "" {
			return nil, fmt.Errorf("%s:%d: question needs query and expect_id", path, line)
		}
		out = append(out, q)
	}
	return out, sc.Err()
}
