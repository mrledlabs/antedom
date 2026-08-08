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
	"strings"
)

// Site is one site definition — where templates live and where data
// comes from — driven two ways: Build renders every page to an output
// directory, Handler renders pages per request. Both share render.
type Site struct {
	// Pages is a directory tree of page templates: each *.html file
	// renders at its own relative path, other files pass through.
	Pages string
	// Data assembles the JS scope shared by every page. It is called
	// per render so serve mode always sees fresh data; it must return
	// a fresh map (render adds page-specific keys).
	Data func() (map[string]any, error)
}

// render evaluates one page template (rel, slash-separated, relative
// to Pages) into w. Each render gets its own Engine: template JS can
// leak globals into a runtime, so none is reused across pages.
func (s *Site) render(w io.Writer, rel string) error {
	src, err := os.ReadFile(filepath.Join(s.Pages, filepath.FromSlash(rel)))
	if err != nil {
		return err
	}
	data, err := s.Data()
	if err != nil {
		return fmt.Errorf("assembling data: %w", err)
	}
	data["page"] = map[string]any{"path": urlPath(rel)}
	engine, err := New()
	if err != nil {
		return err
	}
	if err := engine.Render(w, src, data); err != nil {
		return fmt.Errorf("rendering %s: %w", rel, err)
	}
	return nil
}

// urlPath maps a page file to its URL path: demo/index.html -> /demo/.
func urlPath(rel string) string {
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
		if !strings.HasSuffix(p, ".html") {
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
// extensionless path) resolves to its index.html, .html renders,
// everything else is served verbatim. Mount under a prefix with
// http.StripPrefix.
func (s *Site) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rel := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if rel == "" || !strings.Contains(path.Base(rel), ".") {
			rel = path.Join(rel, "index.html")
		}
		if !strings.HasSuffix(rel, ".html") {
			http.ServeFile(w, r, filepath.Join(s.Pages, filepath.FromSlash(rel)))
			return
		}
		// Render to a buffer so an error returns a clean 500
		// instead of trailing a half-written page.
		var buf bytes.Buffer
		if err := s.render(&buf, rel); err != nil {
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
