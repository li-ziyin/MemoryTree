package validate

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
	if err != nil {
		t.Fatalf("copy example: %v", err)
	}
}

func exampleCopy(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	copyDir(t, filepath.Join("..", "..", "spec", "examples"), dst)
	return dst
}

func mustValidate(t *testing.T, root string) *Result {
	t.Helper()
	res, err := Validate(root)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	return res
}

func hasCode(res *Result, code string) bool {
	for _, issue := range res.Issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}

func edit(t *testing.T, file, old, new string) {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	if !strings.Contains(string(data), old) {
		t.Fatalf("%s does not contain %q", file, old)
	}
	data = []byte(strings.Replace(string(data), old, new, 1))
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", file, err)
	}
}

func TestExampleRepositoryIsValid(t *testing.T) {
	res := mustValidate(t, exampleCopy(t))
	if !res.OK() {
		t.Fatalf("expected valid repository, got %d errors: %+v", res.Errors(), res.Issues)
	}
	if res.Nodes != 4 {
		t.Errorf("nodes = %d, want 4", res.Nodes)
	}
	if res.Entries != 2 {
		t.Errorf("entries = %d, want 2", res.Entries)
	}
}

func TestSpecifiedInvariants(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, root string)
		code   string
	}{
		{
			name: "root must have null parent",
			mutate: func(t *testing.T, root string) {
				edit(t, filepath.Join(root, "node.md"), "parent: null", "parent: 019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b02")
			},
			code: CodeRoot,
		},
		{
			name: "ids are globally unique",
			mutate: func(t *testing.T, root string) {
				edit(t, filepath.Join(root, "work", "node.md"),
					"id: 019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b03", "id: 019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b02")
			},
			code: CodeIDFormat,
		},
		{
			name: "project cannot have child nodes",
			mutate: func(t *testing.T, root string) {
				dir := filepath.Join(root, "work", "agentflow", "child")
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
				writeNodeFile(t, filepath.Join(dir, "node.md"),
					"019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b90", "category", "019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b04", "child")
			},
			code: CodeKindChild,
		},
		{
			name: "tags must be normalised",
			mutate: func(t *testing.T, root string) {
				edit(t, filepath.Join(root, "global", "node.md"), "tags_delta: [global]", "tags_delta: [Global]")
			},
			code: CodeTagSyntax,
		},
		{
			name: "tags_delta must not repeat inherited tags",
			mutate: func(t *testing.T, root string) {
				edit(t, filepath.Join(root, "work", "agentflow", "node.md"),
					"tags_delta: [java, spring, agent]", "tags_delta: [work, java, spring, agent]")
			},
			code: CodeTagDelta,
		},
		{
			name: "slug must match directory",
			mutate: func(t *testing.T, root string) {
				edit(t, filepath.Join(root, "global", "node.md"), "slug: global", "slug: globals")
			},
			code: CodeSlug,
		},
		{
			name: "sensitivity cannot weaken down the tree",
			mutate: func(t *testing.T, root string) {
				edit(t, filepath.Join(root, "global", "node.md"),
					"sensitivity: private", "sensitivity: public")
			},
			code: CodeSensitivity,
		},
		{
			name: "superseded requires superseded_by",
			mutate: func(t *testing.T, root string) {
				edit(t, filepath.Join(root, "work", "agentflow", "memories", "2026-10-20-sse-pitfall.md"),
					"status: active", "status: superseded")
			},
			code: CodeSupersededBy,
		},
		{
			name: "references must resolve",
			mutate: func(t *testing.T, root string) {
				edit(t, filepath.Join(root, "global", "memories", "2026-10-20-user-profile.md"),
					"node: 019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b02",
					"node: 019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b77")
			},
			code: CodeIDReference,
		},
		{
			name: "broken YAML is a file error",
			mutate: func(t *testing.T, root string) {
				edit(t, filepath.Join(root, "work", "agentflow", "node.md"),
					"title: AgentFlow", "title: [unclosed")
			},
			code: CodeFile,
		},
		{
			name: "format_version 2 is rejected",
			mutate: func(t *testing.T, root string) {
				edit(t, filepath.Join(root, "memory.yaml"), "format_version: 1", "format_version: 2")
			},
			code: CodeFormatUnsupported,
		},
		{
			name: "yaml aliases are rejected",
			mutate: func(t *testing.T, root string) {
				edit(t, filepath.Join(root, "work", "node.md"),
					"title: 工作", "title: 工作\nsecret: &a hidden\ncopy: *a")
			},
			code: CodeFile,
		},
		{
			name: "entry filenames use ascii slugs",
			mutate: func(t *testing.T, root string) {
				old := filepath.Join(root, "global", "memories", "2026-10-20-user-profile.md")
				new := filepath.Join(root, "global", "memories", "2026-10-20-User.md")
				if err := os.Rename(old, new); err != nil {
					t.Fatal(err)
				}
			},
			code: CodeFile,
		},
		{
			name: "categories other than global hold no entries",
			mutate: func(t *testing.T, root string) {
				dir := filepath.Join(root, "work", "memories")
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
				writeEntryFile(t, filepath.Join(dir, "2026-10-20-note.md"),
					"019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b91", "019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b03", "reference")
			},
			code: CodeKindEntries,
		},
		{
			name: "global has no child nodes",
			mutate: func(t *testing.T, root string) {
				dir := filepath.Join(root, "global", "sub")
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
				writeNodeFile(t, filepath.Join(dir, "node.md"),
					"019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b92", "category", "019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b02", "sub")
			},
			code: CodeKindGlobal,
		},
		{
			name: "supersedes links must be reciprocal",
			mutate: func(t *testing.T, root string) {
				file := filepath.Join(root, "work", "agentflow", "memories", "2026-10-20-newer.md")
				writeEntryFile(t, file,
					"019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b93", "019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b04", "project")
				edit(t, file, "supersedes: []", "supersedes: [019a2f4e-7c1a-7b3d-9e21-4f6a8c2d1b06]")
			},
			code: CodeSupersedePair,
		},
		{
			name: "entry sensitivity cannot weaken",
			mutate: func(t *testing.T, root string) {
				edit(t, filepath.Join(root, "global", "memories", "2026-10-20-user-profile.md"),
					"sensitivity: private", "sensitivity: public")
			},
			code: CodeSensitivity,
		},
		{
			name: "absolute source refs are rejected",
			mutate: func(t *testing.T, root string) {
				edit(t, filepath.Join(root, "work", "agentflow", "memories", "2026-10-20-sse-pitfall.md"),
					"  kind: conversation", "  kind: conversation\n  ref: C:\\\\secret\\\\notes.md")
			},
			code: CodeSource,
		},
		{
			name: "updated_at cannot precede created_at",
			mutate: func(t *testing.T, root string) {
				edit(t, filepath.Join(root, "work", "agentflow", "node.md"),
					"updated_at: \"2026-10-20T00:00:00Z\"", "updated_at: \"2026-01-01T00:00:00Z\"")
			},
			code: CodeTime,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := exampleCopy(t)
			tc.mutate(t, root)
			res := mustValidate(t, root)
			if res.OK() {
				t.Fatalf("expected validation failure, got none")
			}
			if !hasCode(res, tc.code) {
				t.Fatalf("expected code %s, got %+v", tc.code, res.Issues)
			}
		})
	}
}

