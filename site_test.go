package antedom

import (
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func demoSite(t *testing.T) *Site {
	t.Helper()
	pages, layout := t.TempDir(), t.TempDir()
	files := map[string]string{
		"index.html":     `<body><h1 ante:text="title"></h1></body>`,
		"sub/index.html": `<body><p ante:text="page.path"></p></body>`,
		"sub/plain.txt":  "passthrough",
		// A markdown page through the full pipeline: layout/fill
		// as raw HTML blocks, markdown between the blank lines,
		// an inline directive mid-paragraph.
		"md/index.md": "<template ante:layout=\"default.html\">\n\n" +
			"<template ante:fill=\"main\">\n\n" +
			"# Hi *there*\n\nOn <span ante:text=\"page.path\"></span> today.\n\n" +
			"</template>\n\n</template>\n",
	}
	for name, content := range files {
		p := filepath.Join(pages, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	base := `<html><body><main ante:slot="main"><p>fallback</p></main></body></html>`
	if err := os.WriteFile(filepath.Join(layout, "default.html"), []byte(base), 0o644); err != nil {
		t.Fatal(err)
	}
	return &Site{
		Pages:  pages,
		Layout: layout,
		Data:   func() (map[string]any, error) { return map[string]any{"title": "hi"}, nil },
	}
}

func TestSiteBuild(t *testing.T) {
	s := demoSite(t)
	out := t.TempDir()
	pages, err := s.Build(out)
	if err != nil {
		t.Fatal(err)
	}
	if pages != 3 {
		t.Errorf("built %d pages, want 3", pages)
	}
	for name, want := range map[string]string{
		"index.html":     "<h1>hi</h1>",
		"sub/index.html": "<p>/sub/</p>",
		"sub/plain.txt":  "passthrough",
		"md/index.html":  `<h1 id="hi-there">Hi <em>there</em></h1>`,
	} {
		data, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), want) {
			t.Errorf("%s: missing %q in %s", name, want, data)
		}
	}
}

func TestPageMeta(t *testing.T) {
	render := func(src string) (string, error) {
		pages := t.TempDir()
		if err := os.WriteFile(filepath.Join(pages, "index.html"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		s := &Site{Pages: pages, Data: func() (map[string]any, error) { return map[string]any{}, nil }}
		var buf strings.Builder
		err := s.render(&buf, "index.html")
		return buf.String(), err
	}

	out, err := render(`<body><script ante:meta type="application/json">
{
  "title": "Meta title",
  "weight": 2,
  "date": "2026-01-05",
  "params": { "author": "ada" }
}
</script><h1 ante:text="page.title"></h1><p ante:text="page.params.author"></p></body>`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<h1>Meta title</h1>", "<p>ada</p>"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
	if strings.Contains(out, "<script") {
		t.Errorf("metadata script shipped: %s", out)
	}

	meta := func(body string) string {
		return `<body><script ante:meta type="application/json">` + body + `</script></body>`
	}
	for name, src := range map[string]string{
		"unknown key":   meta(`{"author": "ada"}`),
		"wrong type":    meta(`{"weight": "heavy"}`),
		"bad date":      meta(`{"date": "January 5th"}`),
		"trailing data": meta(`{"weight": 1} {"weight": 2}`),
		"empty body":    meta(``),
		"non-script":    `<body><div ante:meta>{"weight": 1}</div></body>`,
		"two elements": `<body><script ante:meta type="application/json">{"weight": 1}</script>` +
			`<script ante:meta type="application/json">{"weight": 2}</script></body>`,
	} {
		if _, err := render(src); err == nil {
			t.Errorf("%s: no error for %s", name, src)
		}
	}
}

func TestSiteHandler(t *testing.T) {
	h := demoSite(t).Handler()
	for url, want := range map[string]string{
		"/":              "<h1>hi</h1>",
		"/sub/":          "<p>/sub/</p>",
		"/sub":           "<p>/sub/</p>", // extensionless resolves to index
		"/sub/plain.txt": "passthrough",
		"/md/":           "<p>On <span>/md/</span> today.</p>",
		"/md/index.html": `<h1 id="hi-there">Hi <em>there</em></h1>`, // .html falls back to .md source
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", url, nil))
		body, _ := io.ReadAll(rec.Result().Body)
		if rec.Code != 200 || !strings.Contains(string(body), want) {
			t.Errorf("%s: code %d, missing %q in %s", url, rec.Code, want, body)
		}
	}
	for _, url := range []string{"/nope/", "/md/index.md"} { // .md source is never served
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", url, nil))
		if rec.Code != 404 {
			t.Errorf("%s: code %d, want 404", url, rec.Code)
		}
	}
}
