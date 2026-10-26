package gardener

import (
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"

	"github.com/li-ziyin/memorytree/internal/model"
)

var (
	tagPattern  = regexp.MustCompile(`^[\p{L}\p{N}_-]{1,32}$`)
	slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
)

var reservedSlugs = map[string]bool{
	"node.md": true, "memories": true, ".inbox": true,
	".archive": true, ".index": true, "global": true,
}

// normalizeTag applies the format 5.2 rules: NFC, trim, lowercase. It
// returns the normalized tag and an empty message, or the original value and
// an error message when the tag is not valid.
func normalizeTag(raw string) (string, string) {
	tag := strings.ToLower(norm.NFC.String(strings.TrimSpace(raw)))
	if !tagPattern.MatchString(tag) {
		return raw, "tag " + strings.TrimSpace(raw) + " does not match the 5.2 syntax after normalization"
	}
	return tag, ""
}

func validSlug(slug string) bool {
	if !slugPattern.MatchString(slug) {
		return false
	}
	return !reservedSlugs[slug]
}

func validSensitivity(level string) bool {
	return model.SensitivityRank(level) >= 0
}
