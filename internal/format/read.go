// Package format reads MemoryTree metadata files. It enforces the file-level
// rules of format v1 that are independent of repository structure: UTF-8
// without BOM, a YAML frontmatter block, size limits, no symlinks, and a
// restricted YAML subset (no anchors, aliases, custom tags, or duplicate keys).
package format

import (
	"bytes"
	"errors"
	"os"
	"unicode/utf8"
)

const (
	// MaxFrontmatter is the format limit for the YAML block.
	MaxFrontmatter = 64 * 1024
	// MaxBody is the format limit for Markdown content following frontmatter.
	MaxBody = 1 << 20
	// MaxFileSize bounds what the reader is willing to load.
	MaxFileSize = MaxFrontmatter + MaxBody + 4096
)

var (
	ErrSymlink    = errors.New("managed file is a symbolic link")
	ErrNotRegular = errors.New("managed file is not a regular file")
	ErrBOM        = errors.New("file must not start with a UTF-8 BOM")
	ErrNotUTF8    = errors.New("file is not valid UTF-8")
	ErrNoFrontmat = errors.New("missing YAML frontmatter delimiter")
	ErrNoClosing  = errors.New("unterminated YAML frontmatter")
	ErrTooLarge   = errors.New("file exceeds the format size limit")
	ErrMetaTooBig = errors.New("frontmatter exceeds 64 KiB")
	ErrBodyTooBig = errors.New("body exceeds 1 MiB")
)

// Document is a parsed metadata file.
type Document struct {
	Meta []byte
	Body []byte
}

// ReadFile reads a managed metadata file. Symlinks and special files are
// rejected before any content is read.
func ReadFile(path string) (*Document, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrSymlink
	}
	if !info.Mode().IsRegular() {
		return nil, ErrNotRegular
	}
	if info.Size() > MaxFileSize {
		return nil, ErrTooLarge
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) > MaxFileSize {
		return nil, ErrTooLarge
	}
	return Split(data)
}

// ReadPlain reads a metadata file that has no frontmatter, such as
// memory.yaml. The same symlink, encoding, and size rules apply.
func ReadPlain(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrSymlink
	}
	if !info.Mode().IsRegular() {
		return nil, ErrNotRegular
	}
	if info.Size() > MaxFrontmatter {
		return nil, ErrMetaTooBig
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		return nil, ErrBOM
	}
	if !utf8.Valid(data) {
		return nil, ErrNotUTF8
	}
	return data, nil
}

// Split validates encoding and separates the frontmatter from the body.
func Split(data []byte) (*Document, error) {
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		return nil, ErrBOM
	}
	if !utf8.Valid(data) {
		return nil, ErrNotUTF8
	}

	firstEnd := bytes.IndexByte(data, '\n')
	if firstEnd < 0 {
		return nil, ErrNoFrontmat
	}
	if trimCR(data[:firstEnd]) != "---" {
		return nil, ErrNoFrontmat
	}

	metaStart := firstEnd + 1
	pos := metaStart
	for pos <= len(data) {
		lineEnd := bytes.IndexByte(data[pos:], '\n')
		var line []byte
		var next int
		if lineEnd < 0 {
			line = data[pos:]
			next = len(data)
		} else {
			line = data[pos : pos+lineEnd]
			next = pos + lineEnd + 1
		}
		if trimCR(line) == "---" {
			meta := data[metaStart:pos]
			body := data[next:]
			if len(meta) > MaxFrontmatter {
				return nil, ErrMetaTooBig
			}
			if len(body) > MaxBody {
				return nil, ErrBodyTooBig
			}
			return &Document{Meta: meta, Body: body}, nil
		}
		if lineEnd < 0 {
			break
		}
		pos = next
	}
	return nil, ErrNoClosing
}

func trimCR(b []byte) string {
	if len(b) > 0 && b[len(b)-1] == '\r' {
		b = b[:len(b)-1]
	}
	return string(b)
}
