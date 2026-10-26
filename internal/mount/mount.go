// Package mount ranks host nodes for a new memory entry (M3). Scoring is
// deterministic and explainable: tag coverage plus CJK-aware text
// similarity, with a prior that user-profile entries belong under global.
// Suggest only ranks candidates — the calling agent makes the final call.
package mount

import (
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Query is the entry profile to find a host node for. Tags are normalized
// (lowercase) by the caller; Suggest does not re-normalize.
type Query struct {
	Type    string
	Title   string
	Summary string
	Tags    []string
}

// EntryLite is the part of an existing entry that feeds node similarity.
type EntryLite struct {
	Title   string
	Summary string
	Tags    []string
}

// CandidateNode is one possible host as seen by the scorer.
type CandidateNode struct {
	ID          string
	Path        string
	Kind        string
	Slug        string
	Title       string
	Summary     string
	Tags        []string // effective tags
	Status      string
	Sensitivity string
	Entries     []EntryLite
}

// Suggestion is one ranked candidate with its evidence.
type Suggestion struct {
	NodeID        string   `json:"node_id"`
	Path          string   `json:"path"`
	Kind          string   `json:"kind"`
	Title         string   `json:"title"`
	Summary       string   `json:"summary"`
	Sensitivity   string   `json:"sensitivity"`
	EntryCount    int      `json:"entry_count"`
	Score         float64  `json:"score"`
	TagScore      float64  `json:"tag_score"`
	TextScore     float64  `json:"text_score"`
	MatchedTags   []string `json:"matched_tags"`
	MatchedTokens []string `json:"matched_tokens"`
	Reasons       []string `json:"reasons"`
}

// Field weights for text matching: a token counts at the weight of the
// strongest field it appears in.
const (
	wNodeTitle  = 3.0
	wNodeSumry  = 1.0
	wTag        = 2.0
	wEntryTitle = 1.5
	wEntrySumry = 0.5
	// wPriorUser is the bonus for global when the entry type is user:
	// the format pins user-profile entries under global (DESIGN 5.2).
	wPriorUser = 0.5
)

// Suggest ranks entry-hosting nodes (project or global) for q and returns at
// most limit candidates, best first. Ties break by path so output is stable.
func Suggest(nodes []CandidateNode, q Query, limit int) []Suggestion {
	if limit <= 0 {
		limit = 5
	}
	queryTags := setOf(q.Tags)
	queryTokens := setOf(append(tokenize(q.Title), tokenize(q.Summary)...))

	out := make([]Suggestion, 0, len(nodes))
	for _, node := range nodes {
		if !hostsEntries(node) {
			continue
		}
		s := scoreNode(node, q, queryTags, queryTokens)
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Path < out[j].Path
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func hostsEntries(n CandidateNode) bool {
	if n.Status != "active" {
		return false
	}
	return n.Kind == "project" || n.Slug == "global"
}

func scoreNode(node CandidateNode, q Query, queryTags, queryTokens map[string]struct{}) Suggestion {
	// Tag pool: the node's effective tags plus every sampled entry's tags.
	tagPool := setOf(node.Tags)
	for _, e := range node.Entries {
		for _, tag := range e.Tags {
			tagPool[tag] = struct{}{}
		}
	}
	matchedTags := make([]string, 0, len(queryTags))
	for tag := range queryTags {
		if _, ok := tagPool[tag]; ok {
			matchedTags = append(matchedTags, tag)
		}
	}
	sort.Strings(matchedTags)
	tagScore := 0.0
	if len(queryTags) > 0 {
		tagScore = float64(len(matchedTags)) / float64(len(queryTags))
	}

	// Text pool: token -> weight of the strongest matching field.
	weights := map[string]float64{}
	addField(weights, node.Title, wNodeTitle)
	addField(weights, node.Summary, wNodeSumry)
	for _, tag := range node.Tags {
		for _, tok := range tokenize(tag) {
			if w := weights[tok]; w < wTag {
				weights[tok] = wTag
			}
		}
	}
	for _, e := range node.Entries {
		addField(weights, e.Title, wEntryTitle)
		addField(weights, e.Summary, wEntrySumry)
	}

	matchedTokens := make([]string, 0, len(queryTokens))
	var weighted float64
	for tok := range queryTokens {
		w, ok := weights[tok]
		if !ok {
			continue
		}
		matchedTokens = append(matchedTokens, tok)
		weighted += w
	}
	sort.Strings(matchedTokens)
	textScore := 0.0
	if len(queryTokens) > 0 {
		// Normalize by the number of query tokens at node-title weight so
		// textScore stays in [0,1] no matter how strong the field hits are.
		textScore = weighted / (float64(len(queryTokens)) * wNodeTitle)
		if textScore > 1 {
			textScore = 1
		}
	}

	score := 0.45*tagScore + 0.45*textScore
	var reasons []string
	if len(matchedTags) > 0 {
		reasons = append(reasons, "shared tags: "+strings.Join(matchedTags, ", "))
	}
	if len(matchedTokens) > 0 {
		top := matchedTokens
		if len(top) > 5 {
			top = top[:5]
		}
		reasons = append(reasons, "text overlap: "+strings.Join(top, " "))
	}
	if len(node.Entries) > 0 {
		reasons = append(reasons, plural(len(node.Entries), "existing entry", "existing entries")+" under this node")
	}
	if node.Slug == "global" && q.Type == "user" {
		score += wPriorUser
		reasons = append(reasons, "user-profile entries belong under global")
	}
	if len(reasons) == 0 {
		reasons = append(reasons, "no tag or text overlap; ranked only by default order")
	}

	return Suggestion{
		NodeID:        node.ID,
		Path:          node.Path,
		Kind:          node.Kind,
		Title:         node.Title,
		Summary:       node.Summary,
		Sensitivity:   node.Sensitivity,
		EntryCount:    len(node.Entries),
		Score:         round3(score),
		TagScore:      round3(tagScore),
		TextScore:     round3(textScore),
		MatchedTags:   matchedTags,
		MatchedTokens: matchedTokens,
		Reasons:       reasons,
	}
}

func addField(weights map[string]float64, text string, weight float64) {
	for _, tok := range tokenize(text) {
		if w := weights[tok]; w < weight {
			weights[tok] = weight
		}
	}
}

// tokenize lowercases ASCII words into single tokens and splits every
// non-ASCII letter or digit (CJK in practice) into one token per rune, so
// Chinese titles and summaries match on shared characters.
func tokenize(s string) []string {
	var tokens []string
	var word []rune
	flush := func() {
		if len(word) > 0 {
			tokens = append(tokens, strings.ToLower(string(word)))
			word = word[:0]
		}
	}
	for _, r := range s {
		switch {
		case r < 0x80 && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			word = append(word, r)
		default:
			flush()
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				tokens = append(tokens, string(r))
			}
		}
	}
	flush()
	return tokens
}

func setOf(items []string) map[string]struct{} {
	if len(items) == 0 {
		return map[string]struct{}{}
	}
	out := make(map[string]struct{}, len(items))
	for _, item := range items {
		if item = strings.TrimSpace(item); item != "" {
			out[strings.ToLower(item)] = struct{}{}
		}
	}
	return out
}

func round3(v float64) float64 {
	return float64(int(v*1000+0.5)) / 1000
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}
