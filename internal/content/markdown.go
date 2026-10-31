// Package content turns Markdown into the editor's ProseMirror (Tiptap) JSON,
// which the management API takes as content_structured.
//
// It only emits the subset that both the editor and the public site's
// renderer handle: paragraph, heading (levels 1-3), text, hardBreak,
// bulletList, orderedList, listItem, blockquote, codeBlock and
// horizontalRule, with the bold, italic, strike, code and link marks.
// Everything else degrades to that subset rather than being dropped:
//
//   - h4-h6 become level 3 headings;
//   - images become a link to the image, labelled with the alt text
//     (images need a media library ID, which Markdown does not carry);
//   - raw HTML and tables are kept as plain text.
package content

import (
	"bytes"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// Node is a ProseMirror node.
type Node struct {
	Type    string         `json:"type"`
	Version int            `json:"version,omitempty"`
	Attrs   map[string]any `json:"attrs,omitempty"`
	Content []*Node        `json:"content,omitempty"`
	Text    string         `json:"text,omitempty"`
	Marks   []Mark         `json:"marks,omitempty"`
}

// Mark is a ProseMirror mark.
type Mark struct {
	Type  string         `json:"type"`
	Attrs map[string]any `json:"attrs,omitempty"`
}

var md = goldmark.New(goldmark.WithExtensions(extension.Strikethrough))

// FromMarkdown converts Markdown to a Tiptap document.
func FromMarkdown(src []byte) *Node {
	root := md.Parser().Parse(text.NewReader(src))
	c := converter{src: src}
	doc := &Node{Type: "doc", Version: 1, Content: c.blocks(root)}
	if len(doc.Content) == 0 {
		doc.Content = []*Node{{Type: "paragraph"}}
	}
	return doc
}

type converter struct{ src []byte }

func (c *converter) blocks(parent ast.Node) []*Node {
	var out []*Node
	for n := parent.FirstChild(); n != nil; n = n.NextSibling() {
		out = append(out, c.block(n)...)
	}
	return out
}

func (c *converter) block(n ast.Node) []*Node {
	switch n := n.(type) {
	case *ast.Paragraph, *ast.TextBlock:
		return c.paragraphs(n)
	case *ast.Heading:
		return []*Node{{Type: "heading", Attrs: map[string]any{"level": min(n.Level, 3)}, Content: c.inlines(n, nil)}}
	case *ast.Blockquote:
		return []*Node{{Type: "blockquote", Content: nonEmpty(c.blocks(n))}}
	case *ast.List:
		list := &Node{Type: "bulletList"}
		if n.IsOrdered() {
			list.Type = "orderedList"
			list.Attrs = map[string]any{"start": n.Start}
		}
		for item := n.FirstChild(); item != nil; item = item.NextSibling() {
			list.Content = append(list.Content, &Node{Type: "listItem", Content: nonEmpty(c.blocks(item))})
		}
		return []*Node{list}
	case *ast.FencedCodeBlock:
		var lang any
		if l := n.Language(c.src); len(l) > 0 {
			lang = string(l)
		}
		return []*Node{codeBlock(lang, c.lines(n))}
	case *ast.CodeBlock:
		return []*Node{codeBlock(nil, c.lines(n))}
	case *ast.ThematicBreak:
		return []*Node{{Type: "horizontalRule"}}
	case *ast.HTMLBlock:
		raw := strings.TrimRight(c.lines(n), "\n")
		if raw == "" {
			return nil
		}
		return []*Node{{Type: "paragraph", Content: []*Node{{Type: "text", Text: raw}}}}
	}
	if n.Type() == ast.TypeBlock {
		raw := strings.TrimSpace(c.lines(n))
		if raw == "" {
			return nil
		}
		return []*Node{{Type: "paragraph", Content: []*Node{{Type: "text", Text: raw}}}}
	}
	return nil
}

func (c *converter) paragraphs(n ast.Node) []*Node {
	content := c.inlines(n, nil)
	if len(content) == 0 {
		return nil
	}
	return []*Node{{Type: "paragraph", Content: content}}
}

func codeBlock(lang any, code string) *Node {
	n := &Node{Type: "codeBlock", Attrs: map[string]any{"language": lang}}
	if code != "" {
		n.Content = []*Node{{Type: "text", Text: code}}
	}
	return n
}

func (c *converter) inlines(parent ast.Node, marks []Mark) []*Node {
	var out []*Node
	for n := parent.FirstChild(); n != nil; n = n.NextSibling() {
		out = append(out, c.inline(n, marks)...)
	}
	return mergeText(out)
}

func (c *converter) inline(n ast.Node, marks []Mark) []*Node {
	switch n := n.(type) {
	case *ast.Text:
		var out []*Node
		if s := string(n.Segment.Value(c.src)); s != "" {
			out = append(out, textNode(s, marks))
		}
		switch {
		case n.HardLineBreak():
			out = append(out, &Node{Type: "hardBreak"})
		case n.SoftLineBreak():
			out = append(out, textNode(" ", marks))
		}
		return out
	case *ast.String:
		return []*Node{textNode(string(n.Value), marks)}
	case *ast.Emphasis:
		mark := Mark{Type: "italic"}
		if n.Level >= 2 {
			mark.Type = "bold"
		}
		return c.inlines(n, with(marks, mark))
	case *east.Strikethrough:
		return c.inlines(n, with(marks, Mark{Type: "strike"}))
	case *ast.CodeSpan:
		return []*Node{textNode(plain(n, c.src), with(marks, Mark{Type: "code"}))}
	case *ast.Link:
		return c.inlines(n, with(marks, Mark{Type: "link", Attrs: map[string]any{"href": string(n.Destination)}}))
	case *ast.AutoLink:
		url := string(n.URL(c.src))
		href := url
		if n.AutoLinkType == ast.AutoLinkEmail && !strings.HasPrefix(href, "mailto:") {
			href = "mailto:" + href
		}
		return []*Node{textNode(string(n.Label(c.src)), with(marks, Mark{Type: "link", Attrs: map[string]any{"href": href}}))}
	case *ast.Image:
		alt := plain(n, c.src)
		if alt == "" {
			alt = string(n.Destination)
		}
		for _, m := range marks {
			if m.Type == "link" {
				return []*Node{textNode(alt, marks)}
			}
		}
		return []*Node{textNode(alt, with(marks, Mark{Type: "link", Attrs: map[string]any{"href": string(n.Destination)}}))}
	case *ast.RawHTML:
		var b bytes.Buffer
		for i := 0; i < n.Segments.Len(); i++ {
			seg := n.Segments.At(i)
			b.Write(seg.Value(c.src))
		}
		return []*Node{textNode(b.String(), marks)}
	}
	return c.inlines(n, marks)
}

func (c *converter) lines(n ast.Node) string {
	var b bytes.Buffer
	lines := n.Lines()
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		b.Write(seg.Value(c.src))
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func plain(n ast.Node, src []byte) string {
	var b strings.Builder
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		switch t := child.(type) {
		case *ast.Text:
			b.Write(t.Segment.Value(src))
		case *ast.String:
			b.Write(t.Value)
		default:
			b.WriteString(plain(child, src))
		}
	}
	return b.String()
}

func textNode(s string, marks []Mark) *Node {
	return &Node{Type: "text", Text: s, Marks: marks}
}

func with(marks []Mark, m Mark) []Mark {
	out := make([]Mark, 0, len(marks)+1)
	out = append(out, marks...)
	return append(out, m)
}

// mergeText joins neighbouring text nodes with the same marks.
func mergeText(nodes []*Node) []*Node {
	var out []*Node
	for _, n := range nodes {
		if n.Type == "text" && n.Text == "" {
			continue
		}
		if len(out) > 0 {
			last := out[len(out)-1]
			if last.Type == "text" && n.Type == "text" && sameMarks(last.Marks, n.Marks) {
				last.Text += n.Text
				continue
			}
		}
		out = append(out, n)
	}
	return out
}

func sameMarks(a, b []Mark) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Type != b[i].Type || a[i].Attrs["href"] != b[i].Attrs["href"] {
			return false
		}
	}
	return true
}

// nonEmpty gives container nodes the empty paragraph ProseMirror requires.
func nonEmpty(nodes []*Node) []*Node {
	if len(nodes) == 0 {
		return []*Node{{Type: "paragraph"}}
	}
	return nodes
}
