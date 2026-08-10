package antedom

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectSite(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "data", "site.json"), []byte(`{"title":"Example"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	site := NewProject(root).Site()
	if site.Pages != filepath.Join(root, "pages") || site.Layout != filepath.Join(root, "layout") {
		t.Fatalf("unexpected site paths: pages=%q layout=%q", site.Pages, site.Layout)
	}
	data, err := site.Data()
	if err != nil {
		t.Fatal(err)
	}
	got := data["data"].(map[string]any)["site"].(map[string]any)["title"]
	if got != "Example" {
		t.Fatalf("data.site.title = %v, want Example", got)
	}
	if data["now"] == "" {
		t.Error("now is empty")
	}
}

func TestOperationNewPage(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "layout"), 0o755); err != nil {
		t.Fatal(err)
	}
	layout := `<!doctype html><html><body><main ante:slot></main></body></html>`
	if err := os.WriteFile(filepath.Join(root, "layout", "base.html"), []byte(layout), 0o644); err != nil {
		t.Fatal(err)
	}

	op := NewOperation(context.Background(), OperationNew, NewProject(root))
	dest, err := op.NewPage("base.html", "posts/hello-world")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "pages", "posts", "hello-world.md"); dest != want {
		t.Fatalf("destination = %q, want %q", dest, want)
	}
	src, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"title": "Hello world"`, `"layout": "base.html"`} {
		if !strings.Contains(string(src), want) {
			t.Errorf("new page missing %q:\n%s", want, src)
		}
	}
}

// artifactExtension registers a page:document hook plus two artifacts, so
// artifact parity between build paths also proves the hook ran: highlighting
// changes rendered page sizes, and sitemap.xml records them.
const artifactExtension = `
antedom.apiVersion(1);
antedom.on("page:document", (page) => {
  page.document.highlight({style: "github"});
});
antedom.output("sitemap", {
  file: "sitemap.xml",
  begin(_, output) { output.write("<urlset>\n"); },
  page(page, output) { output.write('<url path="' + page.urlPath + '" bytes="' + page.size + '"/>\n'); },
  end(_, output) { output.write("</urlset>\n"); },
});
antedom.output("manifest", antedom.go.jsonManifest({file: "manifest.json"}));
`

