package antedom

import (
	"bytes"
	"context"
	"fmt"
	htmltmpl "html/template"
	"io"
	"os"
	"testing"

	"golang.org/x/net/html"

	"github.com/mrledlabs/antedom/engine/testsites"
)

var benchData = map[string]any{
	"site": map[string]any{"title": "ellipsis", "year": 2026},
	"page": map[string]any{
		"title": "Static site experiments",
		"draft": true,
		"tags":  []any{"go", "ssg"},
		"projects": []any{
			map[string]any{"slug": "ellipsisweb-ssg", "title": "Ellipsisweb SSG", "status": "active"},
			map[string]any{"slug": "hugo-exit", "title": "Hugo exit", "status": "someday"},
		},
		"body": "<p>Logic in <em>JS</em>, structure in HTML.</p>",
	},
}

// Full pipeline: parse template + walk/eval + serialize, per render.
func BenchmarkRender(b *testing.B) {
	src, err := os.ReadFile("testdata/chronday.html")
	if err != nil {
		b.Fatal(err)
	}
	e, err := New()
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for b.Loop() {
		if err := e.Render(io.Discard, src, benchData); err != nil {
			b.Fatal(err)
		}
	}
}

// Just html.Parse of the template, to see how much of Render it is.
func BenchmarkParseOnly(b *testing.B) {
	src, err := os.ReadFile("testdata/chronday.html")
	if err != nil {
		b.Fatal(err)
	}
	for b.Loop() {
		if _, err := html.Parse(bytes.NewReader(src)); err != nil {
			b.Fatal(err)
		}
	}
}

// Whole-site build benchmarks share two size tiers. 1000 pages — a
// medium-large personal site — is the standard tier: every recorded
// config runs there, and ten samples resolve per-page regressions of a
// few percent. A 10000-page tier runs only where scale itself is the
// question: whole-build scaling (BuildBlog), O(site) accumulation
// (extension rss-feed), allocation-heavy per-page work (highlight
// 3-blocks). Per-page cost is flat across sizes, so smaller tiers
// measured nothing 1000 doesn't, and whole builds under ~100ms were
// dominated by process noise (40–120% CV on recorded runs); the 10- and
// 100-page tiers were removed. BenchmarkDiag holds one-off design
// questions whose name keeps them out of the recorded suite (see
// docs/pages/design/benchmarks/index.md).

// benchBuildBlog times whole-site builds of one generated blog site
// (see testsites) through the same Project/Operation path the CLI uses.
// pages/s counts rendered pages (posts + index), not opaque files.
func benchBuildBlog(b *testing.B, n int, options testsites.BlogOptions) {
	site := b.TempDir()
	if err := testsites.BlogWithOptions(site, n, options); err != nil {
		b.Fatal(err)
	}
	op := NewProject(site).Operation(context.Background(), OperationBuild)
	b.ReportAllocs()
	b.ResetTimer()
	pages := 0
	for b.Loop() {
		built, err := op.Build(b.TempDir())
		if err != nil {
			b.Fatal(err)
		}
		pages += built
	}
	elapsed := b.Elapsed()
	b.ReportMetric(float64(pages)/elapsed.Seconds(), "pages/s")
	b.ReportMetric(float64(elapsed.Microseconds())/float64(pages), "us/page")
	b.ReportMetric(elapsed.Seconds(), "wall-s")
}

// The unmodified pipeline at both tiers. One timed build per sample:
//
//	go test -run '^$' -bench BuildBlog -benchtime 1x
func BenchmarkBuildBlog(b *testing.B) {
	for _, n := range []int{1000, 10000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			benchBuildBlog(b, n, testsites.BlogOptions{})
		})
	}
}

