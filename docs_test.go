package antedom

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDocsSite builds docs/ — the docs are themselves an antedom
// site (cmd/antedom serves them with -site docs) — so the docs
// cannot drift from the engine.
func TestDocsSite(t *testing.T) {
	s := &Site{
		Pages:  "docs/pages",
		Layout: "docs/layout",
		Data: func() (map[string]any, error) {
			src, err := os.ReadFile("docs/data/site.json")
			if err != nil {
				return nil, err
			}
			var site any
			if err := json.Unmarshal(src, &site); err != nil {
				return nil, err
			}
			return map[string]any{
				"now":  "test",
				"data": map[string]any{"site": site},
			}, nil
		},
	}
	out := t.TempDir()
	pages, err := s.Build(out)
	if err != nil {
		t.Fatal(err)
	}
	if pages != 9 {
		t.Errorf("built %d pages, want 9", pages)
	}
	for name, wants := range map[string][]string{
		"index.html": {
			"<title>antedom: docs</title>",
			`<link rel="icon" href="/icon.svg" type="image/svg+xml"/>`,
			`<a aria-current="page" href="/">docs</a>`,                         // sidebar marks the page
			`<a href="/templating.html">Templating: layouts, slots, fills</a>`, // title from that page's h1
			`<h1 id="docs">docs</h1>`,
			`<a href="templating.html">`,
			"<code>7</code> directives", // inline directive mid-markdown
			"<footer>rendered test</footer>",
		},
		"templating.html": {
			"<title>antedom: Templating: layouts, slots, fills</title>",
			`<a aria-current="page" href="/templating.html">Templating: layouts, slots, fills</a>`,
			`<h1 id="templating`,
			"&lt;template ante:layout=&#34;base.html&#34;&gt;", // fenced examples stay text
		},
		"markdown.html": {
			`<h1 id="markdown-pages">Markdown pages</h1>`,
			"<code>`&lt;div&gt;`</code>", // inline code keeps tags
		},
		"demo/index.html": {
			"<li>1. ante:if</li>",
			"<code>/demo/</code>",
		},
		"demo/attributes.html": {
			"<title>antedom: Bound attributes</title>",
			// A child page: indented under its parent via --depth.
			`<a aria-current="page" href="/demo/attributes.html" style="--depth: 1">Bound attributes</a>`,
			"this link to docs is bound",
		},
		"sampleblog/index.html": {
			// The post list: ante:for over the pages global, in order.
			"<a href=\"/sampleblog/third-post.html\">Third post</a>\n    — <time>2026-08-01</time>",
		},
		"sampleblog/first-post.html": {
			"<title>antedom: First post</title>",
			"<time>2026-06-10</time>", // page.date from metadata
			`<a aria-current="page" href="/sampleblog/first-post.html" style="--depth: 1">First post</a>`,
		},
		"sampleblog/second-post.html": {
			"<span>A. N. Author</span>", // page.params, free-form metadata
		},
		"style.css": {"nav a"},
		"icon.svg":  {"<svg"},
	} {
		data, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range wants {
			if !strings.Contains(string(data), want) {
				t.Errorf("%s: missing %q in %s", name, want, data)
			}
		}
		if strings.HasSuffix(name, ".html") && strings.Contains(string(data), "<template") {
			t.Errorf("%s: template element not unwrapped", name)
		}
		if strings.Contains(string(data), "<script ante:meta") {
			t.Errorf("%s: page metadata script not dropped", name)
		}
	}
	// Nav order: weight ascending, then date newest-first for the
	// unweighted blog posts, children after their section index.
	nav, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	last := -1
	for _, href := range []string{
		`href="/"`, `href="/templating.html"`, `href="/markdown.html"`,
		`href="/demo/"`, `href="/demo/attributes.html"`, `href="/sampleblog/"`,
		`href="/sampleblog/third-post.html"`, `href="/sampleblog/second-post.html"`,
		`href="/sampleblog/first-post.html"`,
	} {
		i := strings.Index(string(nav), href)
		if i < 0 || i < last {
			t.Errorf("nav order: %s at %d, after position %d", href, i, last)
		}
		last = i
	}
}
