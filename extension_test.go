package antedom

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mrled/antedom/testsites"
)

func writeExtension(t *testing.T, root, src string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "antedom.js"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadProjectExtension(t *testing.T) {
	if ext, err := loadProjectExtension(t.TempDir()); err != nil || ext != nil {
		t.Fatalf("missing extension = (%v, %v), want (nil, nil)", ext, err)
	}

	for name, tc := range map[string]struct {
		src  string
		want string
	}{
		"minimal": {
			src: `antedom.apiVersion(1);`,
		},
		"empty": {
			want: "apiVersion(1) must be called",
		},
		"late version": {
			src:  `antedom.on("page:document", () => {}); antedom.apiVersion(1);`,
			want: "must be called before antedom.on",
		},
		"unsupported version": {
			src:  `antedom.apiVersion(2);`,
			want: "unsupported antedom API version",
		},
		"fractional version": {
			src:  `antedom.apiVersion(1.2);`,
			want: "unsupported antedom API version",
		},
		"version twice": {
			src:  `antedom.apiVersion(1); antedom.apiVersion(1);`,
			want: "may only be called once",
		},
		"unknown hook": {
			src:  `antedom.apiVersion(1); antedom.on("page:rendered", () => {});`,
			want: "unsupported hook",
		},
		"non-function": {
			src:  `antedom.apiVersion(1); antedom.on("page:document", 42);`,
			want: "must be a function",
		},
		"output before version": {
			src:  `antedom.output("manifest", 42);`,
			want: "must be called before antedom.output",
		},
		"manifest before version": {
			src:  `antedom.go.jsonManifest({file: "pages.json"});`,
			want: "must be called before antedom.go.jsonManifest",
		},
		"manifest missing file": {
			src:  `antedom.apiVersion(1); antedom.go.jsonManifest({});`,
			want: "file is required",
		},
		"manifest escapes output": {
			src:  `antedom.apiVersion(1); antedom.go.jsonManifest({file: "../pages.json"});`,
			want: "must stay within the output directory",
		},
		"invalid output": {
			src:  `antedom.apiVersion(1); antedom.output("manifest", {});`,
			want: "requires an antedom.go output",
		},
		"duplicate output name": {
			src: `antedom.apiVersion(1);
const a = antedom.go.jsonManifest({file: "a.json"});
const b = antedom.go.jsonManifest({file: "b.json"});
antedom.output("manifest", a); antedom.output("manifest", b);`,
			want: "already registered",
		},
		"duplicate output file": {
			src: `antedom.apiVersion(1);
antedom.output("a", antedom.go.jsonManifest({file: "pages.json"}));
antedom.output("b", antedom.go.jsonManifest({file: "pages.json"}));`,
			want: "output file",
		},
		"syntax error": {
			src:  `this is not valid {`,
			want: "SyntaxError",
		},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeExtension(t, root, tc.src)
			ext, err := loadProjectExtension(root)
			if tc.want == "" {
				if err != nil || ext == nil {
					t.Fatalf("load = (%v, %v), want extension", ext, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
			if !strings.Contains(err.Error(), "antedom.js") {
				t.Fatalf("error lacks configuration filename: %v", err)
			}
		})
	}
}

func TestProjectExtensionJSONManifest(t *testing.T) {
	root := t.TempDir()
	if err := testsites.Blog(root, 2); err != nil {
		t.Fatal(err)
	}
	writeExtension(t, root, `
antedom.apiVersion(1);
antedom.output("manifest", antedom.go.jsonManifest({file: "data/pages.json"}));
`)
	out := t.TempDir()
	built, err := NewProject(root).Operation(context.Background(), OperationBuild).Build(out)
	if err != nil {
		t.Fatal(err)
	}
	if built != 3 {
		t.Fatalf("built %d pages, want 3", built)
	}
	if _, err := os.Stat(filepath.Join(out, "index.html")); err != nil {
		t.Fatalf("default HTML output missing: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(out, "data", "pages.json"))
	if err != nil {
		t.Fatal(err)
	}
	var records []struct {
		Path       string         `json:"path"`
		OutputPath string         `json:"outputPath"`
		Format     SourceFormat   `json:"format"`
		Size       int            `json:"size"`
		Meta       map[string]any `json:"meta"`
	}
	if err := json.Unmarshal(data, &records); err != nil {
		t.Fatal(err)
	}
	if len(records) != 3 {
		t.Fatalf("manifest has %d records, want 3", len(records))
	}
	if records[0].Path != "/" || records[0].OutputPath != "index.html" || records[0].Size == 0 {
		t.Fatalf("first manifest record = %#v", records[0])
	}
}

func TestProjectExtensionOutputCollision(t *testing.T) {
	root := t.TempDir()
	if err := testsites.Blog(root, 1); err != nil {
		t.Fatal(err)
	}
	writeExtension(t, root, `
antedom.apiVersion(1);
antedom.output("manifest", antedom.go.jsonManifest({file: "index.html"}));
`)
	out := t.TempDir()
	_, err := NewProject(root).Operation(context.Background(), OperationBuild).Build(out)
	if err == nil || !strings.Contains(err.Error(), `conflicts with page index.html`) {
		t.Fatalf("output collision error = %v", err)
	}
	if entries, readErr := os.ReadDir(out); readErr != nil || len(entries) != 0 {
		t.Fatalf("failed begin wrote output: entries=%v error=%v", entries, readErr)
	}
}

func TestProjectExtensionPageDocument(t *testing.T) {
	root := t.TempDir()
	if err := testsites.Blog(root, 2); err != nil {
		t.Fatal(err)
	}
	writeExtension(t, root, `
antedom.apiVersion(1);
let firstHookRan = false;
antedom.on("page:document", (page) => {
  "use strict";
  if (page.urlPath === "/posts/post-0001/") {
    try { page.meta.title = "Changed"; } catch (_) {}
    if (page.meta.title !== "Post 0001") throw new Error("metadata changed");
    try { page.extra = "added"; } catch (_) {}
    if ("extra" in page) throw new Error("page property added");
    try { delete page.meta.title; } catch (_) {}
    if (page.meta.title !== "Post 0001") throw new Error("metadata deleted");
    try { page.document.extra = true; } catch (_) {}
    if ("extra" in page.document) throw new Error("document property added");
  }
  firstHookRan = true;
});
antedom.on("page:document", (page) => {
  if (!firstHookRan) throw new Error("hook order changed");
  firstHookRan = false;
});
`)
	out := t.TempDir()
	built, err := NewProject(root).Operation(context.Background(), OperationBuild).Build(out)
	if err != nil {
		t.Fatal(err)
	}
	if built != 3 {
		t.Fatalf("built %d pages, want 3", built)
	}
	body, err := os.ReadFile(filepath.Join(out, "posts", "post-0001", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "Changed") || !strings.Contains(string(body), "Post 0001") {
		t.Fatalf("hook mutated page metadata: %s", body)
	}
}

func TestProjectExtensionHookErrorContext(t *testing.T) {
	root := t.TempDir()
	if err := testsites.Blog(root, 1); err != nil {
		t.Fatal(err)
	}
	writeExtension(t, root, `
antedom.apiVersion(1);
antedom.on("page:document", (page) => {
  if (page.urlPath === "/posts/post-0001/") throw new Error("marker");
});
`)
	_, err := NewProject(root).Operation(context.Background(), OperationBuild).Build(t.TempDir())
	if err == nil {
		t.Fatal("hook error did not fail the build")
	}
	for _, want := range []string{"antedom.js", `hook "page:document"`, "posts/post-0001.md", "marker"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
}

func TestProjectExtensionHighlightsLiteralDocumentOnly(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"pages", "layout/shortcode"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"layout/base.html":                `<html><body><main ante:slot></main></body></html>`,
		"layout/shortcode/generated.html": `<pre><code class="language-go">package generated</code></pre>`,
		"pages/index.md": `<script ante:meta type="application/json">
{"title":"Highlight", "layout":"base.html"}
</script>

` + "```go\npackage literal\n```" + `

<shortcode-generated></shortcode-generated>
`,
	}
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeExtension(t, root, `
antedom.apiVersion(1);
antedom.on("page:document", (page) => {
  const count = page.document.highlight({style: "github", unknownLanguage: "error"});
  if (count !== 1) throw new Error(`+"`expected one literal block, got ${count}`"+`);
});
`)
	out := t.TempDir()
	if _, err := NewProject(root).Operation(context.Background(), OperationBuild).Build(out); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	result := string(body)
	if !strings.Contains(result, `<code class="language-go"><span`) || !strings.Contains(result, `>package</span>`) {
		t.Fatalf("literal code was not highlighted: %s", result)
	}
	if !strings.Contains(result, `<code class="language-go">package generated</code>`) {
		t.Fatalf("shortcode-generated code was unexpectedly highlighted: %s", result)
	}
}

func TestProjectExtensionHighlightErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		call string
		want string
	}{
		"non-object options": {
			call: `page.document.highlight(42)`,
			want: "document.highlight options must be an object",
		},
		"null options": {
			call: `page.document.highlight(null)`,
			want: "document.highlight options must be an object",
		},
		"unknown option": {
			call: `page.document.highlight({bogus: true})`,
			want: `unknown document.highlight option "bogus"`,
		},
		"bad unknownLanguage": {
			call: `page.document.highlight({unknownLanguage: "explode"})`,
			want: `unknownLanguage must be "ignore" or "error"`,
		},
		"unknown style": {
			call: `page.document.highlight({style: "no-such-style"})`,
			want: `unknown highlight style "no-such-style"`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := testsites.Blog(root, 1); err != nil {
				t.Fatal(err)
			}
			writeExtension(t, root, `
antedom.apiVersion(1);
antedom.on("page:document", (page) => { `+tc.call+`; });
`)
			_, err := NewProject(root).Operation(context.Background(), OperationBuild).Build(t.TempDir())
			if err == nil {
				t.Fatal("bad highlight call did not fail the build")
			}
			for _, want := range []string{tc.want, "antedom.js", `hook "page:document"`} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q lacks %q", err, want)
				}
			}
		})
	}
}

