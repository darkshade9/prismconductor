package jira

import (
	"encoding/json"
	"fmt"
	"strings"
)

// adfNode is the minimal representation of an Atlassian Document Format node.
type adfNode struct {
	Type    string          `json:"type"`
	Content []adfNode       `json:"content,omitempty"`
	Attrs   json.RawMessage `json:"attrs,omitempty"`
	Text    string          `json:"text,omitempty"`
	Marks   []adfMark       `json:"marks,omitempty"`
}

type adfMark struct {
	Type  string          `json:"type"`
	Attrs json.RawMessage `json:"attrs,omitempty"`
}

// ADFToMarkdown converts Atlassian Document Format JSON to Markdown.
// Conversion is best-effort; unrecognised block types are skipped and unknown
// inline marks are stripped. The caller should fall back to a note pointing to
// the Jira issue if the source field is entirely unrecognised.
func ADFToMarkdown(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// ADF field may be a plain string on very old Jira Server instances.
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var doc adfNode
	if err := json.Unmarshal(raw, &doc); err != nil {
		return ""
	}
	var b strings.Builder
	renderBlock(&b, doc, 0)
	return strings.TrimSpace(b.String())
}

func renderBlock(b *strings.Builder, n adfNode, depth int) {
	switch n.Type {
	case "doc":
		for _, child := range n.Content {
			renderBlock(b, child, depth)
		}

	case "paragraph":
		for _, child := range n.Content {
			renderInline(b, child)
		}
		b.WriteString("\n\n")

	case "heading":
		level := 1
		if n.Attrs != nil {
			var attrs struct {
				Level int `json:"level"`
			}
			if json.Unmarshal(n.Attrs, &attrs) == nil && attrs.Level > 0 {
				level = attrs.Level
			}
		}
		b.WriteString(strings.Repeat("#", level) + " ")
		for _, child := range n.Content {
			renderInline(b, child)
		}
		b.WriteString("\n\n")

	case "bulletList":
		for _, item := range n.Content {
			b.WriteString(strings.Repeat("  ", depth) + "- ")
			renderListItem(b, item, depth)
		}
		if depth == 0 {
			b.WriteString("\n")
		}

	case "orderedList":
		for i, item := range n.Content {
			b.WriteString(strings.Repeat("  ", depth) + fmt.Sprintf("%d. ", i+1))
			renderListItem(b, item, depth)
		}
		if depth == 0 {
			b.WriteString("\n")
		}

	case "blockquote":
		for _, child := range n.Content {
			var inner strings.Builder
			renderBlock(&inner, child, 0)
			for _, line := range strings.Split(strings.TrimRight(inner.String(), "\n"), "\n") {
				b.WriteString("> " + line + "\n")
			}
		}
		b.WriteString("\n")

	case "codeBlock":
		lang := ""
		if n.Attrs != nil {
			var attrs struct {
				Language string `json:"language"`
			}
			if json.Unmarshal(n.Attrs, &attrs) == nil {
				lang = attrs.Language
			}
		}
		b.WriteString("```" + lang + "\n")
		for _, child := range n.Content {
			b.WriteString(child.Text)
		}
		b.WriteString("\n```\n\n")

	case "rule":
		b.WriteString("---\n\n")

	case "hardBreak":
		b.WriteString("\n")

	case "table":
		renderTable(b, n)

	case "panel":
		// Jira panels (info/warning/note/success/error) — render as blockquote.
		for _, child := range n.Content {
			var inner strings.Builder
			renderBlock(&inner, child, 0)
			for _, line := range strings.Split(strings.TrimRight(inner.String(), "\n"), "\n") {
				b.WriteString("> " + line + "\n")
			}
		}
		b.WriteString("\n")

	case "expand", "nestedExpand":
		var attrs struct {
			Title string `json:"title"`
		}
		if n.Attrs != nil {
			_ = json.Unmarshal(n.Attrs, &attrs)
		}
		if attrs.Title != "" {
			b.WriteString("**" + attrs.Title + "**\n\n")
		}
		for _, child := range n.Content {
			renderBlock(b, child, depth)
		}

	case "mediaSingle", "media", "mediaGroup":
		// Best-effort: emit a placeholder.
		b.WriteString("_(media attachment)_\n\n")

	default:
		// Unknown block type: recurse into children so text isn't lost.
		for _, child := range n.Content {
			renderBlock(b, child, depth)
		}
	}
}

