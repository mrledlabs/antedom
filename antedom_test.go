package antedom

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func render(t *testing.T, src string, data map[string]any) string {
	t.Helper()
	e, err := New()
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := e.Render(&buf, []byte(src), data); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestChronday(t *testing.T) {
	src, err := os.ReadFile("testdata/chronday.html")
	if err != nil {
		t.Fatal(err)
	}
	out := render(t, string(src), map[string]any{
		"site": map[string]any{"title": "ellipsis", "year": 2026},
		"page": map[string]any{
			"title": "Static site experiments",
			"draft": true,
			"tags":  []any{"go", "ssg"},
			"projects": []any{
				map[string]any{"slug": "ellipsisweb-ssg", "title": "Ellipsisweb SSG", "status": "active"},
				map[string]any{"slug": "hugo-exit", "title": "Hugo exit", "status": "someday"},
			},
			"body": "<p>Logic in <em>JS</em>, structure in HTML.</p>",
		},
	})
	t.Logf("rendered:\n%s", out)

	for _, want := range []string{
		"<title>Static site experiments · ellipsis</title>",
		`<p class="draft">Draft — not yet published.</p>`,
		// x/net/html sorts attributes at parse time, hence class first
		`<a class="active" href="/projects/ellipsisweb-ssg/">1. Ellipsisweb SSG</a>`,
		`<a href="/projects/hugo-exit/">2. Hugo exit</a>`,
		`<span class="tag">GO</span><span class="tag">SSG</span>`,
		"<h2>Tags</h2>", // template unwrapped, content kept
		"<p>Logic in <em>JS</em>, structure in HTML.</p>",
		"<footer>© 2026</footer>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
	if strings.Contains(out, "<template") {
		t.Error("template element not unwrapped")
	}
	if strings.Contains(out, "placeholder") {
		t.Error("placeholder content survived")
	}
}

func TestIfRemoves(t *testing.T) {
	out := render(t, `<body><p ante:if="hidden">secret</p><p>kept</p></body>`,
		map[string]any{"hidden": false})
	if strings.Contains(out, "secret") || !strings.Contains(out, "kept") {
		t.Errorf("bad output: %s", out)
	}
}

func TestTextEscapes(t *testing.T) {
	out := render(t, `<body><p ante:text="evil"></p></body>`,
		map[string]any{"evil": `<script>alert(1)</script>`})
	if strings.Contains(out, "<script>") {
		t.Errorf(":text did not escape: %s", out)
	}
	if !strings.Contains(out, "&lt;script&gt;") {
		t.Errorf("escaped text missing: %s", out)
	}
}

func TestBoundAttrs(t *testing.T) {
	out := render(t, `<body><input ante:disabled="off" ante:value="name"></body>`,
		map[string]any{"off": true, "name": `a"b`})
	if !strings.Contains(out, `disabled=""`) && !strings.Contains(out, "disabled>") &&
		!strings.Contains(out, "disabled ") {
		t.Errorf("boolean attr missing: %s", out)
	}
	if !strings.Contains(out, `value="a&#34;b"`) {
		t.Errorf("attr not escaped: %s", out)
	}
}

func TestNestedLoopScopes(t *testing.T) {
	out := render(t, `<body><div ante:for="row of rows"><i ante:for="x of row" ante:text="x"></i></div></body>`,
		map[string]any{"rows": []any{[]any{1, 2}, []any{3}}})
	if !strings.Contains(out, "<div><i>1</i><i>2</i></div><div><i>3</i></div>") {
		t.Errorf("bad output: %s", out)
	}
}

func TestExprError(t *testing.T) {
	e, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Render(&bytes.Buffer{}, []byte(`<body><p ante:text="nope.deref"></p></body>`), nil); err == nil {
		t.Error("want evaluation error, got nil")
	} else {
		t.Logf("error (as expected): %v", err)
	}
}
