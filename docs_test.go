package antedom

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDocsSite builds docs/ — the docs are themselves an antedom
// site (cmd/antedom serves them with -site docs) — so the docs
// cannot drift from the engine. It builds through the project layer
// so the docs' own antedom.js extension is exercised too.
func TestDocsSite(t *testing.T) {
	project := &Project{
		Root: "docs",
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
	pages, err := project.Operation(context.Background(), OperationBuild).Build(out)
	if err != nil {
		t.Fatal(err)
	}
	if pages != 16 {
		t.Errorf("built %d pages, want 16", pages)
	}
	for name, wants := range map[string][]string{
		"index.html": {
			"<title>antedom: antedom</title>",
			`<link rel="icon" href="/icon.svg" type="image/svg+xml"/>`,
			`<a aria-current="page" href="/">antedom</a>`, // sidebar marks the page
			`<a href="/templating/">Templating</a>`,       // title from that page's metadata
			"<h1>antedom</h1>",                            // the layout renders the metadata title
			`<a href="templating/">`,
			"<code>9</code> directives", // inline directive mid-markdown
			"<footer>rendered test</footer>",
		},
		"templating/index.html": {
			"<title>antedom: Templating</title>",
			`<a aria-current="page" href="/templating/">Templating</a>`,
			"<h1>Templating</h1>",
			// Fenced examples are highlighted by docs/antedom.js yet stay
			// escaped text: tags become token spans, not elements.
			`<code class="language-html"><span`,
			`&#34;base.html&#34;</span>`,
		},
		"markdown/index.html": {
			"<h1>Markdown pages</h1>",
			"<code>`&lt;div&gt;`</code>", // inline code keeps tags
		},
		"shortcodes/index.html": {
			"<title>antedom: Shortcodes</title>",
			// The live example, expanded from layout/shortcode/quotefig.html;
			// the figcaption link is built by its ante:scope script.
			`<blockquote cite="https://ask.metafilter.com/55153/Whats-the-middle-ground-between-FU-and-Welcome#830421">`,
			"<p>\n  This is a classic case of Ask Culture meets Guess Culture.\n</p>",
			`<figcaption><a href="https://ask.metafilter.com/55153/Whats-the-middle-ground-between-FU-and-Welcome#830421">tangerine on MetaFilter</a></figcaption>`,
			// Fenced examples stay escaped text (highlighted, not expanded).
			`&lt;<span style="color:#7ee787">shortcode-quotefig</span>`,
		},
		"demo/index.html": {
			"<li>1. ante:if</li>",
			"<code>/demo/</code>",
		},
		"demo/attributes/index.html": {
			"<title>antedom: Bound attributes</title>",
			// A child page: indented under its parent via --depth.
			`<a aria-current="page" href="/demo/attributes/" style="--depth: 1">Bound attributes</a>`,
			"this link to antedom is bound",
		},
		"sampleblog/index.html": {
			// The post list: ante:for over the pages global, in order.
			"<a href=\"/sampleblog/third-post/\">Third post</a>\n    — <time>2026-08-01</time>",
		},
		"sampleblog/first-post/index.html": {
			"<title>antedom: First post</title>",
			"<time>2026-06-10</time>", // byline date via page.date
			`<a aria-current="page" href="/sampleblog/first-post/" style="--depth: 1">First post</a>`,
			`<a href="/sampleblog/second-post/">← Second post</a>`,
			"<a>older →</a>", // oldest post: no href, disabled
		},
		"sampleblog/second-post/index.html": {
			"<span>· by A. N. Author</span>", // byline author from page.params
			`<a href="/sampleblog/third-post/">← Third post</a>`,
			`<a href="/sampleblog/first-post/">First post →</a>`,
		},
		"sampleblog/third-post/index.html": {
			"<a>← newer</a>", // newest post: no href, disabled
			`<a href="/sampleblog/second-post/">Second post →</a>`,
		},
		"pages/index.html": {"<h1>Pages</h1>"},
		"design/index.html": {
			"<h1>Design</h1>",
			`<a href="extensibility/">Extensibility</a>`,
		},
		"design/extensibility/index.html": {
			"<h1>Extensibility</h1>",
			"The extension system should be a small Go build kernel",
		},
		"style.css": {"nav a"},
		"icon.svg":  {"<svg"},
		// No ante:layout: opaque, copied verbatim, absent from the nav.
		"pages/non-page-example.md": {"# Non-page Markdown file example"},
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
		`href="/"`, `href="/templating/"`, `href="/pages/"`, `href="/markdown/"`,
		`href="/shortcodes/"`, `href="/demo/"`, `href="/demo/attributes/"`,
		`href="/design/"`, `href="/design/extensibility/"`, `href="/sampleblog/"`,
		`href="/sampleblog/third-post/"`, `href="/sampleblog/second-post/"`,
		`href="/sampleblog/first-post/"`,
	} {
		i := strings.Index(string(nav), href)
		if i < 0 || i < last {
			t.Errorf("nav order: %s at %d, after position %d", href, i, last)
		}
		last = i
	}
}
