package antedom

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Site is one site definition — where templates live and where data
// comes from — driven two ways: Build renders every page to an output
// directory, Handler renders pages per request. Both share render.
type Site struct {
	// Pages is a directory tree: *.html and *.md files bearing an
	// ante:layout element are page templates — each renders at its
	// own relative path, .md at its .html path — and every other
	// file (a layout-less .html or .md included) passes through
	// verbatim.
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
	pages, err := s.pageList()
	if err != nil {
		return fmt.Errorf("listing pages: %w", err)
	}
	data["pages"] = pages
	u := urlPath(rel)
	data["page"] = map[string]any{"path": u}
	for _, pm := range pages {
		if pm["path"] == u {
			data["page"] = pm
			break
		}
	}
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

// isPage reports whether the file is a page template: an .html or
// .md file bearing an ante:layout element. Every other file — a
// layout-less .html or .md included — is opaque: pageList skips it,
// Build copies it, and Handler serves it verbatim.
func (s *Site) isPage(path string) (bool, error) {
	if !strings.HasSuffix(path, ".html") && !strings.HasSuffix(path, ".md") {
		return false, nil
	}
	doc, err := s.parse(path)
	if err != nil {
		return false, err
	}
	return findAttr(doc, Prefix+"layout") != nil, nil
}

// pageMeta is a page's optional metadata — antedom's answer to
// frontmatter: a <script ante:meta type="application/json"> element
// anywhere in the page source, its body one JSON object allowing
// exactly these keys (anything else is a build error):
//
//	title  string — the page's title
//	weight number — sibling sort order (see pageInfo.before)
//	date   string — RFC 3339: a date (2006-01-02) or full timestamp
//	params object — free-form values for the site's own use
//
// The rendering walk drops the element, so metadata never ships.
type pageMeta struct {
	Title  string         `json:"title"`
	Date   string         `json:"date"`
	Weight *float64       `json:"weight"`
	Params map[string]any `json:"params"`
}

// takeMeta finds a page's ante:meta element, validates it, and fills
// info from it. No element is fine; two, a non-script host, malformed
// or unknown keys, or a bad date are errors.
func takeMeta(doc *html.Node, info *pageInfo) error {
	var metas []*html.Node
	find(doc, func(el *html.Node) bool {
		if _, ok := attrVal(el, Prefix+"meta"); ok {
			metas = append(metas, el)
		}
		return false // keep searching; find is our walker here
	})
	if len(metas) == 0 {
		return nil
	}
	if len(metas) > 1 {
		return fmt.Errorf("%d ante:meta elements, want at most one", len(metas))
	}
	sc := metas[0]
	if sc.DataAtom != atom.Script {
		return fmt.Errorf("ante:meta on <%s>, want <script>", sc.Data)
	}
	dec := json.NewDecoder(strings.NewReader(text(sc)))
	dec.DisallowUnknownFields()
	var meta pageMeta
	if err := dec.Decode(&meta); err != nil {
		return err
	}
	if dec.More() {
		return fmt.Errorf("trailing data after the metadata object")
	}
	if meta.Date != "" && !validDate(meta.Date) {
		return fmt.Errorf("date %q: want RFC 3339 (2006-01-02 or a full timestamp)", meta.Date)
	}
	info.title, info.date, info.weight, info.params = meta.Title, meta.Date, meta.Weight, meta.Params
	return nil
}

func validDate(s string) bool {
	for _, layout := range []string{"2006-01-02", time.RFC3339} {
		if _, err := time.Parse(layout, s); err == nil {
			return true
		}
	}
	return false
}

type pageInfo struct {
	path, title, date string
	weight            *float64
	params            map[string]any
	depth             int
}

// before orders siblings: by weight (ascending, weighted before
// unweighted), then date (newest first, dated before dateless),
// then title.
func (a *pageInfo) before(b *pageInfo) bool {
	switch {
	case a.weight != nil && b.weight != nil && *a.weight != *b.weight:
		return *a.weight < *b.weight
	case (a.weight != nil) != (b.weight != nil):
		return a.weight != nil
	}
	if a.date != b.date {
		if a.date == "" || b.date == "" {
			return b.date == ""
		}
		return a.date > b.date
	}
	return strings.ToLower(a.title) < strings.ToLower(b.title)
}

// pageList walks Pages and returns every page as
// {path, title, depth, date?, weight?, params?}, for templates as the
// pages global (e.g. a nav). All but path and depth come from the
// page's metadata (see pageMeta); a missing title falls back to the
// URL path. depth is directory nesting: 0 for top-level pages
// (an index page counts at its own directory's level, so /demo/ is
// 0), +1 per directory below that (/demo/attributes.html is 1).
// Order is hierarchical: siblings sort by before, and a section's
// pages follow its index page. Sources are re-parsed per render so
// serve mode tracks edits.
func (s *Site) pageList() ([]map[string]any, error) {
	groups := map[string][]*pageInfo{} // sibling group per directory
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
		if findAttr(doc, Prefix+"layout") == nil {
			return nil // opaque file, not a page (see isPage)
		}
		u := urlPath(filepath.ToSlash(rel))
		depth := strings.Count(strings.TrimSuffix(u, "/"), "/") - 1
		if depth < 0 {
			depth = 0
		}
		info := &pageInfo{path: u, depth: depth}
		if err := takeMeta(doc, info); err != nil {
			return fmt.Errorf("%s: page metadata: %w", p, err)
		}
		if info.title == "" {
			info.title = u
		}
		groups[groupOf(u)] = append(groups[groupOf(u)], info)
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, g := range groups {
		sort.SliceStable(g, func(i, j int) bool { return g[i].before(g[j]) })
	}
	var out []map[string]any
	emitted := map[string]bool{}
	var emit func(dir string)
	emit = func(dir string) {
		if emitted[dir] {
			return
		}
		emitted[dir] = true
		for _, pi := range groups[dir] {
			m := map[string]any{"path": pi.path, "title": pi.title, "depth": pi.depth}
			if pi.date != "" {
				m["date"] = pi.date
			}
			if pi.weight != nil {
				m["weight"] = *pi.weight
			}
			if pi.params != nil {
				m["params"] = pi.params
			}
			out = append(out, m)
			if pi.path != "/" && strings.HasSuffix(pi.path, "/") {
				emit(pi.path)
			}
		}
	}
	emit("/")
	var rest []string // directories with no index page above them
	for dir := range groups {
		if !emitted[dir] {
			rest = append(rest, dir)
		}
	}
	sort.Strings(rest)
	for _, dir := range rest {
		emit(dir)
	}
	return out, nil
}

// groupOf maps a page's URL path to its sibling group: the directory
// it is listed in. A section's index page is a sibling of its
// parent's pages (/demo/ groups under /), everything else groups
// under its own directory (/demo/attributes.html under /demo/).
func groupOf(u string) string {
	if u == "/" {
		return "/"
	}
	t := strings.TrimSuffix(u, "/")
	return t[:strings.LastIndex(t, "/")+1]
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
		if ok, err := s.isPage(p); err != nil {
			return err
		} else if !ok {
			return copyFile(dst, p)
		}
		if strings.HasSuffix(p, ".md") {
			dst = strings.TrimSuffix(dst, ".md") + ".html"
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
// extensionless path) resolves to its index.html, a page renders
// (from the .html template, or its .md source when absent), and
// everything else — layout-less .html and .md included — is served
// verbatim. Page-source .md is never served. Mount under a prefix
// with http.StripPrefix.
func (s *Site) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rel := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if rel == "" || !strings.Contains(path.Base(rel), ".") {
			rel = path.Join(rel, "index.html")
		}
		file := filepath.Join(s.Pages, filepath.FromSlash(rel))
		if ok, err := s.isPage(file); err != nil && !os.IsNotExist(err) {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		} else if err == nil && !ok {
			http.ServeFile(w, r, file)
			return
		}
		if strings.HasSuffix(rel, ".md") {
			// Markdown page source; it renders at the .html path.
			http.NotFound(w, r)
			return
		}
		// Render to a buffer so an error returns a clean 500
		// instead of trailing a half-written page.
		var buf bytes.Buffer
		err := s.render(&buf, rel)
		if os.IsNotExist(err) {
			// A missing .html falls back to its .md source, but only
			// when that source is itself a page.
			md := strings.TrimSuffix(rel, ".html") + ".md"
			if ok, mdErr := s.isPage(filepath.Join(s.Pages, filepath.FromSlash(md))); mdErr == nil && ok {
				buf.Reset()
				err = s.render(&buf, md)
			}
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
