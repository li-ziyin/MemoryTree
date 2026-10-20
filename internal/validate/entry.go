package validate

import (
	"path"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/li-ziyin/memorytree/internal/ids"
	"github.com/li-ziyin/memorytree/internal/model"
)

var entryFilePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*\.md$`)
var entryDatePattern = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})-`)

func (v *validator) checkEntryFields(rec *entryRec) {
	e := rec.entry
	rel := rec.file

	if e.Schema != model.EntrySchema {
		v.res.errf(CodeSchema, rel, "schema must be %q, got %q", model.EntrySchema, e.Schema)
	}
	if !ids.IsUUIDv7(e.ID) {
		v.res.errf(CodeIDFormat, rel, "id %q is not a canonical UUIDv7", e.ID)
	}
	if !ids.IsUUIDv7(e.Node) {
		v.res.errf(CodeIDReference, rel, "node %q is not a canonical UUIDv7", e.Node)
	}

	switch e.Type {
	case model.TypeUser, model.TypeFeedback, model.TypeProject, model.TypeReference:
	default:
		v.res.errf(CodeSchema, rel, "type %q must be user, feedback, project, or reference", e.Type)
	}
	switch e.Status {
	case model.EntryDraft, model.StatusActive, model.EntrySuperseded, model.EntryDeprecated:
	default:
		v.res.errf(CodeSchema, rel, "status %q must be draft, active, superseded, or deprecated", e.Status)
	}
	if model.SensitivityRank(e.Sensitivity) < 0 {
		v.res.errf(CodeSensitivity, rel, "sensitivity %q must be public, internal, or private", e.Sensitivity)
	}

	if c := utf8.RuneCountInString(e.Title); c < 1 || c > 200 {
		v.res.errf(CodeContent, rel, "title must be 1-200 characters, got %d", c)
	}
	if c := utf8.RuneCountInString(e.Summary); c < 1 || c > 500 {
		v.res.errf(CodeContent, rel, "summary must be 1-500 characters, got %d", c)
	} else if c > 280 {
		v.res.warnf(CodeWarnSummary, rel, "summary is %d characters; 280 or fewer is recommended", c)
	}

	if e.TagsDelta == nil {
		v.res.errf(CodeSchema, rel, "tags_delta is required (use [] when there are no tags)")
	} else {
		rec.tags = v.checkTagList(rel, e.TagsDelta)
	}

	v.checkSource(rec)
	v.checkEntryRefs(rec)
	v.checkEntryFilename(rec)

	created, okCreated := parseTimestamp(e.CreatedAt)
	if !okCreated {
		v.res.errf(CodeTime, rel, "created_at %q is not RFC 3339", e.CreatedAt)
	}
	updated, okUpdated := parseTimestamp(e.UpdatedAt)
	if !okUpdated {
		v.res.errf(CodeTime, rel, "updated_at %q is not RFC 3339", e.UpdatedAt)
	}
	if okCreated && okUpdated && updated.Before(created) {
		v.res.errf(CodeTime, rel, "updated_at is earlier than created_at")
	}
}

func (v *validator) checkSource(rec *entryRec) {
	e := rec.entry
	rel := rec.file
	if strings.TrimSpace(e.Source.Agent) == "" {
		v.res.errf(CodeSource, rel, "source.agent is required")
	}
	switch e.Source.Kind {
	case "conversation", "document", "manual", "import":
	default:
		v.res.errf(CodeSource, rel,
			"source.kind %q must be conversation, document, manual, or import", e.Source.Kind)
	}
	if e.Source.CapturedAt != "" && !validTimestamp(e.Source.CapturedAt) {
		v.res.errf(CodeTime, rel, "source.captured_at %q is not RFC 3339", e.Source.CapturedAt)
	}
	if unsafeSourceRef(e.Source.Ref) {
		v.res.errf(CodeSource, rel, "source.ref %q must be a relative path or URL, not a local absolute path", e.Source.Ref)
	}
}

func (v *validator) checkEntryRefs(rec *entryRec) {
	e := rec.entry
	rel := rec.file

	if e.Supersedes == nil {
		v.res.errf(CodeSchema, rel, "supersedes is required (use [] when empty)")
	} else {
		for _, id := range e.Supersedes {
			if !ids.IsUUIDv7(id) {
				v.res.errf(CodeIDReference, rel, "supersedes contains invalid ID %q", id)
			}
		}
	}
	if e.SupersededBy != nil && !ids.IsUUIDv7(*e.SupersededBy) {
		v.res.errf(CodeIDReference, rel, "superseded_by %q is not a canonical UUIDv7", *e.SupersededBy)
	}
	for _, id := range e.Related {
		if !ids.IsUUIDv7(id) {
			v.res.errf(CodeIDReference, rel, "related contains invalid ID %q", id)
		}
	}
}

func (v *validator) checkEntryFilename(rec *entryRec) {
	base := path.Base(rec.file)
	if !entryFilePattern.MatchString(base) {
		v.res.errf(CodeFile, rec.file, "entry filename %q must use lowercase ASCII slug characters", base)
	}
	m := entryDatePattern.FindStringSubmatch(base)
	if m == nil {
		return
	}
	created, ok := parseTimestamp(rec.entry.CreatedAt)
	if !ok {
		return
	}
	if m[1] != created.Format("2006-01-02") {
		v.res.warnf(CodeTimeFilename, rec.file,
			"filename date %s differs from created_at date %s", m[1], created.Format("2006-01-02"))
	}
}

