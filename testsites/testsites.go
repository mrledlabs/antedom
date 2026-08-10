// Package testsites generates synthetic antedom projects for
// benchmarks and manual performance testing. Each generator writes a
// complete project directory (pages/, layout/, data/) from scratch;
// sites are generated on demand rather than checked in, so sizes can
// grow without bloating the repository. Output is deterministic for a
// given size, so timings are comparable across runs.
package testsites

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// sentences feed post bodies round-robin so files differ slightly.
var sentences = []string{
	"Lorem ipsum dolor sit amet, consectetur adipiscing elit.",
	"Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua.",
	"Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris.",
	"Duis aute irure dolor in reprehenderit in voluptate velit esse cillum.",
	"Excepteur sint occaecat cupidatat non proident, sunt in culpa qui officia.",
}

const blogSite = `{"title": "Bench Blog"}
`

const blogLayout = `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title ante:text="` + "`${page.title} · ${data.site.title}`" + `">title</title>
</head>
<body>
  <header><a href="/">home</a></header>
  <h1 ante:text="page.title">title</h1>
  <p class="byline" ante:if="page.date"><time ante:text="page.date">date</time></p>
  <main ante:slot>
    <p>nothing here yet</p>
  </main>
</body>
</html>
`

const blogIndex = `<template ante:layout="base.html">

<script ante:meta type="application/json">
{
  "title": "Home",
  "weight": 1
}
</script>

<template ante:fill>
  <ul class="posts">
    <li ante:for="p of pages.filter(p => p.path.startsWith('/posts/'))">
      <a ante:href="p.path" ante:text="p.title">post</a>
      <time ante:if="p.date" ante:text="p.date">date</time>
    </li>
  </ul>
</template>

</template>
`

const blogPost = `<script ante:meta type="application/json">
{
  "title": "Post %04d",
  "date": %q,
  "layout": "base.html"
}
</script>

%s %s
`

// BlogOptions controls optional benchmark features.
type BlogOptions struct {
	CodeBlocks int
	Extension  string

	// Expression-density knobs; at zero, posts are plain markdown.
	// Directives appends that many ante:text spans per post — with
	// UniqueExprs each span gets distinct expression source text,
	// otherwise all spans repeat one expression (each page renders in
	// a fresh Engine, so this is the difference between compiling the
	// expression once per page and once per span). LoopItems adds an
	// ante:for over that many generated items; ScopeStatements adds
	// an ante:scope script of that many string-building statements.
	Directives      int
	UniqueExprs     bool
	LoopItems       int
	ScopeStatements int
}

// Blog writes a blog-shaped project to dir: an index page listing
// every post, and n two-sentence markdown posts under posts/, all
// sugar pages composed into one base layout.
func Blog(dir string, n int) error {
	return BlogWithOptions(dir, n, BlogOptions{})
}

// BlogWithOptions is Blog with optional fenced code blocks per post and a
// project extension. Code blocks make highlighting cost independently
// measurable without changing the default benchmark fixture.
func BlogWithOptions(dir string, n int, options BlogOptions) error {
	for _, d := range []string{"pages/posts", "layout", "data"} {
		if err := os.MkdirAll(filepath.Join(dir, filepath.FromSlash(d)), 0o777); err != nil {
			return err
		}
	}
	for name, src := range map[string]string{
		"data/site.json":   blogSite,
		"layout/base.html": blogLayout,
		"pages/index.html": blogIndex,
	} {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(name)), []byte(src), 0o666); err != nil {
			return err
		}
	}
	if options.Extension != "" {
		if err := os.WriteFile(filepath.Join(dir, "antedom.js"), []byte(options.Extension), 0o666); err != nil {
			return err
		}
	}
	epoch := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range n {
		post := fmt.Sprintf(blogPost,
			i+1, epoch.AddDate(0, 0, i).Format("2006-01-02"),
			sentences[i%len(sentences)], sentences[(i+1)%len(sentences)])
		if blocks := blogCodeBlocks(options.CodeBlocks); blocks != "" {
			post += "\n" + blocks
		}
		if exprs := blogExpressions(options); exprs != "" {
			post += "\n" + exprs
		}
		name := fmt.Sprintf("post-%04d.md", i+1)
		if err := os.WriteFile(filepath.Join(dir, "pages", "posts", name), []byte(post), 0o666); err != nil {
			return err
		}
	}
	return nil
}

// blogExpressions renders a post's expression-heavy tail. The scope
// script comes first: its declarations are visible only to following
// siblings. The directive markup is contiguous HTML with no blank
// lines, so goldmark passes the whole block through without
// re-entering markdown parsing.
func blogExpressions(o BlogOptions) string {
	if o.Directives == 0 && o.LoopItems == 0 && o.ScopeStatements == 0 {
		return ""
	}
	var out strings.Builder
	if o.ScopeStatements > 0 || o.LoopItems > 0 {
		out.WriteString("<script ante:scope>\n")
		if o.ScopeStatements > 0 {
			out.WriteString("  let v0 = page.title;\n")
			for i := 1; i < o.ScopeStatements; i++ {
				fmt.Fprintf(&out, "  let v%d = v%d + %q;\n", i, i-1, " "+sentences[i%len(sentences)])
			}
			fmt.Fprintf(&out, "  const heavy = v%d.split(\" \").length;\n", o.ScopeStatements-1)
		}
		if o.LoopItems > 0 {
			fmt.Fprintf(&out, "  const items = Array.from({length: %d}, (_, i) => ({n: i, label: `item ${i}`}));\n", o.LoopItems)
		}
		out.WriteString("</script>\n\n")
	}
	if o.Directives > 0 || o.LoopItems > 0 {
		out.WriteString("<div class=\"expressions\">\n")
		for i := range o.Directives {
			if o.UniqueExprs {
				fmt.Fprintf(&out, "<span ante:text=\"`${page.title} · %d`\">d</span>\n", i)
			} else {
				out.WriteString("<span ante:text=\"`${page.title} · directive`\">d</span>\n")
			}
		}
		if o.LoopItems > 0 {
			out.WriteString("<ul>\n<li ante:for=\"item of items\"><a ante:href=\"'/items/' + item.n\" ante:text=\"item.label\">item</a></li>\n</ul>\n")
		}
		out.WriteString("</div>\n")
	}
	return out.String()
}

func blogCodeBlocks(count int) string {
	var out strings.Builder
	for i := range count {
		fmt.Fprintf(&out, "```go\nfunc example%d() string { return %q }\n```\n\n", i, sentences[i%len(sentences)])
	}
	return out.String()
}
