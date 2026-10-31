package content

import (
	"encoding/json"
	"strings"
	"testing"
)

// The node and mark names the editor and the public renderer both handle.
var (
	allowedNodes = map[string]bool{"doc": true, "paragraph": true, "heading": true, "text": true, "hardBreak": true, "bulletList": true, "orderedList": true, "listItem": true, "blockquote": true, "codeBlock": true, "horizontalRule": true}
	allowedMarks = map[string]bool{"bold": true, "italic": true, "strike": true, "code": true, "link": true}
)

const sample = "# Title\n\n" +
	"###### Deep heading\n\n" +
	"Text with **bold**, *italic*, ~~strike~~, `code` and a [link](https://example.com).\n" +
	"A soft break and a hard one  \nhere.\n\n" +
	"- one\n- two\n  - nested\n\n" +
	"3. three\n4. four\n\n" +
	"> quoted\n\n" +
	"---\n\n" +
	"```python\nprint(1)\n\nprint(2)\n```\n\n" +
	"![A chart](https://example.com/chart.png)\n\n" +
	"<div>raw</div>\n\n" +
	"<https://auto.example>\n"

func walk(t *testing.T, n *Node, depth int) {
	t.Helper()
	if !allowedNodes[n.Type] {
		t.Errorf("node type %q is outside the safe subset", n.Type)
	}
	for _, m := range n.Marks {
		if !allowedMarks[m.Type] {
			t.Errorf("mark %q is outside the safe subset", m.Type)
		}
	}
	if n.Type == "heading" {
		if lvl := n.Attrs["level"].(int); lvl < 1 || lvl > 3 {
			t.Errorf("heading level %d", lvl)
		}
	}
	if n.Type == "text" && n.Text == "" {
		t.Error("empty text node")
	}
	for _, child := range n.Content {
		walk(t, child, depth+1)
	}
}

func TestOnlySafeNodes(t *testing.T) {
	doc := FromMarkdown([]byte(sample))
	if doc.Type != "doc" || doc.Version != 1 {
		t.Fatalf("root = %+v", doc)
	}
	walk(t, doc, 0)
}

func TestConversion(t *testing.T) {
	data, _ := json.Marshal(FromMarkdown([]byte(sample)))
	out := string(data)
	for _, want := range []string{
		`{"type":"heading","attrs":{"level":1},"content":[{"type":"text","text":"Title"}]}`,
		`{"type":"heading","attrs":{"level":3},"content":[{"type":"text","text":"Deep heading"}]}`,
		`{"type":"text","text":"bold","marks":[{"type":"bold"}]}`,
		`{"type":"text","text":"strike","marks":[{"type":"strike"}]}`,
		`{"type":"text","text":"link","marks":[{"type":"link","attrs":{"href":"https://example.com"}}]}`,
		`{"type":"text","text":". A soft break and a hard one"},{"type":"hardBreak"},{"type":"text","text":"here."}`,
		`{"type":"orderedList","attrs":{"start":3}`,
		`{"type":"codeBlock","attrs":{"language":"python"},"content":[{"type":"text","text":"print(1)\n\nprint(2)"}]}`,
		`{"type":"text","text":"A chart","marks":[{"type":"link","attrs":{"href":"https://example.com/chart.png"}}]}`,
		`"href":"https://auto.example"`,
		`{"type":"horizontalRule"}`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s\nin %s", want, out)
		}
	}
}

func TestRawHTMLIsKeptAsText(t *testing.T) {
	doc := FromMarkdown([]byte("<div>raw</div>\n"))
	if got := doc.Content[0].Content[0].Text; got != "<div>raw</div>" {
		t.Errorf("raw HTML = %q", got)
	}
}

func TestCodeBlockWithoutLanguage(t *testing.T) {
	data, _ := json.Marshal(FromMarkdown([]byte("```\nx\n```\n\n    indented\n")))
	for _, want := range []string{
		`{"type":"codeBlock","attrs":{"language":null},"content":[{"type":"text","text":"x"}]}`,
		`{"type":"codeBlock","attrs":{"language":null},"content":[{"type":"text","text":"indented"}]}`,
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("missing %s in %s", want, data)
		}
	}
}

func TestEmptyDocument(t *testing.T) {
	doc := FromMarkdown(nil)
	if len(doc.Content) != 1 || doc.Content[0].Type != "paragraph" {
		t.Errorf("empty markdown = %+v", doc.Content)
	}
}
