package antedom

import (
	"bytes"
	"strings"
	"testing"
)

func renderMarkdown(t *testing.T, src string, data map[string]any) string {
	t.Helper()
	doc, err := parseMarkdown([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	e, err := New()
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := e.RenderDoc(&buf, doc, data); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestMarkdownBasics(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"heading id",
			"# Hello World\n",
			`<h1 id="hello-world">Hello World</h1>`},
		{"inline html in paragraph",
			`some *em* <span class="n">inner *em*</span> after` + "\n",
			`<p>some <em>em</em> <span class="n">inner <em>em</em></span> after</p>`},
		{"fenced code keeps tags",
			"```\n<div>\n```\n",
			"<pre><code>&lt;div&gt;\n</code></pre>"},
		{"inline code keeps tags",
			"the `<div>` tag\n",
			"the <code>&lt;div&gt;</code> tag"},
	} {
		out := renderMarkdown(t, tc.src, nil)
		if !strings.Contains(out, tc.want) {
			t.Errorf("%s: missing %q in %s", tc.name, tc.want, out)
		}
	}
}

// Inline elements carrying directives sit mid-paragraph; markdown
// keeps flowing around them.
func TestMarkdownInlineDirectives(t *testing.T) {
	out := renderMarkdown(t, `Version <code ante:text="v"></code> is *current*.`+"\n",
		map[string]any{"v": "1.2"})
	want := `<p>Version <code>1.2</code> is <em>current</em>.</p>`
	if !strings.Contains(out, want) {
		t.Errorf("missing %q in %s", want, out)
	}
}

// Block-level HTML needs blank lines around the tags (CommonMark
// HTML-block rules); the markdown between them renders normally.
func TestMarkdownBlockHTML(t *testing.T) {
	out := renderMarkdown(t, `<div class="callout" ante:if="show">

## Inside

body text

</div>
`, map[string]any{"show": true})
	for _, want := range []string{
		`<div class="callout">`,
		`<h2 id="inside">Inside</h2>`,
		`<p>body text</p>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
}