func (v *validator) checkEntries() {
	for _, rec := range v.entries {
		e := rec.entry
		rel := rec.file

		host, ok := v.nodesByDir[rec.hostDir]
		if !ok {
			v.res.errf(CodeKindEntries, rel, "entries must be directly inside a node directory containing node.md")
			continue
		}
		rec.host = host

		if !canHostEntries(host.node) {
			v.res.errf(CodeKindEntries, rel,
				"node %s (%s) cannot contain entries; only projects and global can", host.file, host.node.Kind)
		}

		if target, ok := v.nodesByID[e.Node]; !ok {
			v.res.errf(CodeIDReference, rel, "node ID %s does not reference an existing node", e.Node)
		} else if target != host {
			v.res.errf(CodeKindEntries, rel, "entry is stored under %s but declares node %s", host.file, target.file)
		}

		if hostLevel := model.SensitivityRank(host.node.Sensitivity); hostLevel >= 0 {
			if own := model.SensitivityRank(e.Sensitivity); own >= 0 && own < hostLevel {
				v.res.errf(CodeSensitivity, rel,
					"sensitivity %q is weaker than host node sensitivity %q", e.Sensitivity, host.node.Sensitivity)
			}
		}

		inherited := v.effectiveTags(host)
		for tag := range rec.tags {
			if _, dup := inherited[tag]; dup {
				v.res.errf(CodeTagDelta, rel,
					"tags_delta contains %q, which is already inherited from the node path", tag)
			}
		}

		for _, id := range e.Related {
			if _, ok := v.entriesByID[id]; !ok {
				v.res.errf(CodeIDReference, rel, "related ID %s does not reference an existing entry", id)
			}
		}
	}
}

func (v *validator) checkSupersession() {
	for _, rec := range v.entries {
		e := rec.entry
		rel := rec.file

		if e.SupersededBy != nil {
			if *e.SupersededBy == e.ID {
				v.res.errf(CodeSupersedeSelf, rel, "superseded_by must not reference the entry itself")
			}
			if e.Status != model.EntrySuperseded {
				v.res.errf(CodeSupersededBy, rel, "superseded_by is set but status is %q, not superseded", e.Status)
			}
			if target, ok := v.entriesByID[*e.SupersededBy]; !ok {
				v.res.errf(CodeIDReference, rel, "superseded_by ID %s does not reference an existing entry", *e.SupersededBy)
			} else if !containsID(target.entry.Supersedes, e.ID) {
				v.res.errf(CodeSupersedePair, rel,
					"superseded_by %s does not list this entry in its supersedes", *e.SupersededBy)
			}
		} else if e.Status == model.EntrySuperseded {
			v.res.errf(CodeSupersededBy, rel, "status is superseded but superseded_by is null")
		}

		for _, oldID := range e.Supersedes {
			if oldID == e.ID {
				v.res.errf(CodeSupersedeSelf, rel, "supersedes must not contain the entry itself")
				continue
			}
			if e.Status != model.StatusActive {
				v.res.errf(CodeSupersedeStatus, rel,
					"an entry with a non-empty supersedes list must be active, got %q", e.Status)
			}
			old, ok := v.entriesByID[oldID]
			if !ok {
				v.res.errf(CodeIDReference, rel, "supersedes ID %s does not reference an existing entry", oldID)
				continue
			}
			if old.entry.Status != model.EntrySuperseded {
				v.res.errf(CodeSupersedePair, rel,
					"entry %s is superseded by %s but its status is %q", oldID, e.ID, old.entry.Status)
			}
			if old.entry.SupersededBy == nil || *old.entry.SupersededBy != e.ID {
				v.res.errf(CodeSupersedePair, rel,
					"entry %s supersedes %s but %s.superseded_by does not point back", e.ID, oldID, oldID)
			}
		}
	}

	v.checkSupersedeCycles()
}

func (v *validator) checkSupersedeCycles() {
	const (
		unvisited = 0
		visiting  = 1
		done      = 2
	)
	state := make(map[string]int, len(v.entries))
	reported := make(map[string]bool)

	var visit func(rec *entryRec)
	visit = func(rec *entryRec) {
		id := rec.entry.ID
		switch state[id] {
		case visiting:
			if !reported[id] {
				v.res.errf(CodeSupersedeSelf, rec.file, "supersedes chain contains a cycle")
				reported[id] = true
			}
			return
		case done:
			return
		}
		state[id] = visiting
		for _, oldID := range rec.entry.Supersedes {
			if next, ok := v.entriesByID[oldID]; ok {
				visit(next)
			}
		}
		state[id] = done
	}
	for _, rec := range v.entries {
		visit(rec)
	}
}

func canHostEntries(n *model.Node) bool {
	if n.Kind == model.KindProject {
		return true
	}
	return n.Kind == model.KindCategory && n.Slug == model.GlobalSlug
}

func containsID(list []string, id string) bool {
	for _, v := range list {
		if v == id {
			return true
		}
	}
	return false
}

func unsafeSourceRef(ref string) bool {
	if ref == "" {
		return false
	}
	lower := strings.ToLower(ref)
	if strings.HasPrefix(lower, "file://") {
		return true
	}
	if strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, `\`) {
		return true
	}
	if strings.Contains(ref, "://") {
		return false
	}
	if len(ref) >= 2 && ref[1] == ':' {
		c := ref[0]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			return true
		}
	}
	for _, seg := range strings.FieldsFunc(ref, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." {
			return true
		}
	}
	return false
}
