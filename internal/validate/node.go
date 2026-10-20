package validate

import (
	"path"
	"strings"
	"unicode/utf8"

	"github.com/li-ziyin/memorytree/internal/ids"
	"github.com/li-ziyin/memorytree/internal/model"
)

func (v *validator) checkNodeFields(rec *nodeRec) {
	n := rec.node
	rel := rec.file

	if n.Schema != model.NodeSchema {
		v.res.errf(CodeSchema, rel, "schema must be %q, got %q", model.NodeSchema, n.Schema)
	}
	if !ids.IsUUIDv7(n.ID) {
		v.res.errf(CodeIDFormat, rel, "id %q is not a canonical UUIDv7", n.ID)
	}

	switch n.Kind {
	case model.KindRoot:
		if n.Parent != nil {
			v.res.errf(CodeRoot, rel, "root node must have parent: null")
		}
		if n.Slug != "" {
			v.res.errf(CodeRoot, rel, "root node must have an empty slug, got %q", n.Slug)
		}
	case model.KindCategory, model.KindProject:
		if n.Parent == nil {
			v.res.errf(CodeParent, rel, "non-root node must declare a parent")
		} else if !ids.IsUUIDv7(*n.Parent) {
			v.res.errf(CodeParent, rel, "parent %q is not a canonical UUIDv7", *n.Parent)
		}
		if !validSlug(n.Slug) {
			v.res.errf(CodeSlug, rel, "slug %q does not match %s", n.Slug, slugPattern)
		}
	case "":
		v.res.errf(CodeSchema, rel, "kind is required")
	default:
		v.res.errf(CodeSchema, rel, "kind %q must be root, category, or project", n.Kind)
	}

	if n.Kind != model.KindRoot && n.Kind != "" {
		if strings.TrimSpace(n.Slug) == "" {
			v.res.errf(CodeSlug, rel, "slug is required for non-root nodes")
		}
	}

	if c := utf8.RuneCountInString(n.Title); c < 1 || c > 120 {
		v.res.errf(CodeContent, rel, "title must be 1-120 characters, got %d", c)
	}
	if c := utf8.RuneCountInString(n.Summary); c < 1 || c > 500 {
		v.res.errf(CodeContent, rel, "summary must be 1-500 characters, got %d", c)
	} else if c > 280 {
		v.res.warnf(CodeWarnSummary, rel, "summary is %d characters; 280 or fewer is recommended", c)
	}

	switch n.Status {
	case model.StatusActive, model.StatusArchived:
	default:
		v.res.errf(CodeSchema, rel, "status %q must be active or archived", n.Status)
	}

	if model.SensitivityRank(n.Sensitivity) < 0 {
		v.res.errf(CodeSensitivity, rel, "sensitivity %q must be public, internal, or private", n.Sensitivity)
	}

	created, okCreated := parseTimestamp(n.CreatedAt)
	if !okCreated {
		v.res.errf(CodeTime, rel, "created_at %q is not RFC 3339", n.CreatedAt)
	}
	updated, okUpdated := parseTimestamp(n.UpdatedAt)
	if !okUpdated {
		v.res.errf(CodeTime, rel, "updated_at %q is not RFC 3339", n.UpdatedAt)
	}
	if okCreated && okUpdated && updated.Before(created) {
		v.res.errf(CodeTime, rel, "updated_at is earlier than created_at")
	}

	if n.TagsDelta == nil {
		v.res.errf(CodeSchema, rel, "tags_delta is required (use [] when there are no tags)")
	} else {
		rec.tags = v.checkTagList(rel, n.TagsDelta)
	}
}

func (v *validator) checkNodes() {
	rootCount := 0
	var rootRec *nodeRec
	for _, rec := range v.nodes {
		if rec.node.Kind == model.KindRoot {
			rootCount++
			if rec.dir == "." {
				rootRec = rec
			} else {
				v.res.errf(CodeRoot, rec.file, "root node must be located at the repository root")
			}
		}
	}
	if rootCount != 1 {
		v.res.errf(CodeRoot, ".", "repository must contain exactly one kind: root node, found %d", rootCount)
	}
	if rootRec == nil {
		return
	}

	slugOwners := make(map[string]string)
	for _, rec := range v.nodes {
		v.checkNodeStructure(rec, rootRec, slugOwners)
		v.effectiveTags(rec)
		v.checkNodeWarnings(rec)
	}
}

