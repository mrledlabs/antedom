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

// Blog writes a blog-shaped project to dir: an index page listing
// every post, and n two-sentence markdown posts under posts/, all
// sugar pages composed into one base layout.
func Blog(dir string, n int) error {
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
	epoch := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range n {
		post := fmt.Sprintf(blogPost,
			i+1, epoch.AddDate(0, 0, i).Format("2006-01-02"),
			sentences[i%len(sentences)], sentences[(i+1)%len(sentences)])
		name := fmt.Sprintf("post-%04d.md", i+1)
		if err := os.WriteFile(filepath.Join(dir, "pages", "posts", name), []byte(post), 0o666); err != nil {
			return err
		}
	}
	return nil
}
