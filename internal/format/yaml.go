package format

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

var (
	ErrYAML       = errors.New("invalid YAML frontmatter")
	ErrYAMLUnsafe = errors.New("YAML construct not allowed by format v1")
	ErrNotMapping = errors.New("frontmatter must be a YAML mapping")
	ErrEmptyMeta  = errors.New("frontmatter is empty")
	ErrMultiDoc   = errors.New("frontmatter must contain a single YAML document")
)

// Decode parses frontmatter YAML into out. Unknown fields are accepted; the
// dangerous YAML features listed in the package documentation are rejected.
func Decode(meta []byte, out any) error {
	if len(bytes.TrimSpace(meta)) == 0 {
		return ErrEmptyMeta
	}

	dec := yaml.NewDecoder(bytes.NewReader(meta))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		return fmt.Errorf("%w: %v", ErrYAML, err)
	}
	var extra yaml.Node
	err := dec.Decode(&extra)
	if err == nil {
		return ErrMultiDoc
	}
	if !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: %v", ErrYAML, err)
	}
	if len(doc.Content) == 0 {
		return ErrEmptyMeta
	}

	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return ErrNotMapping
	}
	if err := checkSafe(root); err != nil {
		return err
	}
	if err := root.Decode(out); err != nil {
		return fmt.Errorf("%w: %v", ErrYAML, err)
	}
	return nil
}

var allowedTags = map[string]bool{
	"!!map":       true,
	"!!seq":       true,
	"!!str":       true,
	"!!int":       true,
	"!!bool":      true,
	"!!null":      true,
	"!!float":     true,
	"!!timestamp": true,
	"!!binary":    true,
}

func checkSafe(n *yaml.Node) error {
	switch n.Kind {
	case yaml.AliasNode:
		return fmt.Errorf("%w: aliases are not allowed", ErrYAMLUnsafe)
	}
	if n.Anchor != "" {
		return fmt.Errorf("%w: anchors are not allowed", ErrYAMLUnsafe)
	}
	if n.Tag != "" && !allowedTags[n.Tag] {
		return fmt.Errorf("%w: tag %q is not allowed", ErrYAMLUnsafe, n.Tag)
	}

	switch n.Kind {
	case yaml.MappingNode:
		if len(n.Content)%2 != 0 {
			return fmt.Errorf("%w: malformed mapping", ErrYAML)
		}
		seen := make(map[string]struct{}, len(n.Content)/2)
		for i := 0; i < len(n.Content); i += 2 {
			key := n.Content[i]
			if key.Kind != yaml.ScalarNode {
				return fmt.Errorf("%w: non-scalar mapping keys are not allowed", ErrYAMLUnsafe)
			}
			if _, dup := seen[key.Value]; dup {
				return fmt.Errorf("%w: duplicate key %q", ErrYAML, key.Value)
			}
			seen[key.Value] = struct{}{}
		}
		for _, child := range n.Content {
			if err := checkSafe(child); err != nil {
				return err
			}
		}
	case yaml.SequenceNode:
		for _, child := range n.Content {
			if err := checkSafe(child); err != nil {
				return err
			}
		}
	}
	return nil
}
