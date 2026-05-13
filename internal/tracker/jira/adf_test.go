package jira

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestADFToMarkdown_PlainString(t *testing.T) {
	raw := json.RawMessage(`"hello world"`)
	got := ADFToMarkdown(raw)
	if got != "hello world" {
		t.Errorf("expected %q, got %q", "hello world", got)
	}
}

func TestADFToMarkdown_Paragraph(t *testing.T) {
	raw := json.RawMessage(`{
		"version": 1,
		"type": "doc",
		"content": [{
			"type": "paragraph",
			"content": [{"type": "text", "text": "Hello, world!"}]
		}]
	}`)
	got := ADFToMarkdown(raw)
	if !strings.Contains(got, "Hello, world!") {
		t.Errorf("expected 'Hello, world!' in output, got %q", got)
	}
}

func TestADFToMarkdown_Heading(t *testing.T) {
	raw := json.RawMessage(`{
		"version": 1,
		"type": "doc",
		"content": [{
			"type": "heading",
			"attrs": {"level": 2},
			"content": [{"type": "text", "text": "My heading"}]
		}]
	}`)
	got := ADFToMarkdown(raw)
	if !strings.HasPrefix(got, "## My heading") {
		t.Errorf("expected heading prefix, got %q", got)
	}
}

func TestADFToMarkdown_BulletList(t *testing.T) {
	raw := json.RawMessage(`{
		"version": 1,
		"type": "doc",
		"content": [{
			"type": "bulletList",
			"content": [
				{"type": "listItem", "content": [{"type": "paragraph", "content": [{"type": "text", "text": "Item one"}]}]},
				{"type": "listItem", "content": [{"type": "paragraph", "content": [{"type": "text", "text": "Item two"}]}]}
			]
		}]
	}`)
	got := ADFToMarkdown(raw)
	if !strings.Contains(got, "- Item one") {
		t.Errorf("expected '- Item one' in output, got %q", got)
	}
	if !strings.Contains(got, "- Item two") {
		t.Errorf("expected '- Item two' in output, got %q", got)
	}
}

func TestADFToMarkdown_InlineMarks(t *testing.T) {
	raw := json.RawMessage(`{
		"version": 1,
		"type": "doc",
		"content": [{
			"type": "paragraph",
			"content": [
				{"type": "text", "text": "bold", "marks": [{"type": "strong"}]},
				{"type": "text", "text": " and "},
				{"type": "text", "text": "italic", "marks": [{"type": "em"}]}
			]
		}]
	}`)
	got := ADFToMarkdown(raw)
	if !strings.Contains(got, "**bold**") {
		t.Errorf("expected **bold** in output, got %q", got)
	}
	if !strings.Contains(got, "_italic_") {
		t.Errorf("expected _italic_ in output, got %q", got)
	}
}

func TestADFToMarkdown_CodeBlock(t *testing.T) {
	raw := json.RawMessage(`{
		"version": 1,
		"type": "doc",
		"content": [{
			"type": "codeBlock",
			"attrs": {"language": "go"},
			"content": [{"type": "text", "text": "fmt.Println(\"hi\")"}]
		}]
	}`)
	got := ADFToMarkdown(raw)
	if !strings.Contains(got, "```go") {
		t.Errorf("expected ```go in output, got %q", got)
	}
	if !strings.Contains(got, `fmt.Println("hi")`) {
		t.Errorf("expected code content in output, got %q", got)
	}
}

func TestADFToMarkdown_Empty(t *testing.T) {
	got := ADFToMarkdown(nil)
	if got != "" {
		t.Errorf("expected empty string for nil input, got %q", got)
	}
}

func TestADFToMarkdown_Table(t *testing.T) {
	raw := json.RawMessage(`{
		"version": 1,
		"type": "doc",
		"content": [{
			"type": "table",
			"content": [
				{"type": "tableRow", "content": [
					{"type": "tableHeader", "content": [{"type": "paragraph", "content": [{"type": "text", "text": "Name"}]}]},
					{"type": "tableHeader", "content": [{"type": "paragraph", "content": [{"type": "text", "text": "Age"}]}]}
				]},
				{"type": "tableRow", "content": [
					{"type": "tableCell", "content": [{"type": "paragraph", "content": [{"type": "text", "text": "Alice"}]}]},
					{"type": "tableCell", "content": [{"type": "paragraph", "content": [{"type": "text", "text": "30"}]}]}
				]}
			]
		}]
	}`)
	got := ADFToMarkdown(raw)
	if !strings.Contains(got, "| Name |") {
		t.Errorf("expected table header in output, got %q", got)
	}
	if !strings.Contains(got, "| Alice |") {
		t.Errorf("expected table row in output, got %q", got)
	}
}
