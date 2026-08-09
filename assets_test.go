package antedom

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// assetSite writes a site whose posts/ page bundles two text assets and
// whose root page has none; other/big.bin exercises the lazy read.
func assetSite(t *testing.T) *Site {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"layout/base.html": `<!DOCTYPE html><html><body><main ante:slot></main></body></html>`,
		"pages/index.html": `<template ante:layout="base.html"><template ante:fill>
			<p id="count" ante:text="page.assets.length"></p>
		</template></template>`,
		"pages/posts/index.html": `<template ante:layout="base.html"><template ante:fill>
			<ul><li ante:for="a of page.assets" ante:text="a.name + ' ' + a.url"></li></ul>
			<pre ante:text="page.assets.filter(a => a.name == 'b.txt')[0].text()"></pre>
		</template></template>`,
		"pages/posts/a.txt":   "alpha",
		"pages/posts/b.txt":   "beta contents",
		"pages/other/big.bin": "pretend this is huge",
		"pages/other/index.html": `<template ante:layout="base.html"><template ante:fill>
			<p ante:text="page.assets[0].name"></p>
		</template></template>`,
	}
	for name, src := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o777); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o666); err != nil {
			t.Fatal(err)
		}
	}
	return &Site{
		Pages:  filepath.Join(root, "pages"),
		Layout: filepath.Join(root, "layout"),
		Data:   func() (map[string]any, error) { return map[string]any{}, nil },
	}
}

func TestPageAssets(t *testing.T) {
	s := assetSite(t)
	var buf bytes.Buffer
	if err := s.render(&buf, "posts/index.html"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"a.txt /posts/a.txt",
		"b.txt /posts/b.txt",
		"beta contents",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("posts page: missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "big.bin") {
		t.Errorf("posts page sees another directory's asset:\n%s", out)
	}

	buf.Reset()
	if err := s.render(&buf, "index.html"); err != nil {
		t.Fatal(err)
	}
	if want := `<p id="count">0</p>`; !strings.Contains(buf.String(), want) {
		t.Errorf("root page: missing %q in:\n%s", want, buf.String())
	}
}

// A page listing an unreadable asset renders (text is lazy); calling
// text() on it fails the render.
func TestPageAssetsLazyRead(t *testing.T) {
	s := assetSite(t)
	locked := filepath.Join(s.Pages, "other", "big.bin")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(locked); err == nil {
		t.Skipf("read of a chmod-000 file succeeded (running as root?): %q", b)
	}
	var buf bytes.Buffer
	if err := s.render(&buf, "other/index.html"); err != nil {
		t.Fatalf("listing an unreadable asset should not read it: %v", err)
	}
	if want := "<p>big.bin</p>"; !strings.Contains(buf.String(), want) {
		t.Errorf("missing %q in:\n%s", want, buf.String())
	}

	page := filepath.Join(s.Pages, "other", "index.html")
	src := `<template ante:layout="base.html"><template ante:fill>
		<p ante:text="page.assets[0].text()"></p>
	</template></template>`
	if err := os.WriteFile(page, []byte(src), 0o666); err != nil {
		t.Fatal(err)
	}
	err := s.render(&buf, "other/index.html")
	if err == nil || !strings.Contains(err.Error(), "big.bin") {
		t.Errorf("text() on unreadable asset: want error naming big.bin, got %v", err)
	}
}
