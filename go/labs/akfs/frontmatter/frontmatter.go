// Package frontmatter extracts and parses YAML frontmatter from text
// documents. It is part of the experimental akfs labs tooling and carries no
// compatibility guarantees; see go/labs/README.md.
package frontmatter

import (
	"errors"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

// delimiter is the line that opens and closes a YAML frontmatter block.
const delimiter = "---"

// ErrNoFrontmatter is returned when the input does not begin with a frontmatter
// block (a line containing only "---").
var ErrNoFrontmatter = errors.New("no frontmatter found: input does not start with '---'")

// ErrUnterminated is returned when an opening "---" delimiter is found but the
// closing delimiter is missing.
var ErrUnterminated = errors.New("unterminated frontmatter: missing closing '---'")

// Document is the result of parsing a frontmatter document: the YAML
// frontmatter block and the body content that follows it.
type Document struct {
	// Frontmatter is the parsed YAML frontmatter. An empty block yields an
	// empty, non-nil map.
	Frontmatter map[string]any
	// Content is the document body following the closing delimiter, with the
	// single newline that separates the delimiter from the body stripped so
	// the content reads as though the frontmatter block were absent. Any
	// trailing newline at the end of the input is preserved.
	Content string
}

// Parse reads r and returns the parsed YAML frontmatter found at the start of
// the input along with the document body that follows it. The frontmatter must
// begin on the first line with a "---" delimiter and end with a matching "---"
// (or "...") delimiter line.
//
// An empty frontmatter block parses to an empty, non-nil map. Parse returns
// ErrNoFrontmatter if the input has no leading delimiter and ErrUnterminated if
// the closing delimiter is absent.
func Parse(r io.Reader) (Document, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return Document{}, err
	}
	s := string(raw)

	// The opening delimiter must be the first line.
	first, rest, hadNewline := strings.Cut(s, "\n")
	if strings.TrimRight(first, "\r") != delimiter {
		return Document{}, ErrNoFrontmatter
	}
	if !hadNewline {
		// Input was a single "---" line with no closing delimiter.
		return Document{}, ErrUnterminated
	}

	var body strings.Builder
	closed := false
	for {
		var line string
		line, rest, hadNewline = strings.Cut(rest, "\n")
		trimmed := strings.TrimRight(line, "\r")
		if trimmed == delimiter || trimmed == "..." {
			closed = true
			break
		}
		body.WriteString(trimmed)
		body.WriteByte('\n')
		if !hadNewline {
			// Reached the end of input without a closing delimiter.
			break
		}
	}
	if !closed {
		return Document{}, ErrUnterminated
	}

	// rest holds everything after the closing delimiter line. Strip the single
	// leading newline separating the delimiter from the body so the content
	// renders as though the frontmatter were absent; the trailing newline (if
	// any) is left untouched.
	content := rest
	switch {
	case strings.HasPrefix(content, "\r\n"):
		content = content[2:]
	case strings.HasPrefix(content, "\n"):
		content = content[1:]
	}

	out := map[string]any{}
	if err := yaml.Unmarshal([]byte(body.String()), &out); err != nil {
		return Document{}, err
	}
	if out == nil {
		out = map[string]any{}
	}
	return Document{Frontmatter: out, Content: content}, nil
}
