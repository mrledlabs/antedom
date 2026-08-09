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
