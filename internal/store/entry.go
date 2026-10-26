package store

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/li-ziyin/memorytree/internal/ids"
	"github.com/li-ziyin/memorytree/internal/model"
	"github.com/li-ziyin/memorytree/internal/validate"
)

const (
	maxTitleRunes   = 200
	maxSummaryRunes = 500
	// format.MaxBody is 1 MiB; the input check mirrors it before writing.
	maxBodyBytes = 1 << 20
	maxSlugChars = 48
)

var tagPattern = regexp.MustCompile(`^[\p{L}\p{N}_-]{1,32}$`)

// sourceKindValues is the closed enum of spec 8.2.
var sourceKindValues = map[string]bool{
	"conversation": true,
	"document":     true,
	"manual":       true,
	"import":       true,
}

func validType(t string) bool {
	switch t {
	case model.TypeUser, model.TypeFeedback, model.TypeProject, model.TypeReference:
		return true
	}
	return false
}

func validSensitivity(s string) bool {
	return model.SensitivityRank(s) >= 0
}

// buildEntry validates the input and produces the frontmatter object.
func buildEntry(node *validate.NodeInfo, in EntryInput, created string) (*model.Entry, error) {
	title := strings.TrimSpace(in.Title)
	switch r := utf8.RuneCountInString(title); {
	case r == 0:
		return nil, inputf("V-CONTENT-1", "title is required")
	case r > maxTitleRunes:
		return nil, inputf("V-CONTENT-1", "title is %d runes, want at most %d", r, maxTitleRunes)
	}
	summary := strings.TrimSpace(in.Summary)
	switch r := utf8.RuneCountInString(summary); {
	case r == 0:
		return nil, inputf("V-CONTENT-1", "summary is required; write a semantic summary, do not truncate the body")
	case r > maxSummaryRunes:
		return nil, inputf("V-CONTENT-1", "summary is %d runes, want at most %d", r, maxSummaryRunes)
	}
	if len(in.Body) > maxBodyBytes {
		return nil, inputf("V-CONTENT-3", "body is %d bytes, want at most %d", len(in.Body), maxBodyBytes)
	}
	if !validType(in.Type) {
		return nil, inputf("V-ENUM-1", "type must be user, feedback, project, or reference")
	}

	status := in.Status
	if status == "" {
		status = model.StatusActive
	}
	if status != model.StatusActive && status != model.EntryDraft {
		return nil, inputf("V-ENUM-1", "new entries must be active or draft, not %q", status)
	}
	if len(in.Supersedes) > 0 && status != model.StatusActive {
		return nil, inputf("V-SUP-4", "an entry that supersedes others must be active")
	}

	sensitivity := in.Sensitivity
	if sensitivity == "" {
		// Default to the host node's level: never weaker than the node
		// (V-SENS-1), and stricter only when the caller asks for it.
		sensitivity = node.Sensitivity
	}
	if !validSensitivity(sensitivity) {
		return nil, inputf("V-ENUM-1", "sensitivity must be public, internal, or private")
	}
	if model.SensitivityRank(sensitivity) < model.SensitivityRank(node.Sensitivity) {
		return nil, inputf("V-SENS-1",
			"sensitivity %q is weaker than host node %q (%s)", sensitivity, node.Path, node.Sensitivity)
	}

	tags, err := normalizeTags(in.TagsDelta)
	if err != nil {
		return nil, err
	}

	agent := strings.TrimSpace(in.Source.Agent)
	if agent == "" {
		return nil, inputf("V-CONTENT-2", "source.agent is required")
	}
	kind := strings.TrimSpace(in.Source.Kind)
	if !sourceKindValues[kind] {
		return nil, inputf("V-CONTENT-2", "source.kind must be conversation, document, manual, or import")
	}
	if ref := strings.TrimSpace(in.Source.Ref); ref != "" && isAbsoluteish(ref) {
		return nil, inputf("V-CONTENT-2", "source.ref must not be an absolute path")
	}

	for _, id := range append(append([]string{}, in.Supersedes...), in.Related...) {
		if !ids.IsUUIDv7(id) {
			return nil, inputf("V-REF-1", "%q is not a UUIDv7", id)
		}
	}

	return &model.Entry{
		Schema:      model.EntrySchema,
		ID:          ids.NewUUIDv7(),
		Node:        node.ID,
		Type:        in.Type,
		Status:      status,
		Sensitivity: sensitivity,
		Title:       title,
		Summary:     summary,
		TagsDelta:   tags,
		Source: model.Source{
			Agent:      agent,
			Kind:       kind,
			Session:    strings.TrimSpace(in.Source.Session),
			CapturedAt: created,
			Ref:        strings.TrimSpace(in.Source.Ref),
		},
		Supersedes: in.Supersedes,
		Related:    in.Related,
		CreatedAt:  created,
		UpdatedAt:  created,
	}, nil
}

// normalizeTags applies the 5.2 rules: NFC, trim, lowercase, syntax check,
// no duplicates, sorted for reproducible writes.
func normalizeTags(tags []string) ([]string, error) {
	seen := make(map[string]struct{}, len(tags))
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		t := strings.ToLower(norm.NFC.String(strings.TrimSpace(tag)))
		if !tagPattern.MatchString(t) {
			return nil, inputf("V-TAG-1", "tag %q does not match the 5.2 syntax after normalization", tag)
		}
		if _, dup := seen[t]; dup {
			return nil, inputf("V-TAG-2", "duplicate tag %q after normalization", t)
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	sort.Strings(out)
	return out, nil
}

// isAbsoluteish rejects Windows and POSIX absolute paths and UNC shares.
func isAbsoluteish(ref string) bool {
	if strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, `\\`) {
		return true
	}
	if len(ref) >= 2 && ref[1] == ':' && unicode.IsLetter(rune(ref[0])) {
		return true
	}
	return false
}

// slugFromTitle keeps the NFC-lowercased ASCII letters and digits of the
// title, joined by single hyphens. Chinese-only titles yield "entry".
func slugFromTitle(title string) string {
	lowered := strings.ToLower(norm.NFC.String(title))
	var b strings.Builder
	prevDash := true // suppresses a leading hyphen
	for _, r := range lowered {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		case !prevDash:
			b.WriteByte('-')
			prevDash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if utf8.RuneCountInString(slug) > maxSlugChars {
		slug = strings.Trim(slug[:maxSlugChars], "-")
	}
	if slug == "" {
		slug = "entry"
	}
	return slug
}

// entryFileName is <YYYY-MM-DD>-<slug>.md. Collisions are disambiguated with
// the RANDOM tail of the entry UUID — the head is a millisecond timestamp
// shared by every entry written in the same minute, so using it would make
// same-titled entries overwrite each other. A counter breaks the remaining
// theoretical clash.
func entryFileName(entry *model.Entry, created string, taken map[string]bool) string {
	date := created
	if t, err := time.Parse(time.RFC3339, created); err == nil {
		date = t.Format("2006-01-02")
	}
	slug := slugFromTitle(entry.Title)
	base := fmt.Sprintf("%s-%s", date, slug)
	if !taken[base+".md"] {
		return base + ".md"
	}
	tail := entry.ID[28:36]
	for n := 1; ; n++ {
		var name string
		if n == 1 {
			name = fmt.Sprintf("%s-%s.md", base, tail)
		} else {
			name = fmt.Sprintf("%s-%s-%d.md", base, tail, n)
		}
		if !taken[name] {
			return name
		}
	}
}
