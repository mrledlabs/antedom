package antedom

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type recordingOutput struct {
	began, committed, aborted bool
	pages                     []string
	assets                    []string
	failPage                  bool
}

func (o *recordingOutput) Begin(context.Context, *BuildPlan) error {
	o.began = true
	return nil
}
func (o *recordingOutput) WritePage(_ context.Context, page *RenderedPage) error {
	if o.failPage {
		return errors.New("output failed")
	}
	if page.Document == nil || len(page.HTML) == 0 {
		return errors.New("output received an incomplete rendered page")
	}
	o.pages = append(o.pages, page.Page.OutputPath)
	return nil
}
func (o *recordingOutput) WriteAsset(_ context.Context, asset *Asset) error {
	o.assets = append(o.assets, asset.OutputPath)
	return nil
}
func (o *recordingOutput) Commit(context.Context) error { o.committed = true; return nil }
func (o *recordingOutput) Abort(context.Context) error  { o.aborted = true; return nil }

func demoSite(t *testing.T) *Site {
	t.Helper()
	pages, layout := t.TempDir(), t.TempDir()
	files := map[string]string{
		"index.html": `<template ante:layout="default.html">` +
			`<template ante:fill><h1 ante:text="title"></h1></template></template>`,
		"sub/index.html": `<template ante:layout="default.html">` +
			`<template ante:fill><p ante:text="page.path"></p></template></template>`,
		"sub/plain.txt": "passthrough",
		// No ante:layout: opaque files, not pages — passed through
		// verbatim, directives and all, and absent from pages.
		"sub/raw.html": `<p ante:text="title">opaque</p>`,
		"notes.md":     "# just notes\n",
		// A non-index page: renders to leaf/index.html, URL /leaf/.
		"leaf.md": "<template ante:layout=\"default.html\">\n\n" +
			"<template ante:fill>\n\nA *leaf* page.\n\n</template>\n\n</template>\n",
		// A markdown page through the full pipeline: layout/fill
		// as raw HTML blocks, markdown between the blank lines,
		// an inline directive mid-paragraph.
		"md/index.md": "<template ante:layout=\"default.html\">\n\n" +
			"<template ante:fill>\n\n" +
			"# Hi *there*\n\nOn <span ante:text=\"page.path\"></span> today.\n\n" +
			"</template>\n\n</template>\n",
		// The sugar form: no wrapper templates — the metadata names
		// the layout, the body fills its default slot.
		"sugar.md": "<script ante:meta type=\"application/json\">\n" +
			"{\"title\": \"Sugar\", \"layout\": \"default.html\"}\n</script>\n\n" +
			"A *sugar* page on <span ante:text=\"page.path\"></span>.\n",
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
	base := `<html><body><main ante:slot><p>fallback</p></main></body></html>`
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
	if pages != 5 {
		t.Errorf("built %d pages, want 5", pages)
	}
	for name, want := range map[string]string{
		"index.html":       "<h1>hi</h1>",
		"sub/index.html":   "<p>/sub/</p>",
		"sub/plain.txt":    "passthrough",
		"sub/raw.html":     `<p ante:text="title">opaque</p>`, // verbatim, unrendered
		"notes.md":         "# just notes",                    // verbatim, still .md
		"leaf/index.html":  "A <em>leaf</em> page.",           // non-index page, pretty URL
		"md/index.html":    `<h1 id="hi-there">Hi <em>there</em></h1>`,
		"sugar/index.html": "A <em>sugar</em> page on <span>/sugar/</span>.",
	} {
		data, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), want) {
			t.Errorf("%s: missing %q in %s", name, want, data)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "notes.html")); err == nil {
		t.Error("notes.html built; opaque markdown should not render")
	}
	if _, err := os.Stat(filepath.Join(out, "leaf.html")); err == nil {
		t.Error("leaf.html built; pages render only at their URL directory")
	}
}

