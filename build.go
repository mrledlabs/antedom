package antedom

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/net/html"
)

// SourceFormat identifies the source representation of a page.
type SourceFormat string

const (
	SourceHTML     SourceFormat = "html"
	SourceMarkdown SourceFormat = "markdown"
)

// Page is the lightweight, generation-wide description of a page. Source and
// DOM content are deliberately absent: those values live only while the page
// is being rendered.
type Page struct {
	SourcePath string
	RelPath    string // slash-separated, relative to Site.Pages
	URLPath    string
	OutputPath string // slash-separated, relative to an output root
	Format     SourceFormat
	Meta       map[string]any
}

// Asset is an opaque source file copied without rendering.
type Asset struct {
	SourcePath string
	RelPath    string // slash-separated, relative to Site.Pages
	OutputPath string // slash-separated, relative to an output root
}

// BuildPlan is one discovery snapshot, treated as immutable during a build. It
// retains only page metadata and paths, not page sources, parsed DOMs, or
// rendered output.
type BuildPlan struct {
	Pages  []*Page
	Assets []*Asset

	pageData []map[string]any
}

// pageAssets returns the opaque files sharing rel's source directory,
// in name order: the page's bundle, exposed to templates as
// page.assets (see assetScope). Sibling pages in one directory share a
// bundle; there is no leaf/branch distinction.
func (p *BuildPlan) pageAssets(rel string) []*Asset {
	dir := path.Dir(rel)
	var out []*Asset
	for _, a := range p.Assets {
		if path.Dir(a.RelPath) == dir {
			out = append(out, a)
		}
	}
	return out
}

// RenderedPage is the ephemeral result passed to outputs. Document and HTML
// remain valid for the duration of WritePage; outputs retaining them must copy
// what they need.
type RenderedPage struct {
	Page     *Page
	Document *html.Node
	HTML     []byte
}

// PageDocumentHook runs after layout composition and before template
// directives and serialization. The document is valid only during the call.
type PageDocumentHook func(context.Context, *Page, *html.Node) error

// BuildOptions supplies optional build-pipeline behavior.
type BuildOptions struct {
	PageDocument PageDocumentHook
}

// Output consumes a build incrementally. Commit publishes a successful build;
// Abort cleans up after any earlier failure.
type Output interface {
	Begin(context.Context, *BuildPlan) error
	WritePage(context.Context, *RenderedPage) error
	WriteAsset(context.Context, *Asset) error
	Commit(context.Context) error
	Abort(context.Context) error
}

// HTMLOutput writes the conventional directory tree produced by Site.Build.
type HTMLOutput struct {
	Dir string
}

func NewHTMLOutput(dir string) *HTMLOutput { return &HTMLOutput{Dir: dir} }

func (o *HTMLOutput) Begin(context.Context, *BuildPlan) error { return nil }

func (o *HTMLOutput) WritePage(ctx context.Context, page *RenderedPage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dst := filepath.Join(o.Dir, filepath.FromSlash(page.Page.OutputPath))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, page.HTML, 0o666)
}

func (o *HTMLOutput) WriteAsset(ctx context.Context, asset *Asset) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dst := filepath.Join(o.Dir, filepath.FromSlash(asset.OutputPath))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return copyFile(dst, asset.SourcePath)
}

func (o *HTMLOutput) Commit(context.Context) error { return nil }
func (o *HTMLOutput) Abort(context.Context) error  { return nil }

// Plan discovers every page and asset and parses page metadata exactly once.
func (s *Site) Plan() (*BuildPlan, error) {
	return s.PlanContext(context.Background())
}