func renderListItem(b *strings.Builder, item adfNode, depth int) {
	for i, child := range item.Content {
		switch child.Type {
		case "paragraph":
			if i == 0 {
				for _, c := range child.Content {
					renderInline(b, c)
				}
				b.WriteString("\n")
			} else {
				// Continuation paragraph inside a list item.
				b.WriteString(strings.Repeat("  ", depth+1))
				for _, c := range child.Content {
					renderInline(b, c)
				}
				b.WriteString("\n")
			}
		case "bulletList", "orderedList":
			renderBlock(b, child, depth+1)
		default:
			renderBlock(b, child, depth+1)
		}
	}
}

func renderInline(b *strings.Builder, n adfNode) {
	switch n.Type {
	case "text":
		text := n.Text
		text = applyMarks(text, n.Marks)
		b.WriteString(text)
	case "hardBreak":
		b.WriteString("\n")
	case "mention":
		var attrs struct {
			Text string `json:"text"`
		}
		if n.Attrs != nil {
			_ = json.Unmarshal(n.Attrs, &attrs)
		}
		if attrs.Text != "" {
			b.WriteString("@" + attrs.Text)
		}
	case "emoji":
		var attrs struct {
			Text string `json:"text"`
		}
		if n.Attrs != nil {
			_ = json.Unmarshal(n.Attrs, &attrs)
		}
		b.WriteString(attrs.Text)
	case "inlineCard":
		var attrs struct {
			URL string `json:"url"`
		}
		if n.Attrs != nil {
			_ = json.Unmarshal(n.Attrs, &attrs)
		}
		if attrs.URL != "" {
			b.WriteString("[" + attrs.URL + "](" + attrs.URL + ")")
		}
	default:
		// Recurse for unknown inline types.
		for _, child := range n.Content {
			renderInline(b, child)
		}
	}
}

func applyMarks(text string, marks []adfMark) string {
	for _, m := range marks {
		switch m.Type {
		case "strong":
			text = "**" + text + "**"
		case "em":
			text = "_" + text + "_"
		case "code":
			text = "`" + text + "`"
		case "strike":
			text = "~~" + text + "~~"
		case "link":
			var attrs struct {
				Href string `json:"href"`
			}
			if m.Attrs != nil {
				_ = json.Unmarshal(m.Attrs, &attrs)
			}
			if attrs.Href != "" {
				text = "[" + text + "](" + attrs.Href + ")"
			}
		case "underline":
			// Markdown has no underline; skip.
		}
	}
	return text
}

func renderTable(b *strings.Builder, n adfNode) {
	// Collect rows.
	type cell struct {
		header bool
		text   string
	}
	var rows [][]cell
	for _, row := range n.Content {
		if row.Type != "tableRow" {
			continue
		}
		var cols []cell
		for _, c := range row.Content {
			isHeader := c.Type == "tableHeader"
			var inner strings.Builder
			for _, child := range c.Content {
				renderBlock(&inner, child, 0)
			}
			text := strings.TrimSpace(inner.String())
			// Remove embedded newlines for table rendering.
			text = strings.ReplaceAll(text, "\n", " ")
			cols = append(cols, cell{header: isHeader, text: text})
		}
		rows = append(rows, cols)
	}
	if len(rows) == 0 {
		return
	}
	// Determine column count.
	cols := 0
	for _, r := range rows {
		if len(r) > cols {
			cols = len(r)
		}
	}
	// Render header row (first row) then separator.
	header := rows[0]
	b.WriteString("|")
	for i := 0; i < cols; i++ {
		text := ""
		if i < len(header) {
			text = header[i].text
		}
		b.WriteString(" " + text + " |")
	}
	b.WriteString("\n|")
	for i := 0; i < cols; i++ {
		b.WriteString(" --- |")
	}
	b.WriteString("\n")
	for _, row := range rows[1:] {
		b.WriteString("|")
		for i := 0; i < cols; i++ {
			text := ""
			if i < len(row) {
				text = row[i].text
			}
			b.WriteString(" " + text + " |")
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
}