func (v *validator) checkNodeStructure(rec, rootRec *nodeRec, slugOwners map[string]string) {
	n := rec.node
	if rec == rootRec {
		return
	}

	base := path.Base(rec.dir)
	if n.Slug != base {
		v.res.errf(CodeSlug, rec.file, "slug %q does not match directory name %q", n.Slug, base)
	}
	switch n.Slug {
	case "node.md", "memories", ".inbox", ".archive", ".index":
		v.res.errf(CodeSlug, rec.file, "slug %q is reserved", n.Slug)
	case model.GlobalSlug:
		if !(n.Kind == model.KindCategory && rec.parent != nil && rec.parent.node.Kind == model.KindRoot) {
			v.res.errf(CodeSlug, rec.file, "global is reserved for a category directly under the root")
		}
	}
	key := rec.parentDirKey() + "\x00" + n.Slug
	if prev, ok := slugOwners[key]; ok {
		v.res.errf(CodeSlug, rec.file, "slug %q is not unique among siblings (also %s)", n.Slug, prev)
	} else {
		slugOwners[key] = rec.file
	}

	if n.Parent != nil && ids.IsUUIDv7(*n.Parent) {
		target, ok := v.nodesByID[*n.Parent]
		switch {
		case !ok:
			v.res.errf(CodeIDReference, rec.file, "parent ID %s does not reference an existing node", *n.Parent)
		case rec.parent != nil && target != rec.parent:
			v.res.errf(CodeParent, rec.file, "parent ID %s does not match the containing directory", *n.Parent)
		}
	}

	if n.Kind == model.KindProject && len(rec.children) > 0 {
		v.res.errf(CodeKindChild, rec.file, "project nodes must not have child nodes")
	}
	if rec.parent != nil && rec.parent.node.Kind == model.KindProject {
		v.res.errf(CodeKindChild, rec.file, "parent %s is a project; project nodes must not have child nodes", rec.parent.file)
	}
	if n.Kind == model.KindCategory && n.Slug == model.GlobalSlug && len(rec.children) > 0 {
		v.res.errf(CodeKindGlobal, rec.file, "global must not have child nodes")
	}

	if rec.parent != nil {
		parentLevel := model.SensitivityRank(rec.parent.node.Sensitivity)
		ownLevel := model.SensitivityRank(n.Sensitivity)
		if parentLevel >= 0 && ownLevel >= 0 && ownLevel < parentLevel {
			v.res.errf(CodeSensitivity, rec.file,
				"sensitivity %q is weaker than parent sensitivity %q", n.Sensitivity, rec.parent.node.Sensitivity)
		}
	}
}

func (v *validator) effectiveTags(rec *nodeRec) map[string]struct{} {
	if rec.effective != nil {
		return rec.effective
	}
	merged := make(map[string]struct{})
	if rec.parent != nil {
		for tag := range v.effectiveTags(rec.parent) {
			merged[tag] = struct{}{}
		}
	}
	for tag := range rec.tags {
		if _, inherited := merged[tag]; inherited {
			v.res.errf(CodeTagDelta, rec.file,
				"tags_delta contains %q, which is already inherited from an ancestor", tag)
			continue
		}
		merged[tag] = struct{}{}
	}
	rec.effective = merged
	return merged
}

func (v *validator) checkNodeWarnings(rec *nodeRec) {
	depth := 0
	for p := rec.parent; p != nil; p = p.parent {
		depth++
	}
	if depth > 4 {
		v.res.warnf(CodeWarnDepth, rec.file, "node depth is %d; more than 4 is hard to navigate", depth)
	}
	if len(rec.children) > 8 {
		v.res.warnf(CodeWarnBranch, rec.file, "node has %d children; more than 8 exceeds the recommended branch factor", len(rec.children))
	}
}

func (rec *nodeRec) parentDirKey() string {
	if rec.parent == nil {
		return "."
	}
	return rec.parent.dir
}
