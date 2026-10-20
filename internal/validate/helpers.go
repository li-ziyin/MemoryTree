package validate

import (
	"regexp"
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"
)

const slugPattern = `^[a-z0-9][a-z0-9-]{0,63}$`

var slugRe = regexp.MustCompile(slugPattern)
var tagRe = regexp.MustCompile(`^[\p{L}\p{N}_-]{1,32}$`)

func validSlug(s string) bool {
	return slugRe.MatchString(s)
}

// checkTagList validates tag syntax, normalisation, and in-list duplicates.
// It returns the normalised tag set for later inheritance checks.
func (v *validator) checkTagList(rel string, tags []string) map[string]struct{} {
	set := make(map[string]struct{}, len(tags))
	for _, raw := range tags {
		tag := strings.ToLower(norm.NFC.String(strings.TrimSpace(raw)))
		if tag != raw {
			v.res.errf(CodeTagSyntax, rel, "tag %q is not normalised (expected %q)", raw, tag)
		}
		if !tagRe.MatchString(tag) {
			v.res.errf(CodeTagSyntax, rel,
				"tag %q must be 1-32 letters, numbers, underscores, or hyphens", raw)
			continue
		}
		if _, dup := set[tag]; dup {
			v.res.errf(CodeTagDelta, rel, "tag %q is duplicated in tags_delta", raw)
			continue
		}
		set[tag] = struct{}{}
	}
	return set
}

func parseTimestamp(s string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func validTimestamp(s string) bool {
	_, ok := parseTimestamp(s)
	return ok
}