func TestBuildPlanAndOutput(t *testing.T) {
	s := demoSite(t)
	plan, err := s.Plan()
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Pages) != 5 || len(plan.Assets) != 3 {
		t.Fatalf("plan has %d pages and %d assets, want 5 and 3", len(plan.Pages), len(plan.Assets))
	}
	out := &recordingOutput{}
	built, err := s.BuildWith(context.Background(), plan, out)
	if err != nil {
		t.Fatal(err)
	}
	if built != 5 || !out.began || !out.committed || out.aborted {
		t.Fatalf("output lifecycle: built=%d begin=%v commit=%v abort=%v", built, out.began, out.committed, out.aborted)
	}
	if len(out.pages) != 5 || len(out.assets) != 3 {
		t.Fatalf("output got %d pages and %d assets", len(out.pages), len(out.assets))
	}

	failing := &recordingOutput{failPage: true}
	if _, err := s.BuildWith(context.Background(), plan, failing); err == nil {
		t.Fatal("failing output returned no error")
	}
	if !failing.aborted || failing.committed {
		t.Fatalf("failed output lifecycle: commit=%v abort=%v", failing.committed, failing.aborted)
	}
}

func TestPageMeta(t *testing.T) {
	render := func(src string) (string, error) {
		pages, layout := t.TempDir(), t.TempDir()
		base := `<html><body><template ante:slot="main"></template></body></html>`
		if err := os.WriteFile(filepath.Join(layout, "base.html"), []byte(base), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(pages, "index.html"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		s := &Site{
			Pages:  pages,
			Layout: layout,
			Data:   func() (map[string]any, error) { return map[string]any{}, nil },
		}
		var buf strings.Builder
		err := s.render(&buf, "index.html")
		return buf.String(), err
	}
	page := func(meta, body string) string {
		return `<template ante:layout="base.html"><script ante:meta type="application/json">` +
			meta + `</script><template ante:fill="main">` + body + `</template></template>`
	}

	out, err := render(page(`
{
  "title": "Meta title",
  "weight": 2,
  "date": "2026-01-05",
  "params": { "author": "ada" }
}
`, `<h1 ante:text="page.title"></h1><p ante:text="page.params.author"></p>`))
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

	for name, src := range map[string]string{
		"unknown key":           page(`{"author": "ada"}`, ""),
		"wrong type":            page(`{"weight": "heavy"}`, ""),
		"both layout spellings": page(`{"layout": "base.html"}`, ""),
		"bad date":              page(`{"date": "January 5th"}`, ""),
		"trailing data":         page(`{"weight": 1} {"weight": 2}`, ""),
		"empty body":            page(``, ""),
		"non-script": `<template ante:layout="base.html"><div ante:meta>{"weight": 1}</div>` +
			`<template ante:fill="main"></template></template>`,
		"two elements": page(`{"weight": 1}`, "") +
			`<script ante:meta type="application/json">{"weight": 2}</script>`,
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
		"/sub/raw.html":  `<p ante:text="title">opaque</p>`, // opaque: verbatim
		"/notes.md":      "# just notes",                    // opaque: raw markdown served
		"/leaf/":         "A <em>leaf</em> page.",           // non-index page at its pretty URL
		"/md/":           "<p>On <span>/md/</span> today.</p>",
		"/md/index.html": `<h1 id="hi-there">Hi <em>there</em></h1>`, // URL's own .md source renders
		"/sugar/":        "A <em>sugar</em> page on <span>/sugar/</span>.",
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", url, nil))
		body, _ := io.ReadAll(rec.Result().Body)
		if rec.Code != 200 || !strings.Contains(string(body), want) {
			t.Errorf("%s: code %d, missing %q in %s", url, rec.Code, want, body)
		}
	}
	// A page template's own path redirects to its URL.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/leaf.html", nil))
	if loc := rec.Result().Header.Get("Location"); rec.Code != 301 || loc != "/leaf/" {
		t.Errorf("/leaf.html: code %d location %q, want 301 /leaf/", rec.Code, loc)
	}
	// Page-source .md is never served, and opaque .md renders nowhere.
	for _, url := range []string{"/nope/", "/md/index.md", "/leaf.md", "/notes.html"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", url, nil))
		if rec.Code != 404 {
			t.Errorf("%s: code %d, want 404", url, rec.Code)
		}
	}
}
