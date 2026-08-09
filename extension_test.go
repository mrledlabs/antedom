package antedom

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"micahrl.com/antedom/testsites"
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
  if (!Object.isFrozen(page) || !Object.isFrozen(page.meta) || !Object.isFrozen(page.document)) {
    throw new Error("page export is mutable");
  }
  if (page.urlPath === "/posts/post-0001/") {
    try { page.meta.title = "Changed"; } catch (_) {}
    if (page.meta.title !== "Post 0001") throw new Error("metadata changed");
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
