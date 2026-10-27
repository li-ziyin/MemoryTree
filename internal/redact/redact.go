// Package redact scans text for credential formats and machine-local
// session state, replacing both with [REDACTED] before anything is
// persisted — in captured raw text and in written memory entries. It never
// records the originals; callers only learn the replacement count.
//
// The pattern list is deliberately conservative. Credentials target shapes
// that are never legitimate prose (cloud access-key IDs, platform tokens,
// private-key blocks, Authorization headers) plus labeled fields
// ("password: …") whose value is long enough to be a real secret.
//
// Session state covers identifiers that pin text to one machine: absolute
// paths anchored at a home directory, HOME/USERPROFILE assignments,
// dotless hostnames in labeled fields (host:, hostname:, COMPUTERNAME=),
// and dotless user@host prompt tokens. The false-positive policy is:
// repository-relative references, URLs, email addresses, and dotted
// (qualified) hostnames always stay — a hostname value is only treated as
// machine-local when it contains no dot, and only paths under /home/, /Users/,
// /root/, or a drive letter's Users|home|root directory are rewritten.
package redact

import "regexp"

// Marker replaces every detected secret or machine-local identifier.
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

// stateRules match machine-local session identifiers, each with the
// replacement template that restores its captured boundary groups. RE2
// has no lookaround, so the patterns capture their leading or trailing
// boundary instead; that keeps matches from starting or stopping inside a
// longer token such as a URL. They run before homePathPattern so an
// assignment consumes its whole value in one replacement.
var stateRules = []struct {
	rx   *regexp.Regexp
	repl string
}{
	// HOME=/home/me, USERPROFILE=C:\Users\me — any value shape.
	{regexp.MustCompile(`(?i)\b(home|userprofile)\b([ \t]*[:=][ \t]*)("[^"\n]*"|'[^'\n]*'|[^\s"',;]+)`),
		"${1}${2}" + Marker},
	// host:/hostname:/COMPUTERNAME= with a dotless value; a following dot
	// (qualified name or IP) fails the trailing group, so host: example.com
	// and host: 127.0.0.1 stay.
	{regexp.MustCompile(`(?i)\b(host|hostname|computername)\b([ \t]*[:=][ \t]*)("[A-Za-z0-9-]+"|'[A-Za-z0-9-]+'|[A-Za-z0-9][A-Za-z0-9-]{1,})(\.(?:\s|$)|[^.\w]|$)`),
		"${1}${2}" + Marker + "${4}"},
	// user@host prompt tokens with a dotless host; emails and ssh targets
	// with dotted domains never match because the trailing group rejects a
	// dot followed by more host characters.
	{regexp.MustCompile(`([A-Za-z0-9][A-Za-z0-9._%+-]*@[A-Za-z0-9][A-Za-z0-9-]*)(\.(?:\s|$)|$|[^.\w])`),
		Marker + "${2}"},
}

// homePathPattern matches absolute paths anchored at a home directory:
// Windows drive roots under Users|home|root and the Unix /home/, /Users/,
// /root/ prefixes. The leading group keeps the boundary character, so a
// path glued into a URL tail (https://x.com/home/me) does not match, while
// a standalone or quoted path does. Repository-relative references have no
// home anchor and never match.
var homePathPattern = regexp.MustCompile(`(?m)(^|[^\w.@~/\\-])(?:[A-Za-z]:[\\/](?i:users|home|root)[\\/]|/(?:home|[Uu]sers|root)/)[^\s"'` + "`" + `<>\[\](),;:]+`)

// fieldPattern matches `label: value` assignments for credential-bearing
// labels. Only the value is replaced; the label and separator stay
// readable. Values shorter than 6 characters (unquoted) or 4 (quoted) are
// considered prose and left alone, as are values that already look like
// the Marker (they cannot match the character class).
var fieldPattern = regexp.MustCompile(`(?i)\b(password|passwd|pwd|secret|token|api[_-]?key)\b(\s*[:=]\s*)(?:"[A-Za-z0-9_\-+/=.]{4,}"|'[A-Za-z0-9_\-+/=.]{4,}'|[A-Za-z0-9_\-+/=.]{6,})`)

// Scan returns s with every detected secret or machine-local identifier
// replaced by Marker and how many replacements happened.
func Scan(s string) (string, int) {
	count := 0
	for _, rx := range tokenPatterns {
		if n := len(rx.FindAllString(s, -1)); n > 0 {
			count += n
			s = rx.ReplaceAllString(s, Marker)
		}
	}
	for _, rule := range stateRules {
		if n := len(rule.rx.FindAllString(s, -1)); n > 0 {
			count += n
			s = rule.rx.ReplaceAllString(s, rule.repl)
		}
	}
	if n := len(homePathPattern.FindAllStringSubmatch(s, -1)); n > 0 {
		count += n
		s = homePathPattern.ReplaceAllString(s, "${1}"+Marker)
	}
	if n := len(fieldPattern.FindAllString(s, -1)); n > 0 {
		count += n
		s = fieldPattern.ReplaceAllString(s, "${1}${2}"+Marker)
	}
	return s, count
}
