package antedom

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Template consumption: a <template> with ante: attributes is
// antedom's and unwraps; a plain one is the client's; ante:keep
// processes but retains.

func TestPlainTemplateShips(t *testing.T) {
	out := render(t, `<body><template id="row"><p ante:text="x">raw</p></template></body>`,
		map[string]any{"x": "evaluated"})
	for _, want := range []string{`<template id="row">`, `ante:text="x"`, "raw"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q: %s", want, out)
		}
	}
	if strings.Contains(out, "evaluated") {
		t.Errorf("client template contents were processed: %s", out)
	}
}

func TestTemplateWithDirectiveUnwraps(t *testing.T) {
	out := render(t, `<body><template ante:if="true"><p>x</p></template></body>`, nil)
	if strings.Contains(out, "<template") || !strings.Contains(out, "<p>x</p>") {
		t.Errorf("bad output: %s", out)
	}
}

func TestTemplateForUnwraps(t *testing.T) {
	out := render(t, `<body><template ante:for="x of xs"><i ante:text="x"></i></template></body>`,
		map[string]any{"xs": []any{"a", "b"}})
	if strings.Contains(out, "<template") || !strings.Contains(out, "<i>a</i><i>b</i>") {
		t.Errorf("bad output: %s", out)
	}
}

func TestTemplateKeep(t *testing.T) {
	out := render(t, `<body><template ante:keep ante:if="yes"><p ante:text="msg">m</p></template></body>`,
		map[string]any{"yes": true, "msg": "hi"})
	if !strings.Contains(out, "<template><p>hi</p></template>") {
		t.Errorf("kept template missing or unprocessed: %s", out)
	}
	out = render(t, `<body><template ante:keep ante:if="yes"><p>m</p></template><i>after</i></body>`,
		map[string]any{"yes": false})
	if strings.Contains(out, "<template") || !strings.Contains(out, "<i>after</i>") {
		t.Errorf("false ante:if left kept template behind: %s", out)
	}
}

// Layout composition through a Site: the docs/templating.md example,
// abridged.

func layoutSite(t *testing.T) *Site {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"layout/base.html": `<!DOCTYPE html>
<html>
<head>
  <title ante:text="data.site.title">site</title>
  <template ante:slot="head"></template>
</head>
<body>
  <nav><a href="/">home</a></nav>
  <main ante:slot><p>nothing here yet</p></main>
</body>
</html>`,
		"layout/section.html": `<template ante:layout="base.html">
  <template ante:fill>
    <nav class="section"><a href="/hello/">hello index</a></nav>
    <article ante:slot><p>pick a page</p></article>
  </template>
</template>`,
		"pages/index.html": `<template ante:layout="base.html">
  <template ante:fill><h1>top</h1></template>
</template>`,
		// The page's bare fill chains past the base's default slot —
		// already claimed by section.html — into section's own.
		"pages/hello/index.html": `<template ante:layout="section.html">
  <template ante:fill="head"><link rel="stylesheet" href="style.css"></template>
  <template ante:fill>
    <ul><li ante:for="d of data.demo" ante:text="d">x</li></ul>
  </template>
</template>`,
		"pages/fallback.html": `<template ante:layout="section.html"></template>`,
		"pages/plain.html":    `<body><p>no layout</p></body>`,
	}
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return &Site{
		Pages:  filepath.Join(dir, "pages"),
		Layout: filepath.Join(dir, "layout"),
		Data: func() (map[string]any, error) {
			return map[string]any{"data": map[string]any{
				"site": map[string]any{"title": "ellipsis"},
				"demo": []any{"one", "two"},
			}}, nil
		},
	}
}

func renderPage(t *testing.T, s *Site, rel string) string {
	t.Helper()
	var buf bytes.Buffer
	if err := s.render(&buf, rel); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestComposeChain(t *testing.T) {
	out := renderPage(t, layoutSite(t), "hello/index.html")
	t.Logf("rendered:\n%s", out)
	for _, want := range []string{
		"<title>ellipsis</title>",
		// page's head fill landed in head, template unwrapped
		`<link rel="stylesheet" href="style.css"/>`,
		"<nav><a href=\"/\">home</a></nav>",
		// intermediate's fill inside <main>, its template unwrapped
		`<main>`,
		`<nav class="section">`,
		// page's fill inside the intermediate's <article> slot
		"<article>",
		"<li>one</li><li>two</li>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
	for _, bad := range []string{"<template", "ante:", "nothing here yet", "pick a page"} {
		if strings.Contains(out, bad) {
			t.Errorf("output contains %q", bad)
		}
	}
}

func TestComposeSingleLevel(t *testing.T) {
	out := renderPage(t, layoutSite(t), "index.html")
	if !strings.Contains(out, "<main><h1>top</h1></main>") {
		t.Errorf("fill missing: %s", out)
	}
	if !strings.Contains(out, "<title>ellipsis</title>") {
		t.Errorf("base chrome missing: %s", out)
	}
}

func TestComposeFallbacks(t *testing.T) {
	out := renderPage(t, layoutSite(t), "fallback.html")
	// content slot unfilled: article keeps its fallback children;
	// head slot unfilled: its template unwraps to nothing.
	if !strings.Contains(out, "<article><p>pick a page</p></article>") {
		t.Errorf("fallback missing: %s", out)
	}
	if strings.Contains(out, "<template") || strings.Contains(out, "ante:") {
		t.Errorf("markers survived: %s", out)
	}
}

func TestNoLayoutUnchanged(t *testing.T) {
	out := renderPage(t, layoutSite(t), "plain.html")
	if !strings.Contains(out, "<p>no layout</p>") {
		t.Errorf("bad output: %s", out)
	}
}

func TestLayoutCycleErrors(t *testing.T) {
	s := layoutSite(t)
	loop := filepath.Join(s.Layout, "loop.html")
	if err := os.WriteFile(loop, []byte(`<template ante:layout="loop.html"></template>`), 0o644); err != nil {
		t.Fatal(err)
	}
	page := filepath.Join(s.Pages, "loop.html")
	if err := os.WriteFile(page, []byte(`<template ante:layout="loop.html"></template>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.render(&bytes.Buffer{}, "loop.html"); err == nil {
		t.Error("want chain-depth error, got nil")
	} else {
		t.Logf("error (as expected): %v", err)
	}
}
