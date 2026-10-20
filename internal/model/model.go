// Package model defines the on-disk metadata objects of a MemoryTree
// repository (format v1). Unknown YAML fields are ignored when decoding and
// must be preserved by writers; validation does not reject them.
package model

// Repo is the content of memory.yaml at the repository root.
type Repo struct {
	Schema             string `yaml:"schema"`
	FormatVersion      int    `yaml:"format_version"`
	RepoID             string `yaml:"repo_id"`
	CreatedAt          string `yaml:"created_at"`
	DefaultSensitivity string `yaml:"default_sensitivity"`
	Description        string `yaml:"description"`
}

// Node is the frontmatter of a node.md file.
type Node struct {
	Schema      string   `yaml:"schema"`
	ID          string   `yaml:"id"`
	Kind        string   `yaml:"kind"`
	Parent      *string  `yaml:"parent"`
	Slug        string   `yaml:"slug"`
	Title       string   `yaml:"title"`
	Summary     string   `yaml:"summary"`
	TagsDelta   []string `yaml:"tags_delta"`
	Aliases     []string `yaml:"aliases"`
	Status      string   `yaml:"status"`
	Sensitivity string   `yaml:"sensitivity"`
	CreatedAt   string   `yaml:"created_at"`
	UpdatedAt   string   `yaml:"updated_at"`
}

// Source records where an entry came from.
type Source struct {
	Agent      string `yaml:"agent"`
	Kind       string `yaml:"kind"`
	Session    string `yaml:"session,omitempty"`
	CapturedAt string `yaml:"captured_at,omitempty"`
	Ref        string `yaml:"ref,omitempty"`
}

// Entry is the frontmatter of a memory entry file.
type Entry struct {
	Schema       string   `yaml:"schema"`
	ID           string   `yaml:"id"`
	Node         string   `yaml:"node"`
	Type         string   `yaml:"type"`
	Status       string   `yaml:"status"`
	Sensitivity  string   `yaml:"sensitivity"`
	Title        string   `yaml:"title"`
	Summary      string   `yaml:"summary"`
	TagsDelta    []string `yaml:"tags_delta"`
	Source       Source   `yaml:"source"`
	Supersedes   []string `yaml:"supersedes"`
	SupersededBy *string  `yaml:"superseded_by"`
	Related      []string `yaml:"related"`
	CreatedAt    string   `yaml:"created_at"`
	UpdatedAt    string   `yaml:"updated_at"`
}

// Schema identifiers fixed by format v1.
const (
	RepoSchema  = "memorytree.repo/v1"
	NodeSchema  = "memorytree.node/v1"
	EntrySchema = "memorytree.entry/v1"

	FormatVersion = 1

	KindRoot     = "root"
	KindCategory = "category"
	KindProject  = "project"

	StatusActive    = "active"
	StatusArchived  = "archived"
	EntryDraft      = "draft"
	EntrySuperseded = "superseded"
	EntryDeprecated = "deprecated"

	TypeUser      = "user"
	TypeFeedback  = "feedback"
	TypeProject   = "project"
	TypeReference = "reference"

	SensitivityPublic   = "public"
	SensitivityInternal = "internal"
	SensitivityPrivate  = "private"

	GlobalSlug = "global"
)

// SensitivityRank maps a sensitivity level to an ordering where a larger
// value is stricter. Unknown levels return -1.
func SensitivityRank(level string) int {
	switch level {
	case SensitivityPublic:
		return 0
	case SensitivityInternal:
		return 1
	case SensitivityPrivate:
		return 2
	default:
		return -1
	}
}
