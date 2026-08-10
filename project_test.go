package antedom

import (
	"context"
	"errors"
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
