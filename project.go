package antedom

// Project and Operation are the application layer above Site. Project describes
// antedom's conventional on-disk project, while an Operation is one invocation
// of new, build, or serve. Keeping this layer out of the CLI gives future
// extension hosts and embedding applications the same entry points.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/net/html"
)

// Project is an antedom project rooted at Root. Data may override the
// conventional data/*.json loader; nil uses that loader.
type Project struct {
	Root string
	Data func() (map[string]any, error)
}

// NewProject constructs a project rooted at root.
func NewProject(root string) *Project {
	return &Project{Root: root}
}

// Site returns the rendering Site described by the project.
func (p *Project) Site() *Site {
	data := p.Data
	if data == nil {
		data = projectData(filepath.Join(p.Root, "data"))
	}
	return &Site{
		Pages:  filepath.Join(p.Root, "pages"),
		Layout: filepath.Join(p.Root, "layout"),
		Data:   data,
	}
}

// OperationKind identifies one top-level project operation.
type OperationKind string

const (
	OperationNew   OperationKind = "new"
	OperationBuild OperationKind = "build"
	OperationServe OperationKind = "serve"
)

// Operation is one invocation against a project. Context cancellation is
// checked at operation boundaries; the page pipeline will propagate it more
// deeply when builds gain a build-plan layer.
type Operation struct {
	Kind    OperationKind
	Project *Project
	Context context.Context
}

// NewOperation creates an operation. A nil context is treated as Background.
func NewOperation(ctx context.Context, kind OperationKind, project *Project) *Operation {
	if ctx == nil {
		ctx = context.Background()
	}
	return &Operation{Kind: kind, Project: project, Context: ctx}
}

// Operation creates an operation against p.
func (p *Project) Operation(ctx context.Context, kind OperationKind) *Operation {
	return NewOperation(ctx, kind, p)
}

// Build renders the operation's project to out.
func (o *Operation) Build(out string) (int, error) {
	if err := o.ready(OperationBuild); err != nil {
		return 0, err
	}
	extension, err := loadProjectExtension(o.Project.Root)
	if err != nil {
		return 0, err
	}
	site := o.Project.Site()
	plan, err := site.PlanContext(o.operationContext())
	if err != nil {
		return 0, err
	}
	options := BuildOptions{}
	output := Output(NewHTMLOutput(out))
	if extension != nil {
		options.PageDocument = extension.pageDocument
		if extra := extension.buildOutputs(out); len(extra) != 0 {
			output = NewOutputGroup(append([]Output{output}, extra...)...)
		}
	}
	return site.BuildWithOptions(o.operationContext(), plan, output, options)
}

// buildArtifacts renders the project's registered extension outputs into
// dir without writing the HTML tree. The pipeline otherwise matches Build —
// outputs observe hook-transformed rendered pages — so the artifacts are
// byte-identical to a full build's. It reports false without building when
// the project registers no outputs. It is not gated on operation kind:
// serve rebuilds artifacts on demand.
func (o *Operation) buildArtifacts(dir string) (bool, error) {
	if o == nil || o.Project == nil {
		return false, fmt.Errorf("artifact build: no project")
	}
	ctx := o.operationContext()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	extension, err := loadProjectExtension(o.Project.Root)
	if err != nil {
		return false, err
	}
	if extension == nil {
		return false, nil
	}
	outputs := extension.buildOutputs(dir)
	if len(outputs) == 0 {
		return false, nil
	}
	site := o.Project.Site()
	plan, err := site.PlanContext(ctx)
	if err != nil {
		return false, err
	}
	_, err = site.BuildWithOptions(ctx, plan, NewOutputGroup(outputs...), BuildOptions{
		PageDocument: extension.pageDocument,
	})
	if err != nil {
		return false, err
	}
	return true, nil
}