// writeCodeSite writes a one-page site whose layout evaluates page metadata
// and whose page holds a fenced code block, plus one verbatim asset.
func writeCodeSite(t *testing.T, root string) {
	t.Helper()
	files := map[string]string{
		"layout/base.html": `<html><head><title ante:text="page.title"></title></head><body><main ante:slot></main></body></html>`,
		"pages/index.md": `<script ante:meta type="application/json">
{"title":"Code", "layout":"base.html"}
</script>

` + "```go\npackage main\n```" + `
`,
		"pages/plain.txt": "verbatim asset\n",
	}
	for name, src := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

const highlightExtension = `
antedom.apiVersion(1);
antedom.on("page:document", (page) => {
  if (!page.meta.title) throw new Error("page metadata missing in hook");
  page.document.highlight({style: "github"});
});
`

func TestServeMatchesBuildWithExtension(t *testing.T) {
	root := t.TempDir()
	writeCodeSite(t, root)
	writeExtension(t, root, highlightExtension)

	out := t.TempDir()
	if _, err := NewProject(root).Operation(context.Background(), OperationBuild).Build(out); err != nil {
		t.Fatal(err)
	}
	built, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil {
		t.Fatal(err)
	}

	h := NewProject(root).Operation(context.Background(), OperationServe).Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 200 {
		t.Fatalf("serve status = %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `<code class="language-go"><span`) {
		t.Fatalf("served page not highlighted: %s", rec.Body.String())
	}
	if rec.Body.String() != string(built) {
		t.Fatalf("serve and build output differ:\nbuild: %s\nserve: %s", built, rec.Body.String())
	}
}

func TestServeReloadsExtensionPerRequest(t *testing.T) {
	root := t.TempDir()
	writeCodeSite(t, root)
	h := NewProject(root).Operation(context.Background(), OperationServe).Handler()

	get := func(url string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", url, nil))
		return rec
	}

	if rec := get("/"); rec.Code != 200 || strings.Contains(rec.Body.String(), "<span") {
		t.Fatalf("no-extension serve = %d, want plain page: %s", rec.Code, rec.Body.String())
	}

	writeExtension(t, root, highlightExtension)
	if rec := get("/"); rec.Code != 200 || !strings.Contains(rec.Body.String(), `<code class="language-go"><span`) {
		t.Fatalf("added extension not picked up: %d %s", rec.Code, rec.Body.String())
	}

	writeExtension(t, root, `antedom.on("page:document", () => {});`)
	if rec := get("/"); rec.Code != 500 || !strings.Contains(rec.Body.String(), "antedom.js") {
		t.Fatalf("broken extension = %d, want 500 naming antedom.js: %s", rec.Code, rec.Body.String())
	}
	if rec := get("/plain.txt"); rec.Code != 200 || !strings.Contains(rec.Body.String(), "verbatim asset") {
		t.Fatalf("verbatim asset with broken extension = %d: %s", rec.Code, rec.Body.String())
	}
}

func TestServeExtensionConcurrentRequests(t *testing.T) {
	root := t.TempDir()
	writeCodeSite(t, root)
	writeExtension(t, root, highlightExtension)
	h := NewProject(root).Operation(context.Background(), OperationServe).Handler()

	var wg sync.WaitGroup
	errs := make(chan error, 8*4)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 4 {
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
				if rec.Code != 200 || !strings.Contains(rec.Body.String(), `<code class="language-go"><span`) {
					errs <- fmt.Errorf("concurrent serve = %d: %s", rec.Code, rec.Body.String())
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestProjectExtensionDocumentHandleExpires(t *testing.T) {
	root := t.TempDir()
	if err := testsites.Blog(root, 1); err != nil {
		t.Fatal(err)
	}
	writeExtension(t, root, `
antedom.apiVersion(1);
let saved;
antedom.on("page:document", (page) => {
  if (saved) saved.highlight();
  saved = page.document;
});
`)
	_, err := NewProject(root).Operation(context.Background(), OperationBuild).Build(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "document handle has expired") {
		t.Fatalf("expired document handle error = %v", err)
	}
}
