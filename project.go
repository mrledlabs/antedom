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
	"path/filepath"
	"slices"
	"strings"
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
	return o.Project.Site().Build(out)
}

// Handler returns the operation's project handler. It checks both the
// operation context and each request context before doing work.
func (o *Operation) Handler() http.Handler {
	if err := o.ready(OperationServe); err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		})
	}
	next := o.Project.Site().Handler()
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
			next.ServeHTTP(w, r)
		}
	})
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
