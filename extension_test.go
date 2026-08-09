package antedom

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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