// PlanContext is Plan with cancellation support.
func (s *Site) PlanContext(ctx context.Context) (*BuildPlan, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	plan := &BuildPlan{}
	var infos []*pageInfo
	err := filepath.WalkDir(s.Pages, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(s.Pages, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !strings.HasSuffix(p, ".html") && !strings.HasSuffix(p, ".md") {
			plan.Assets = append(plan.Assets, &Asset{SourcePath: p, RelPath: rel, OutputPath: rel})
			return nil
		}
		doc, err := s.parse(p)
		if err != nil {
			return err
		}
		if findAttr(doc, Prefix+"layout") == nil {
			plan.Assets = append(plan.Assets, &Asset{SourcePath: p, RelPath: rel, OutputPath: rel})
			return nil
		}
		u := urlPath(rel)
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
		format := SourceHTML
		if strings.HasSuffix(p, ".md") {
			format = SourceMarkdown
		}
		page := &Page{
			SourcePath: p,
			RelPath:    rel,
			URLPath:    u,
			OutputPath: strings.TrimPrefix(u, "/") + "index.html",
			Format:     format,
			Meta:       pageMetaMap(info),
		}
		plan.Pages = append(plan.Pages, page)
		infos = append(infos, info)
		return nil
	})
	if err != nil {
		return nil, err
	}
	plan.pageData = orderedPageData(infos)
	return plan, nil
}

// BuildWith renders a previously discovered plan into output. Page DOMs and
// bytes are released after each WritePage call.
func (s *Site) BuildWith(ctx context.Context, plan *BuildPlan, output Output) (pages int, err error) {
	return s.BuildWithOptions(ctx, plan, output, BuildOptions{})
}

// BuildWithOptions is BuildWith with optional pipeline hooks.
func (s *Site) BuildWithOptions(ctx context.Context, plan *BuildPlan, output Output, options BuildOptions) (pages int, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if plan == nil {
		return 0, fmt.Errorf("build: nil plan")
	}
	if output == nil {
		return 0, fmt.Errorf("build: nil output")
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err = output.Begin(ctx, plan); err != nil {
		return 0, fmt.Errorf("beginning output: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = output.Abort(context.WithoutCancel(ctx))
		}
	}()

	for _, asset := range plan.Assets {
		if err = output.WriteAsset(ctx, asset); err != nil {
			return pages, fmt.Errorf("writing asset %s: %w", asset.RelPath, err)
		}
	}
	for _, page := range plan.Pages {
		if err = ctx.Err(); err != nil {
			return pages, err
		}
		doc, body, renderErr := s.renderPageWithHook(ctx, page, plan.pageData, plan.pageAssets(page.RelPath), options.PageDocument)
		if renderErr != nil {
			return pages, renderErr
		}
		if err = output.WritePage(ctx, &RenderedPage{Page: page, Document: doc, HTML: body}); err != nil {
			return pages, fmt.Errorf("writing page %s: %w", page.RelPath, err)
		}
		pages++
	}
	if err = output.Commit(ctx); err != nil {
		return pages, fmt.Errorf("committing output: %w", err)
	}
	committed = true
	return pages, nil
}

func pageMetaMap(info *pageInfo) map[string]any {
	m := map[string]any{"path": info.path, "title": info.title, "depth": info.depth}
	if info.date != "" {
		m["date"] = info.date
	}
	if info.weight != nil {
		m["weight"] = *info.weight
	}
	if info.params != nil {
		m["params"] = info.params
	}
	return m
}

func orderedPageData(infos []*pageInfo) []map[string]any {
	groups := map[string][]*pageInfo{}
	for _, info := range infos {
		groups[groupOf(info.path)] = append(groups[groupOf(info.path)], info)
	}
	for _, group := range groups {
		sort.SliceStable(group, func(i, j int) bool { return group[i].before(group[j]) })
	}
	var out []map[string]any
	emitted := map[string]bool{}
	var emit func(string)
	emit = func(dir string) {
		if emitted[dir] {
			return
		}
		emitted[dir] = true
		for _, info := range groups[dir] {
			out = append(out, pageMetaMap(info))
			if info.path != "/" && strings.HasSuffix(info.path, "/") {
				emit(info.path)
			}
		}
	}
	emit("/")
	var rest []string
	for dir := range groups {
		if !emitted[dir] {
			rest = append(rest, dir)
		}
	}
	sort.Strings(rest)
	for _, dir := range rest {
		emit(dir)
	}
	return out
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
