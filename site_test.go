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
	pages := t.TempDir()
	if err := os.MkdirAll(filepath.Join(pages, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"index.html":     `<body><h1 ante:text="title"></h1></body>`,
		"sub/index.html": `<body><p ante:text="page.path"></p></body>`,
		"sub/plain.txt":  "passthrough",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(pages, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return &Site{
		Pages: pages,
		Data:  func() (map[string]any, error) { return map[string]any{"title": "hi"}, nil },
	}
}

func TestSiteBuild(t *testing.T) {
	s := demoSite(t)
	out := t.TempDir()
	pages, err := s.Build(out)
	if err != nil {
		t.Fatal(err)
	}
	if pages != 2 {
		t.Errorf("built %d pages, want 2", pages)
	}
	for name, want := range map[string]string{
		"index.html":     "<h1>hi</h1>",
		"sub/index.html": "<p>/sub/</p>",
		"sub/plain.txt":  "passthrough",
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

func TestSiteHandler(t *testing.T) {
	h := demoSite(t).Handler()
	for url, want := range map[string]string{
		"/":              "<h1>hi</h1>",
		"/sub/":          "<p>/sub/</p>",
		"/sub":           "<p>/sub/</p>", // extensionless resolves to index
		"/sub/plain.txt": "passthrough",
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", url, nil))
		body, _ := io.ReadAll(rec.Result().Body)
		if rec.Code != 200 || !strings.Contains(string(body), want) {
			t.Errorf("%s: code %d, missing %q in %s", url, rec.Code, want, body)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/nope/", nil))
	if rec.Code != 404 {
		t.Errorf("missing page: code %d, want 404", rec.Code)
	}
}
