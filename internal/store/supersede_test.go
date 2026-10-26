package store

import (
	"strings"
	"testing"

	"github.com/li-ziyin/memorytree/internal/format"
	"github.com/li-ziyin/memorytree/internal/index"
	"github.com/li-ziyin/memorytree/internal/model"
)

func readEntry(t *testing.T, path string) *model.Entry {
	t.Helper()
	doc, err := format.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var e model.Entry
	if err := format.Decode(doc.Meta, &e); err != nil {
		t.Fatal(err)
	}
	return &e
}

func addOne(t *testing.T, root string, title string) *AddResult {
	t.Helper()
	in := validInput()
	in.Title = title
	res, err := AddEntry(root, in)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestAddEntrySupersedesRewritesOld(t *testing.T) {
	root := newRepo(t)
	first := addOne(t, root, "U30 网络诊断")
	in := validInput()
	in.Title = "U30 网络诊断 v2"
	in.Supersedes = []string{first.ID}
	second, err := AddEntry(root, in)
	if err != nil {
		t.Fatal(err)
	}

	old := readEntry(t, first.Path)
	if old.Status != model.EntrySuperseded || old.SupersededBy == nil || *old.SupersededBy != second.ID {
		t.Fatalf("old entry = %+v", old)
	}
	fresh := readEntry(t, second.Path)
	if len(fresh.Supersedes) != 1 || fresh.Supersedes[0] != first.ID {
		t.Fatalf("new entry supersedes = %v", fresh.Supersedes)
	}

	// The tree stays valid (V-SUP-2 both directions).
	if _, err := index.Build(root, index.BuildOptions{}); err != nil {
		t.Fatalf("repository invalid after supersede: %v", err)
	}

	// M2 acceptance: superseded entries are gone from default search.
	ix := indexOpen(t, root)
	defer ix.Close()
	hits, err := ix.Search(nil, index.SearchOptions{Query: "U30"})
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hits {
		if h.ID == first.ID {
			t.Fatalf("superseded entry still in default search: %+v", h)
		}
	}
	all, err := ix.Search(nil, index.SearchOptions{Query: "U30", Statuses: []string{model.EntrySuperseded}})
	if err != nil || len(all) != 1 || all[0].ID != first.ID {
		t.Fatalf("explicit superseded search: %v %+v", err, all)
	}
}

// Format v1 keeps supersedes lists only on the active head (V-SUP-4), so a
// third version takes over the WHOLE predecessor set: v3 supersedes
// [v1, v2], v2's own list is cleared, and v1/v2 both point at v3.
func TestSupersedeChainFlattens(t *testing.T) {
	root := newRepo(t)
	v1 := addOne(t, root, "压缩工具")
	in := validInput()
	in.Title = "压缩工具 v2"
	in.Supersedes = []string{v1.ID}
	v2, err := AddEntry(root, in)
	if err != nil {
		t.Fatal(err)
	}

	// Path A: memoryd-style add with supersedes.
	in2 := validInput()
	in2.Title = "压缩工具 v3"
	in2.Supersedes = []string{v2.ID}
	v3, err := AddEntry(root, in2)
	if err != nil {
		t.Fatal(err)
	}
	e1, e2, e3 := readEntry(t, v1.Path), readEntry(t, v2.Path), readEntry(t, v3.Path)
	if *e1.SupersededBy != v3.ID || *e2.SupersededBy != v3.ID {
		t.Fatalf("predecessors not re-pointed: %s %s", *e1.SupersededBy, *e2.SupersededBy)
	}
	if len(e2.Supersedes) != 0 {
		t.Fatalf("v2 supersedes = %v, want empty (V-SUP-4)", e2.Supersedes)
	}
	if len(e3.Supersedes) != 2 {
		t.Fatalf("v3 supersedes = %v, want both predecessors", e3.Supersedes)
	}
	if _, err := index.Build(root, index.BuildOptions{}); err != nil {
		t.Fatalf("repository invalid after chain flatten: %v", err)
	}

	// Path B: standalone SupersedeEntry on a fresh chain.
	w1 := addOne(t, root, "网关配置")
	w2 := addOne(t, root, "网关配置 v2")
	if _, err := SupersedeEntry(root, w1.ID, w2.ID); err != nil {
		t.Fatal(err)
	}
	w3 := addOne(t, root, "网关配置 v3")
	if _, err := SupersedeEntry(root, w2.ID, w3.ID); err != nil {
		t.Fatal(err)
	}
	g1, g3 := readEntry(t, w1.Path), readEntry(t, w3.Path)
	if *g1.SupersededBy != w3.ID {
		t.Fatalf("w1 superseded_by = %s, want %s", *g1.SupersededBy, w3.ID)
	}
	if len(g3.Supersedes) != 2 {
		t.Fatalf("w3 supersedes = %v, want both", g3.Supersedes)
	}
	if _, err := index.Build(root, index.BuildOptions{}); err != nil {
		t.Fatalf("repository invalid after standalone chain flatten: %v", err)
	}
}

func TestSupersedeEntryLinksExisting(t *testing.T) {
	root := newRepo(t)
	first := addOne(t, root, "ReadBook 状态")
	second := addOne(t, root, "ReadBook 状态 v2")

	res, err := SupersedeEntry(root, first.ID, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.OldPath != first.Path || res.ByPath != second.Path {
		t.Fatalf("paths = %+v vs %+v / %+v", res, first, second)
	}
	old := readEntry(t, first.Path)
	if old.Status != model.EntrySuperseded || *old.SupersededBy != second.ID {
		t.Fatalf("old = %+v", old)
	}
	by := readEntry(t, second.Path)
	if len(by.Supersedes) != 1 || by.Supersedes[0] != first.ID {
		t.Fatalf("by supersedes = %v", by.Supersedes)
	}
	if _, err := index.Build(root, index.BuildOptions{}); err != nil {
		t.Fatalf("repository invalid: %v", err)
	}
}

func TestSupersedeEntryRejectsBadPairs(t *testing.T) {
	root := newRepo(t)
	first := addOne(t, root, "Windhawk 设置")
	second := addOne(t, root, "Windhawk 设置 v2")
	if _, err := SupersedeEntry(root, first.ID, second.ID); err != nil {
		t.Fatal(err)
	}
	third := addOne(t, root, "Windhawk 设置 v3")

	cases := []struct {
		name   string
		oldID  string
		byID   string
		code   string
		substr string
	}{
		{"unknown old", "019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b99", second.ID, "V-SUP-1", "does not exist"},
		{"unknown by", first.ID, "019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b98", "V-SUP-1", "does not exist"},
		{"self", first.ID, first.ID, "V-SUP-1", "itself"},
		{"terminal old", first.ID, third.ID, "V-SUP-5", "terminal"},
		{"by draft", second.ID, first.ID, "V-SUP-4", "active"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := SupersedeEntry(root, tc.oldID, tc.byID)
			ie, ok := err.(*InputError)
			if !ok {
				t.Fatalf("err = %v, want *InputError", err)
			}
			if ie.Code != tc.code || !strings.Contains(ie.Message, tc.substr) {
				t.Errorf("err = %s: %s, want %s containing %q", ie.Code, ie.Message, tc.code, tc.substr)
			}
		})
	}
}
