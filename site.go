package antedom

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Site is one site definition — where templates live and where data
// comes from — driven two ways: Build renders every page to an output
// directory, Handler renders pages per request. Both share render.
type Site struct {
	// Pages is a directory tree of page templates: each *.html file
	// renders at its own relative path, each *.md file is markdown
	// content rendering at its .html path, other files pass through.
	Pages string
	// Layout is a directory of layout templates, named by ante:layout
	// attributes (see docs/templating.md); empty disables composition.
	Layout string
	// Data assembles the JS scope shared by every page. It is called
	// per render so serve mode always sees fresh data; it must return
	// a fresh map (render adds page-specific keys).
	Data func() (map[string]any, error)
}

// render evaluates one page template (rel, slash-separated, relative
// to Pages) into w. Each render gets its own Engine: template JS can
// leak globals into a runtime, so none is reused across pages.
func (s *Site) render(w io.Writer, rel string) error {
	doc, err := s.parse(filepath.Join(s.Pages, filepath.FromSlash(rel)))
	if err != nil {
		return err
	}
	if s.Layout != "" {
		doc, err = Compose(doc, func(name string) (*html.Node, error) {
			return s.parse(filepath.Join(s.Layout, filepath.FromSlash(name)))
		})
		if err != nil {
			return fmt.Errorf("composing %s: %w", rel, err)
		}
	}
	data, err := s.Data()
	if err != nil {
		return fmt.Errorf("assembling data: %w", err)
	}
	data["page"] = map[string]any{"path": urlPath(rel)}
	pages, err := s.pageList()
	if err != nil {
		return fmt.Errorf("listing pages: %w", err)
	}
	data["pages"] = pages
	engine, err := New()
	if err != nil {
		return err
	}
	if err := engine.RenderDoc(w, doc, data); err != nil {
		return fmt.Errorf("rendering %s: %w", rel, err)
	}
	return nil
}

func (s *Site) parse(path string) (*html.Node, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if strings.HasSuffix(path, ".md") {
		return parseMarkdown(src)
	}
	return html.Parse(bytes.NewReader(src))
}

// pageList walks Pages and returns every page as {path, title, depth},
// sorted by path, for templates as the pages global (e.g. a nav).
// A page's title is the text of the first <h1> in its source —
// pre-composition, pre-render — falling back to its URL path.
// depth is directory nesting: 0 for top-level pages (an index page
// counts at its own directory's level, so /demo/ is 0), +1 per
// directory below that (/demo/attributes.html is 1).
// Sources are re-parsed per render so serve mode tracks edits.
func (s *Site) pageList() ([]map[string]any, error) {
	var pages []map[string]any
	err := filepath.WalkDir(s.Pages, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !strings.HasSuffix(p, ".html") && !strings.HasSuffix(p, ".md") {
			return nil
		}
		rel, err := filepath.Rel(s.Pages, p)
		if err != nil {
			return err
		}
		doc, err := s.parse(p)
		if err != nil {
			return err
		}
		u := urlPath(filepath.ToSlash(rel))
		title := u
		if h1 := find(doc, func(el *html.Node) bool { return el.DataAtom == atom.H1 }); h1 != nil {
			title = text(h1)
		}
		depth := strings.Count(strings.TrimSuffix(u, "/"), "/") - 1
		if depth < 0 {
			depth = 0
		}
		pages = append(pages, map[string]any{"path": u, "title": title, "depth": depth})
		return nil
	})
	sort.Slice(pages, func(i, j int) bool {
		return pages[i]["path"].(string) < pages[j]["path"].(string)
	})
	return pages, err
}

// text returns n's concatenated text content.
func text(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.WriteString(text(c))
	}
	return b.String()
}

// urlPath maps a page file to its URL path: demo/index.html -> /demo/.
func urlPath(rel string) string {
	if strings.HasSuffix(rel, ".md") {
		rel = strings.TrimSuffix(rel, ".md") + ".html"
	}
	p := "/" + strings.TrimSuffix(rel, "index.html")
	if strings.HasSuffix(p, ".html") || strings.HasSuffix(p, "/") {
		return p
	}
	return p + "/"
}

// Build renders the whole site into out, mirroring the Pages tree.
// It returns the number of pages rendered (passthrough files not
// counted).
func (s *Site) Build(out string) (int, error) {
	pages := 0
	err := filepath.WalkDir(s.Pages, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(s.Pages, p)
		if err != nil {
			return err
		}
		dst := filepath.Join(out, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if strings.HasSuffix(p, ".md") {
			dst = strings.TrimSuffix(dst, ".md") + ".html"
		} else if !strings.HasSuffix(p, ".html") {
			return copyFile(dst, p)
		}
		f, err := os.Create(dst)
		if err != nil {
			return err
		}
		defer f.Close()
		if err := s.render(f, filepath.ToSlash(rel)); err != nil {
			return err
		}
		pages++
		return f.Close()
	})
	return pages, err
}

// Handler serves the site per request: a trailing slash (or an
// extensionless path) resolves to its index.html, .html renders
// (from the .html template, or its .md source when absent),
// everything else is served verbatim. Mount under a prefix with
// http.StripPrefix.
func (s *Site) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rel := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if rel == "" || !strings.Contains(path.Base(rel), ".") {
			rel = path.Join(rel, "index.html")
		}
		if strings.HasSuffix(rel, ".md") {
			// Markdown is page source; it renders at the .html path.
			http.NotFound(w, r)
			return
		}
		if !strings.HasSuffix(rel, ".html") {
			http.ServeFile(w, r, filepath.Join(s.Pages, filepath.FromSlash(rel)))
			return
		}
		// Render to a buffer so an error returns a clean 500
		// instead of trailing a half-written page.
		var buf bytes.Buffer
		err := s.render(&buf, rel)
		if os.IsNotExist(err) {
			buf.Reset()
			err = s.render(&buf, strings.TrimSuffix(rel, ".html")+".md")
		}
		if err != nil {
			if os.IsNotExist(err) {
				http.NotFound(w, r)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(buf.Bytes())
	})
}

func copyFile(dst, src string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
