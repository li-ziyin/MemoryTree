// Package redact scans text for common credential formats and replaces
// them with [REDACTED] before anything is persisted — in captured raw
// text and in written memory entries. It never records the originals;
// callers only learn the replacement count.
//
// The pattern list is deliberately conservative: it targets shapes that
// are never legitimate prose (cloud access-key IDs, platform tokens,
// private-key blocks, Authorization headers) plus labeled fields
// ("password: …") whose value is long enough to be a real secret.
package redact

import "regexp"

// Marker replaces every detected secret.
const Marker = "[REDACTED]"

// tokenPatterns match self-contained secret shapes; the whole match is
// replaced. Private-key blocks run first so their base64 bodies cannot
// feed the shorter token patterns.
var tokenPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`),
	regexp.MustCompile(`github_pat_[A-Za-z0-9_]{22,}`),
	regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{36,255}`),
	regexp.MustCompile(`glpat-[A-Za-z0-9_\-]{20,}`),
	regexp.MustCompile(`\b(?:AKIA|ASIA|ABIA|ACCA)[A-Z0-9]{16}\b`),
	regexp.MustCompile(`LTAI[0-9A-Za-z]{12,24}`),
	regexp.MustCompile(`AKID[0-9A-Za-z]{13,40}`),
	regexp.MustCompile(`AIza[0-9A-Za-z_\-]{35}`),
	regexp.MustCompile(`xox[baprs]-[0-9A-Za-z\-]{10,250}`),
	regexp.MustCompile(`sk-[A-Za-z0-9_\-]{20,}`),
	regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9\-._~+/]{8,}={0,2}`),
}

// fieldPattern matches `label: value` assignments for credential-bearing
// labels. Only the value is replaced; the label and separator stay
// readable. Values shorter than 6 characters (unquoted) or 4 (quoted) are
// considered prose and left alone, as are values that already look like
// the Marker (they cannot match the character class).
var fieldPattern = regexp.MustCompile(`(?i)\b(password|passwd|pwd|secret|token|api[_-]?key)\b(\s*[:=]\s*)(?:"[A-Za-z0-9_\-+/=.]{4,}"|'[A-Za-z0-9_\-+/=.]{4,}'|[A-Za-z0-9_\-+/=.]{6,})`)

// Scan returns s with every detected secret replaced by Marker and how
// many replacements happened.
func Scan(s string) (string, int) {
	count := 0
	for _, rx := range tokenPatterns {
		if n := len(rx.FindAllString(s, -1)); n > 0 {
			count += n
			s = rx.ReplaceAllString(s, Marker)
		}
	}
	if n := len(fieldPattern.FindAllString(s, -1)); n > 0 {
		count += n
		s = fieldPattern.ReplaceAllString(s, "${1}${2}"+Marker)
	}
	return s, count
}
