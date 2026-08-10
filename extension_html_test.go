package antedom

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectExtensionContentHTMLResolvesURLs(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"pages", "layout"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "layout", "base.html"), []byte(
		`<!doctype html><html><head><title>chrome</title></head><body><nav>chrome</nav><main ante:slot></main><footer>chrome</footer></body></html>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pages", "post.md"), []byte(`<script ante:meta type="application/json">
{"title":"Post","layout":"base.html"}
</script>

<p id="part">Read <a href="child/">next</a>, <a href="#part">again</a>.</p>
<img src="/image.png" alt="image">
<a href="javascript:alert(1)">script</a>
<a href="file:///tmp/example.txt">file</a>
<a href="vscode://file/tmp/example.go">editor</a>
`), 0o644); err != nil {
		t.Fatal(err)
	}
	writeExtension(t, root, `
antedom.apiVersion(1);
antedom.output("content", {
  file: "content.html",
  page(page, output) {
    const pageURL = antedom.url.resolve("https://example.com/site", page.urlPath);
    output.write(antedom.html.resolveURLs(page.contentHTML, pageURL));
  },
});
`)
	out := t.TempDir()
	if _, err := NewProject(root).Operation(context.Background(), OperationBuild).Build(out); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(out, "content.html"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, want := range []string{
		`href="https://example.com/site/post/child/"`,
		`href="https://example.com/site/post/#part"`,
		`src="https://example.com/image.png"`,
		`href="javascript:alert(1)"`,
		`href="file:///tmp/example.txt"`,
		`href="vscode://file/tmp/example.go"`,
	} {
		if !strings.Contains(content, want) {
			t.Errorf("resolved content missing %q: %s", want, content)
		}
	}
	for _, unwanted := range []string{"<!DOCTYPE", "<html", "<head", "<nav", "<footer", "chrome"} {
		if strings.Contains(content, unwanted) {
			t.Errorf("content contains layout chrome %q: %s", unwanted, content)
		}
	}
}