// Handler returns the operation's project handler. It checks both the
// operation context and each request context before doing work.
func (o *Operation) Handler() http.Handler {
	if err := o.ready(OperationServe); err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		})
	}
	next := o.Project.Site().HandlerWithOptions(HandlerOptions{
		PageDocument: o.servePageDocument,
	})
	artifacts := &artifactCache{op: o}
	go artifacts.warm()
	ctx := o.operationContext()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-ctx.Done():
			http.Error(w, ctx.Err().Error(), http.StatusServiceUnavailable)
			return
		case <-r.Context().Done():
			http.Error(w, r.Context().Err().Error(), http.StatusRequestTimeout)
			return
		default:
			if rel, ok := artifacts.match(r); ok {
				artifacts.serve(w, r, rel)
				return
			}
			next.ServeHTTP(w, r)
		}
	})
}

// artifactCache serves registered extension outputs in serve mode. Pages
// render fresh per request, but an artifact such as a sitemap needs the whole
// site, so artifacts are rebuilt on demand into a temporary directory and
// reused until the site fingerprint changes: a served artifact is a snapshot
// of the site as of its last rebuild (data values like now are frozen there
// while live in pages). One rebuild runs at a time; concurrent artifact
// requests wait for it and reuse its result.
type artifactCache struct {
	op *Operation

	mu    sync.Mutex
	dir   string
	fp    uint64
	valid bool
}

// match reports the registered output file the request addresses, if any,
// consulting the extension afresh like the rest of the request pipeline.
// Extension load errors defer to the page handler, which surfaces them on
// rendered requests, so non-artifact traffic is unaffected by a broken
// extension.
func (c *artifactCache) match(r *http.Request) (string, bool) {
	extension, err := loadProjectExtension(c.op.Project.Root)
	if err != nil || extension == nil {
		return "", false
	}
	rel := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if slices.Contains(extension.outputFiles(), rel) {
		return rel, true
	}
	return "", false
}