// Extension overhead at the standard tier. "minimal" loads the runtime
// but registers nothing; "noop-hook" also crosses the complete read-only
// page boundary once per page; "manifest" writes HTML plus a streaming
// JSON page manifest, and "generated-manifest" generates the same
// manifest in JS. "xml-sitemap" and "rss-content" stream a small XML
// record per page. "rss-feed" is the accumulating shape from the docs
// site's feed — it materializes page.html for every page, holds all
// entries in memory, and emits one buffered document at end — so it
// alone also runs the 10000-page tier, where O(site) accumulation
// would show.
//
//	go test -run '^$' -bench 'BuildBlogExtension' -benchtime=1x -benchmem
func BenchmarkBuildBlogExtension(b *testing.B) {
	configs := []struct {
		name   string
		sizes  []int // nil means the standard tier only
		source string
	}{
		{"minimal", nil, `antedom.apiVersion(1);`},
		{"noop-hook", nil, `antedom.apiVersion(1); antedom.on("page:document", () => {});`},
		{"manifest", nil, `antedom.apiVersion(1);
antedom.output("manifest", antedom.go.jsonManifest({file: "pages.json"}));`},
		{"generated-manifest", nil, `antedom.apiVersion(1);
let first = true;
antedom.output("manifest", {
	  file: "pages.json",
	  begin(_, output) { output.write("[\n"); },
	  page(page, output) {
	    if (!first) output.write(",\n");
	    first = false;
	    output.writeJSON({
	      path: page.urlPath,
	      outputPath: page.outputPath,
	      format: page.format,
	      size: page.size,
	      meta: page.meta,
	    });
	    output.write("\n");
	  },
	  end(_, output) { output.write("]\n"); },
	});`},
		{"xml-sitemap", nil, "antedom.apiVersion(1);\n" +
			"const siteURL = \"https://example.com/docs\";\n" +
			"antedom.output(\"sitemap\", {\n" +
			"  file: \"sitemap.xml\", validate: \"xml\",\n" +
			"  begin(_, output) { output.write(antedom.xml`<urlset>\\n`); },\n" +
			"  page(page, output) {\n" +
			"    const location = antedom.url.resolve(siteURL, page.urlPath);\n" +
			"    output.write(antedom.xml`<url><loc>${location}</loc></url>\\n`);\n" +
			"  },\n" +
			"  end(_, output) { output.write(antedom.xml`</urlset>\\n`); },\n" +
			"});"},
		{"rss-feed", []int{1000, 10000}, "antedom.apiVersion(1);\n" +
			"const siteURL = \"https://example.com/docs\";\n" +
			"const entries = [];\n" +
			"antedom.output(\"rss\", {\n" +
			"  file: \"feed.xml\", validate: \"xml\",\n" +
			"  page(page) {\n" +
			"    if (!page.meta.date) return;\n" +
			"    entries.push({\n" +
			"      title: page.meta.title,\n" +
			"      date: page.meta.date,\n" +
			"      url: antedom.url.resolve(siteURL, page.urlPath),\n" +
			"      html: page.html,\n" +
			"    });\n" +
			"  },\n" +
			"  end(_, output) {\n" +
			"    entries.sort((a, b) => b.date.localeCompare(a.date));\n" +
			"    const items = entries.map(e => antedom.xml`    <item><title>${e.title}</title><link>${e.url}</link><guid isPermaLink=\"true\">${e.url}</guid><pubDate>${e.date}</pubDate><description>${e.html}</description></item>\\n`);\n" +
			"    output.write(antedom.xml`<rss version=\"2.0\"><channel><title>Bench Blog</title><link>${siteURL}</link>\\n${antedom.xml.join(items)}</channel></rss>\\n`);\n" +
			"  },\n" +
			"});"},
		{"rss-content", nil, "antedom.apiVersion(1);\n" +
			"const siteURL = \"https://example.com/docs\";\n" +
			"antedom.output(\"rss\", {\n" +
			"  file: \"feed.xml\", validate: \"xml\",\n" +
			"  begin(_, output) { output.write(antedom.xml`<rss><channel>\\n`); },\n" +
			"  page(page, output) {\n" +
			"    const pageURL = antedom.url.resolve(siteURL, page.urlPath);\n" +
			"    const content = antedom.html.resolveURLs(page.contentHTML, pageURL);\n" +
			"    output.write(antedom.xml`<item><link>${pageURL}</link><description>${content}</description></item>\\n`);\n" +
			"  },\n" +
			"  end(_, output) { output.write(antedom.xml`</channel></rss>\\n`); },\n" +
			"});"},
	}
	for _, config := range configs {
		sizes := config.sizes
		if sizes == nil {
			sizes = []int{1000}
		}
		for _, n := range sizes {
			b.Run(fmt.Sprintf("%d/%s", n, config.name), func(b *testing.B) {
				benchBuildBlog(b, n, testsites.BlogOptions{Extension: config.source})
			})
		}
	}
}

