package antedom

import (
	"bytes"
	htmltmpl "html/template"
	"io"
	"os"
	"testing"

	"golang.org/x/net/html"
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