func (c *artifactCache) serve(w http.ResponseWriter, r *http.Request, rel string) {
	file, err := c.refresh(rel)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	f, err := os.Open(file)
	if os.IsNotExist(err) {
		// The output was unregistered between match and rebuild.
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.ServeContent(w, r, path.Base(rel), info.ModTime(), f)
}

// warm builds the artifacts once at serve startup so the first artifact
// request does not pay for the first full render. Errors are dropped here:
// valid stays false, and the next artifact request rebuilds and reports them.
func (c *artifactCache) warm() {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.refreshLocked()
}

// refresh rebuilds the cached artifacts if the site changed since the last
// build and returns rel's on-disk path.
func (c *artifactCache) refresh(rel string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.refreshLocked(); err != nil {
		return "", err
	}
	return filepath.Join(c.dir, filepath.FromSlash(rel)), nil
}

// refreshLocked, called with mu held, rebuilds into the cache directory when
// the site changed since the last build. The fingerprint is taken before
// building so edits landing mid-build invalidate the result instead of going
// unseen. It covers the conventional project inputs; a custom Project.Data
// source is invisible to it.
func (c *artifactCache) refreshLocked() error {
	if c.dir == "" {
		dir, err := os.MkdirTemp("", "antedom-artifacts-")
		if err != nil {
			return err
		}
		c.dir = dir
	}
	root := c.op.Project.Root
	fp := fingerprint(
		filepath.Join(root, "pages"),
		filepath.Join(root, "layout"),
		filepath.Join(root, "data"),
		filepath.Join(root, "antedom.js"),
	)
	if !c.valid || fp != c.fp {
		if _, err := c.op.buildArtifacts(c.dir); err != nil {
			return err
		}
		c.fp, c.valid = fp, true
	}
	return nil
}

// servePageDocument loads the project extension for one request's render.
// Serve loads it per request for the same reason it re-plans the site per
// request: edits to antedom.js take effect immediately. It also confines each
// Sobek runtime to one request's goroutine — runtimes are not goroutine-safe,
// and concurrent requests must not share one. Hooks therefore cannot carry
// state between pages in serve the way they can within a single build.
func (o *Operation) servePageDocument() (PageDocumentHook, error) {
	extension, err := loadProjectExtension(o.Project.Root)
	if err != nil || extension == nil {
		return nil, err
	}
	return extension.pageDocument, nil
}

// NewPage scaffolds a page at rel, relative to the project's pages directory,
// wrapping layout. It returns the path written.
func (o *Operation) NewPage(layout, rel string) (string, error) {
	if err := o.ready(OperationNew); err != nil {
		return "", err
	}
	if filepath.Ext(rel) == "" {
		rel += ".md"
	}
	layoutPath := filepath.Join(o.Project.Root, "layout", layout)
	if _, err := os.Stat(layoutPath); err != nil {
		return "", fmt.Errorf("layout %s: %w", layout, err)
	}
	dest := filepath.Join(o.Project.Root, "pages", rel)
	if _, err := os.Stat(dest); err == nil {
		return "", fmt.Errorf("%s already exists", dest)
	}
	title, _ := json.Marshal(pageTitle(rel))
	date := time.Now().Format("2006-01-02")
	slots := openSlots(o.Project.Root, layoutPath)
	var src string
	if strings.HasSuffix(rel, ".md") {
		if !slices.Contains(slots, "") {
			return "", fmt.Errorf("layout %s has no open default slot (bare ante:slot) for the page body; scaffold an .html page for the explicit form", layout)
		}
		src = fmt.Sprintf(`<script ante:meta type="application/json">
{
  "title": %s,
  "date": %q,
  "layout": %q
}
</script>

`, title, date, layout)
	} else {
		var fills strings.Builder
		for _, slot := range slots {
			if slot == "" {
				fills.WriteString("\n<template ante:fill>\n\n</template>\n")
			} else {
				fmt.Fprintf(&fills, "\n<template ante:fill=%q>\n\n</template>\n", slot)
			}
		}
		src = fmt.Sprintf(`<template ante:layout=%q>

<script ante:meta type="application/json">
{
  "title": %s,
  "date": %q
}
</script>
%s
</template>
`, layout, title, date, fills.String())
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o777); err != nil {
		return "", err
	}
	return dest, os.WriteFile(dest, []byte(src), 0o666)
}

func (o *Operation) ready(want OperationKind) error {
	if o == nil || o.Project == nil {
		return fmt.Errorf("%s operation: no project", want)
	}
	if o.Kind != want {
		return fmt.Errorf("%s operation cannot run %s", o.Kind, want)
	}
	ctx := o.operationContext()
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func (o *Operation) operationContext() context.Context {
	if o.Context == nil {
		return context.Background()
	}
	return o.Context
}

// projectData assembles each data/*.json file under data.<name>, plus the
// render timestamp as now.
func projectData(dir string) func() (map[string]any, error) {
	return func() (map[string]any, error) {
		data := map[string]any{}
		files, err := filepath.Glob(filepath.Join(dir, "*.json"))
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			src, err := os.ReadFile(f)
			if err != nil {
				return nil, err
			}
			var v any
			if err := json.Unmarshal(src, &v); err != nil {
				return nil, fmt.Errorf("%s: %w", f, err)
			}
			data[strings.TrimSuffix(filepath.Base(f), ".json")] = v
		}
		return map[string]any{
			"now":  time.Now().Format(time.RFC3339),
			"data": data,
		}, nil
	}
}

func pageTitle(rel string) string {
	name := strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
	if name == "index" && filepath.Dir(rel) != "." {
		name = filepath.Base(filepath.Dir(rel))
	}
	name = strings.NewReplacer("-", " ", "_", " ").Replace(name)
	r := []rune(name)
	if len(r) > 0 {
		r[0] = unicode.ToUpper(r[0])
	}
	return string(r)
}

// openSlots returns the open slots in a composed layout in document order.
// The empty string identifies the default slot.
func openSlots(root, layoutPath string) []string {
	load := func(name string) (*html.Node, error) {
		src, err := os.ReadFile(filepath.Join(root, "layout", name))
		if err != nil {
			return nil, err
		}
		return html.Parse(bytes.NewReader(src))
	}
	doc, err := load(filepath.Base(layoutPath))
	if err == nil {
		doc, err = Compose(doc, load)
	}
	if err != nil {
		return []string{""}
	}
	var slots []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if slot, ok := attrVal(n, Prefix+"slot"); ok {
			fill := n.FirstChild
			filled := false
			if fill != nil {
				_, filled = attrVal(fill, Prefix+"fill")
			}
			if !filled {
				slots = append(slots, slot)
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return slots
}
