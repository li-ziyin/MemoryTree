// Package docfile writes MemoryTree metadata files with a stable,
// human-readable YAML style (two-space indentation).
package docfile

import (
	"bytes"
	"os"

	"gopkg.in/yaml.v3"
)

// Marshal serialises v with two-space indentation.
func Marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// MarshalFrontmatter returns a complete frontmatter document.
func MarshalFrontmatter(v any, body []byte) ([]byte, error) {
	meta, err := Marshal(v)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	out.WriteString("---\n")
	out.Write(meta)
	out.WriteString("---\n")
	out.Write(body)
	return out.Bytes(), nil
}

// WriteFrontmatter writes a frontmatter document with owner-only permissions.
func WriteFrontmatter(path string, v any, body []byte) error {
	data, err := MarshalFrontmatter(v, body)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// WriteYAML writes a plain YAML document with owner-only permissions.
func WriteYAML(path string, v any) error {
	data, err := Marshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
