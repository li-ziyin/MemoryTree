package mcpserver

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	defaultExcerptChars = 1200
	minExcerptChars     = 200
	maxExcerptChars     = 8000
)

// excerptBody returns at most maxChars runes of body around matches of query.
// The boolean is false when the query does not occur and the beginning of the
// body is returned instead.
func excerptBody(body, query string, maxChars int) (string, bool) {
	runes := []rune(body)
	if maxChars <= 0 || len(runes) <= maxChars {
		return body, true
	}
	tokens := strings.Fields(query)
	if len(tokens) == 0 {
		return string(runes[:maxChars]), true
	}
	quoted := make([]string, len(tokens))
	for i, token := range tokens {
		quoted[i] = regexp.QuoteMeta(token)
	}
	re, err := regexp.Compile("(?i)" + strings.Join(quoted, "|"))
	if err != nil {
		return string(runes[:maxChars]), true
	}
	locs := re.FindAllStringIndex(body, 64)
	if len(locs) == 0 {
		return string(runes[:maxChars]), false
	}

	type span struct{ start, end int }
	width := maxChars / min(len(locs), 5)
	if width < minExcerptChars {
		width = minExcerptChars
	}
	if width > maxChars {
		width = maxChars
	}
	spans := make([]span, 0, len(locs))
	for _, loc := range locs {
		start := utf8.RuneCountInString(body[:loc[0]])
		end := start + utf8.RuneCountInString(body[loc[0]:loc[1]])
		from := start - width/3
		if from < 0 {
			from = 0
		}
		to := from + width
		if to < end {
			to = end
		}
		if to > len(runes) {
			to = len(runes)
			from = to - width
			if from < 0 {
				from = 0
			}
		}
		if n := len(spans); n > 0 && from <= spans[n-1].end+40 {
			if to > spans[n-1].end {
				spans[n-1].end = to
			}
			continue
		}
		spans = append(spans, span{from, to})
	}

	var b strings.Builder
	used := 0
	lastEnd := 0
	for i, sp := range spans {
		if i == 0 && sp.start > 0 {
			b.WriteString("…")
		}
		if i > 0 {
			b.WriteString("\n…\n")
		}
		text := []rune(string(runes[sp.start:sp.end]))
		room := maxChars - used
		if room <= 0 {
			break
		}
		if len(text) > room {
			text = text[:room]
		}
		b.WriteString(string(text))
		used += len(text)
		lastEnd = sp.start + len(text)
		if lastEnd < sp.end {
			break
		}
	}
	if lastEnd < len(runes) {
		b.WriteString("…")
	}
	out := []rune(b.String())
	if len(out) > maxChars {
		out = out[:maxChars]
		out[maxChars-1] = '…'
	}
	if len(out) == 0 {
		return string(runes[:maxChars]), true
	}
	return string(out), true
}