// Expression-heavy pages at the standard tier; BuildBlog/1000 is the
// no-expressions baseline. Each page renders in a fresh Engine (site.go:
// template JS can leak globals), so directive expressions compile per
// page and the compiled-expression cache dedupes only within a page.
// "directives" repeats one expression across twenty spans per post
// (compile once per page, evaluate per span), "loop" stresses subtree
// cloning plus a child scope per iteration, "scope" an ante:scope
// script's parse and eval, and "combined" all three, roughly the shape
// of a data-driven docs page.
//
//	go test -run '^$' -bench 'BuildBlogExpressions' -benchtime=1x -benchmem
func BenchmarkBuildBlogExpressions(b *testing.B) {
	configs := []struct {
		name    string
		options testsites.BlogOptions
	}{
		{"directives", testsites.BlogOptions{Directives: 20}},
		{"loop", testsites.BlogOptions{LoopItems: 50}},
		{"scope", testsites.BlogOptions{ScopeStatements: 40}},
		{"combined", testsites.BlogOptions{Directives: 20, LoopItems: 50, ScopeStatements: 40}},
	}
	for _, config := range configs {
		b.Run(fmt.Sprintf("1000/%s", config.name), func(b *testing.B) {
			benchBuildBlog(b, 1000, config.options)
		})
	}
}

// Go-backed highlighting with zero, one, and several fenced code blocks per
// post. This separates hook/traversal overhead from work that should scale
// with the number and size of code blocks. 3-blocks, the allocation-heaviest
// recorded config, also runs the 10000-page tier.
//
//	go test -run '^$' -bench 'BuildBlogHighlight' -benchtime=1x -benchmem
func BenchmarkBuildBlogHighlight(b *testing.B) {
	const extension = `antedom.apiVersion(1);
antedom.on("page:document", page => page.document.highlight({style: "github"}));`
	run := func(n, blocks int) {
		b.Run(fmt.Sprintf("%d/%d-blocks", n, blocks), func(b *testing.B) {
			benchBuildBlog(b, n, testsites.BlogOptions{CodeBlocks: blocks, Extension: extension})
		})
	}
	for _, blocks := range []int{0, 1, 3} {
		run(1000, blocks)
	}
	run(10000, 3)
}

// Diagnostics: benchmarks that answered a one-time design question, kept
// runnable but out of the recorded suite — the recording command's
// 'BenchmarkBuildBlog' pattern deliberately misses the Diag name.
//
//	go test -run '^$' -bench 'Diag' -benchtime=1x -benchmem
func BenchmarkDiag(b *testing.B) {
	// Raw string concatenation versus the antedom.xml tagged template;
	// compare with BuildBlogExtension/1000/xml-sitemap.
	b.Run("1000/raw-xml-sitemap", func(b *testing.B) {
		benchBuildBlog(b, 1000, testsites.BlogOptions{Extension: `antedom.apiVersion(1);
const siteURL = "https://example.com/docs";
antedom.output("sitemap", {
  file: "sitemap.xml", validate: "xml",
  begin(_, output) { output.write("<urlset>\n"); },
  page(page, output) { output.write("<url><loc>" + siteURL + page.urlPath + "</loc></url>\n"); },
  end(_, output) { output.write("</urlset>\n"); },
});`})
	})
	// Distinct expression source text per span, defeating the per-page
	// compiled-expression cache; compare with
	// BuildBlogExpressions/1000/directives to price compilation.
	b.Run("1000/unique-exprs", func(b *testing.B) {
		benchBuildBlog(b, 1000, testsites.BlogOptions{Directives: 20, UniqueExprs: true})
	})
}

// The same page as a Go html/template — roughly what Hugo executes.
func BenchmarkGoHTMLTemplate(b *testing.B) {
	tmpl := htmltmpl.Must(htmltmpl.New("p").Parse(`<!DOCTYPE html>
<html>
<head><title>{{.page.title}} · {{.site.title}}</title></head>
<body>
  <h1>{{.page.title}}</h1>
  {{if .page.draft}}<p class="draft">Draft — not yet published.</p>{{end}}
  <ul class="projects">
    {{range $i, $p := .page.projects}}<li>
      <a href="/projects/{{$p.slug}}/"{{if eq $p.status "active"}} class="active"{{end}}>{{$i}}. {{$p.title}}</a>
    </li>{{end}}
  </ul>
  {{if .page.tags}}<h2>Tags</h2>
    {{range .page.tags}}<span class="tag">{{.}}</span>{{end}}
  {{end}}
  <div class="body">{{.page.body}}</div>
  <footer>© {{.site.year}}</footer>
</body>
</html>`))
	b.ResetTimer()
	for b.Loop() {
		if err := tmpl.Execute(io.Discard, benchData); err != nil {
			b.Fatal(err)
		}
	}
}
