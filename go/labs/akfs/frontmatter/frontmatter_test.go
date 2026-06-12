package frontmatter

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	const doc = `---
title: The components of a software factory
status: draft
tags: [software-factory, agents, infrastructure]
slides: content/slides/components-of-a-software-factory/slides.md
---

# Body content here
`
	got, err := Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	want := map[string]any{
		"title":  "The components of a software factory",
		"status": "draft",
		"tags":   []any{"software-factory", "agents", "infrastructure"},
		"slides": "content/slides/components-of-a-software-factory/slides.md",
	}
	if !reflect.DeepEqual(got.Frontmatter, want) {
		t.Errorf("Parse() frontmatter = %#v, want %#v", got.Frontmatter, want)
	}
	// The leading newline after the closing delimiter is stripped; the trailing
	// newline is preserved.
	if wantContent := "# Body content here\n"; got.Content != wantContent {
		t.Errorf("Parse() content = %q, want %q", got.Content, wantContent)
	}
}

// TestParseCRLF verifies that Windows-style line endings are handled when
// matching the delimiter lines.
func TestParseCRLF(t *testing.T) {
	doc := "---\r\ntitle: hi\r\n---\r\nbody\r\n"
	got, err := Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if got.Frontmatter["title"] != "hi" {
		t.Errorf("title = %v, want %q", got.Frontmatter["title"], "hi")
	}
}

// TestParseEmptyBlock verifies an empty frontmatter block yields an empty,
// non-nil map rather than an error.
func TestParseEmptyBlock(t *testing.T) {
	got, err := Parse(strings.NewReader("---\n---\nbody\n"))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if got.Frontmatter == nil {
		t.Fatal("Parse() returned nil map, want empty map")
	}
	if len(got.Frontmatter) != 0 {
		t.Errorf("Parse() = %#v, want empty map", got.Frontmatter)
	}
	// No blank line separates the closing delimiter from the body, so nothing
	// is stripped.
	if got.Content != "body\n" {
		t.Errorf("Parse() content = %q, want %q", got.Content, "body\n")
	}
}

// TestParseDocumentEndDelimiter verifies that a "..." line also closes the
// frontmatter block.
func TestParseDocumentEndDelimiter(t *testing.T) {
	got, err := Parse(strings.NewReader("---\ntitle: hi\n...\nbody\n"))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if got.Frontmatter["title"] != "hi" {
		t.Errorf("title = %v, want %q", got.Frontmatter["title"], "hi")
	}
}

// TestParseContent verifies how the document body is captured: the single
// newline separating the closing delimiter from the body is stripped, while a
// trailing newline (or its absence) is preserved verbatim.
func TestParseContent(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"blank line then body", "---\na: 1\n---\n\nbody\n", "body\n"},
		{"no blank line", "---\na: 1\n---\nbody\n", "body\n"},
		{"no trailing newline", "---\na: 1\n---\nbody", "body"},
		{"empty body", "---\na: 1\n---\n", ""},
		{"multiple trailing blank lines preserved", "---\na: 1\n---\n\nbody\n\n", "body\n\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(strings.NewReader(tt.input))
			if err != nil {
				t.Fatalf("Parse returned error: %v", err)
			}
			if got.Content != tt.want {
				t.Errorf("content = %q, want %q", got.Content, tt.want)
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr error
	}{
		{"empty input", "", ErrNoFrontmatter},
		{"no leading delimiter", "title: hi\n---\n", ErrNoFrontmatter},
		{"unterminated", "---\ntitle: hi\n", ErrUnterminated},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(strings.NewReader(tt.input))
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Parse() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