func writeNodeFile(t *testing.T, file, id, kind, parent, slug string) {
	t.Helper()
	body := "---\n" +
		"schema: memorytree.node/v1\n" +
		"id: " + id + "\n" +
		"kind: " + kind + "\n" +
		"parent: " + parent + "\n" +
		"slug: " + slug + "\n" +
		"title: Test\n" +
		"summary: Test node.\n" +
		"tags_delta: []\n" +
		"aliases: []\n" +
		"status: active\n" +
		"sensitivity: private\n" +
		"created_at: \"2026-10-20T00:00:00Z\"\n" +
		"updated_at: \"2026-10-20T00:00:00Z\"\n" +
		"---\n"
	if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeEntryFile(t *testing.T, file, id, node, typ string) {
	t.Helper()
	body := "---\n" +
		"schema: memorytree.entry/v1\n" +
		"id: " + id + "\n" +
		"node: " + node + "\n" +
		"type: " + typ + "\n" +
		"status: active\n" +
		"sensitivity: private\n" +
		"title: Test entry\n" +
		"summary: Test entry.\n" +
		"tags_delta: []\n" +
		"source:\n" +
		"  agent: manual\n" +
		"  kind: manual\n" +
		"supersedes: []\n" +
		"superseded_by: null\n" +
		"related: []\n" +
		"created_at: \"2026-10-20T00:00:00Z\"\n" +
		"updated_at: \"2026-10-20T00:00:00Z\"\n" +
		"---\n"
	if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
