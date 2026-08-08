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
	if pages != 4 {
		t.Errorf("built %d pages, want 4", pages)
	}
	for name, wants := range map[string][]string{
		"index.html": {
			"<title>antedom docs</title>",
			`<link rel="icon" href="/icon.svg" type="image/svg+xml"/>`,
			`<h1 id="antedom-docs">antedom docs</h1>`,
			`<a href="templating.html">`,
			"<code>7</code> directives", // inline directive mid-markdown
			"<footer>rendered test</footer>",
		},
		"templating.html": {
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
	}
}