func TestOperationBuildArtifacts(t *testing.T) {
	root := t.TempDir()
	writeCodeSite(t, root)
	writeExtension(t, root, artifactExtension)

	full := t.TempDir()
	if _, err := NewProject(root).Operation(context.Background(), OperationBuild).Build(full); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	built, err := NewProject(root).Operation(context.Background(), OperationServe).buildArtifacts(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !built {
		t.Fatal("buildArtifacts reported nothing built")
	}

	for _, artifact := range []string{"sitemap.xml", "manifest.json"} {
		fromBuild, err := os.ReadFile(filepath.Join(full, artifact))
		if err != nil {
			t.Fatal(err)
		}
		fromArtifacts, err := os.ReadFile(filepath.Join(dir, artifact))
		if err != nil {
			t.Fatal(err)
		}
		if string(fromBuild) != string(fromArtifacts) {
			t.Errorf("%s differs from full build:\nbuild: %s\nartifacts: %s", artifact, fromBuild, fromArtifacts)
		}
	}
	for _, page := range []string{"index.html", "plain.txt"} {
		if _, err := os.Stat(filepath.Join(dir, page)); !os.IsNotExist(err) {
			t.Errorf("artifact build wrote site file %s: %v", page, err)
		}
	}
}

func TestOperationBuildArtifactsWithoutOutputs(t *testing.T) {
	root := t.TempDir()
	writeCodeSite(t, root)

	// No antedom.js at all.
	if built, err := NewProject(root).Operation(context.Background(), OperationServe).buildArtifacts(t.TempDir()); err != nil || built {
		t.Errorf("no extension: buildArtifacts = (%v, %v), want (false, nil)", built, err)
	}

	// An extension registering no outputs.
	writeExtension(t, root, `antedom.apiVersion(1);`)
	if built, err := NewProject(root).Operation(context.Background(), OperationServe).buildArtifacts(t.TempDir()); err != nil || built {
		t.Errorf("no outputs: buildArtifacts = (%v, %v), want (false, nil)", built, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewProject(root).Operation(ctx, OperationServe).buildArtifacts(t.TempDir()); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled artifact build error = %v, want context.Canceled", err)
	}
}

func TestServeArtifacts(t *testing.T) {
	root := t.TempDir()
	writeCodeSite(t, root)
	writeExtension(t, root, artifactExtension)

	full := t.TempDir()
	if _, err := NewProject(root).Operation(context.Background(), OperationBuild).Build(full); err != nil {
		t.Fatal(err)
	}

	h := NewProject(root).Operation(context.Background(), OperationServe).Handler()
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		return rec
	}

	for _, artifact := range []string{"sitemap.xml", "manifest.json"} {
		rec := get("/" + artifact)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /%s = %d: %s", artifact, rec.Code, rec.Body)
		}
		want, err := os.ReadFile(filepath.Join(full, artifact))
		if err != nil {
			t.Fatal(err)
		}
		if rec.Body.String() != string(want) {
			t.Errorf("served /%s differs from build:\nbuild: %s\nserve: %s", artifact, want, rec.Body)
		}
	}

	// Artifact routing leaves pages and misses alone.
	if rec := get("/"); rec.Code != http.StatusOK {
		t.Errorf("GET / = %d", rec.Code)
	}
	if rec := get("/absent.xml"); rec.Code != http.StatusNotFound {
		t.Errorf("GET /absent.xml = %d, want 404", rec.Code)
	}

	// A site edit invalidates the cached artifacts.
	extra := `<script ante:meta type="application/json">
{"title":"Extra", "layout":"base.html"}
</script>

more
`
	if err := os.WriteFile(filepath.Join(root, "pages", "extra.md"), []byte(extra), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := get("/sitemap.xml")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /sitemap.xml after edit = %d: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `path="/extra/"`) {
		t.Errorf("sitemap not rebuilt after page added:\n%s", rec.Body)
	}
}

func TestArtifactCacheWarm(t *testing.T) {
	root := t.TempDir()
	writeCodeSite(t, root)
	writeExtension(t, root, artifactExtension)

	cache := &artifactCache{op: NewProject(root).Operation(context.Background(), OperationServe)}
	cache.warm()
	if !cache.valid {
		t.Fatal("warm did not build the cache")
	}
	for _, artifact := range []string{"sitemap.xml", "manifest.json"} {
		if _, err := os.Stat(filepath.Join(cache.dir, artifact)); err != nil {
			t.Errorf("warm did not produce %s: %v", artifact, err)
		}
	}

	// Warming a project without an extension builds nothing and does not fail.
	bare := &artifactCache{op: NewProject(t.TempDir()).Operation(context.Background(), OperationServe)}
	bare.warm()
	if entries, err := os.ReadDir(bare.dir); err != nil || len(entries) != 0 {
		t.Errorf("bare warm cache = (%v, %v), want empty", entries, err)
	}
}

func TestServeArtifactsWithoutExtension(t *testing.T) {
	root := t.TempDir()
	writeCodeSite(t, root)
	h := NewProject(root).Operation(context.Background(), OperationServe).Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/sitemap.xml", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /sitemap.xml = %d, want 404", rec.Code)
	}
}

func TestOperationBoundaries(t *testing.T) {
	project := NewProject(t.TempDir())
	if _, err := NewOperation(context.Background(), OperationNew, project).Build(t.TempDir()); err == nil {
		t.Error("new operation unexpectedly ran build")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewOperation(ctx, OperationBuild, project).Build(t.TempDir())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled build error = %v, want context.Canceled", err)
	}
}
