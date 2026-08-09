package antedom

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/grafana/sobek"
	"golang.org/x/net/html"
)

const extensionAPIVersion int64 = 1

// projectExtension is the build-scoped JavaScript extension runtime. Sobek
// runtimes are not goroutine-safe; builds currently invoke it serially.
type projectExtension struct {
	path          string
	vm            *sobek.Runtime
	versionCalled bool
	hooks         []sobek.Callable
	deepFreeze    sobek.Callable
}

// loadProjectExtension loads <root>/antedom.js. A missing file means no
// extension; an existing file must call antedom.apiVersion(1) before
// registering anything.
func loadProjectExtension(root string) (*projectExtension, error) {
	path := filepath.Join(root, "antedom.js")
	src, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("loading extension %s: %w", path, err)
	}

	vm := sobek.New()
	ext := &projectExtension{path: path, vm: vm}
	api := vm.NewObject()
	if err := api.DefineDataProperty("apiVersion", vm.ToValue(ext.apiVersion), sobek.FLAG_FALSE, sobek.FLAG_FALSE, sobek.FLAG_TRUE); err != nil {
		return nil, err
	}
	if err := api.DefineDataProperty("on", vm.ToValue(ext.on), sobek.FLAG_FALSE, sobek.FLAG_FALSE, sobek.FLAG_TRUE); err != nil {
		return nil, err
	}
	if err := vm.Set("antedom", api); err != nil {
		return nil, err
	}

	v, err := vm.RunString(`(function deepFreeze(value) {
  if (value && typeof value === "object" && !Object.isFrozen(value)) {
    Object.freeze(value);
    for (const key of Object.keys(value)) deepFreeze(value[key]);
  }
  return value;
})`)
	if err != nil {
		return nil, err
	}
	ext.deepFreeze, _ = sobek.AssertFunction(v)

	if _, err := vm.RunScript(path, string(src)); err != nil {
		return nil, fmt.Errorf("loading extension %s: %w", path, err)
	}
	if !ext.versionCalled {
		return nil, fmt.Errorf("loading extension %s: antedom.apiVersion(1) must be called before registering hooks", path)
	}
	return ext, nil
}

func (e *projectExtension) apiVersion(call sobek.FunctionCall) sobek.Value {
	if e.versionCalled {
		panic(e.vm.NewTypeError("antedom.apiVersion may only be called once"))
	}
	version := call.Argument(0)
	if !version.StrictEquals(e.vm.ToValue(extensionAPIVersion)) {
		panic(e.vm.NewTypeError("unsupported antedom API version %s; want %d", version.String(), extensionAPIVersion))
	}
	e.versionCalled = true
	return sobek.Undefined()
}

func (e *projectExtension) on(call sobek.FunctionCall) sobek.Value {
	if !e.versionCalled {
		panic(e.vm.NewTypeError("antedom.apiVersion(1) must be called before antedom.on"))
	}
	name := call.Argument(0).String()
	if name != "page:document" {
		panic(e.vm.NewTypeError("unsupported hook %q; want %q", name, "page:document"))
	}
	fn, ok := sobek.AssertFunction(call.Argument(1))
	if !ok {
		panic(e.vm.NewTypeError("handler for %q must be a function", name))
	}
	e.hooks = append(e.hooks, fn)
	return sobek.Undefined()
}

func (e *projectExtension) pageDocument(ctx context.Context, page *Page, _ *html.Node) error {
	if e == nil || len(e.hooks) == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	document := e.vm.NewObject() // Opaque until Go-backed DOM operations arrive.
	pageValue := map[string]any{
		"sourcePath": page.SourcePath,
		"relPath":    page.RelPath,
		"urlPath":    page.URLPath,
		"outputPath": page.OutputPath,
		"format":     string(page.Format),
		"meta":       page.Meta,
		"document":   document,
	}
	v := e.plainValue(pageValue)
	if _, err := e.deepFreeze(sobek.Undefined(), v); err != nil {
		return fmt.Errorf("extension %s: preparing page %s: %w", e.path, page.RelPath, err)
	}
	for _, hook := range e.hooks {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := hook(sobek.Undefined(), v); err != nil {
			return fmt.Errorf("extension %s: hook %q: page %s: %w", e.path, "page:document", page.RelPath, err)
		}
	}
	return nil
}

// plainValue copies JSON-shaped Go data into ordinary JavaScript objects and
// arrays. Sobek exposes Go maps as host objects, whose fields cannot be frozen;
// extension inputs must instead be genuine, deeply freezable JS values.
func (e *projectExtension) plainValue(value any) sobek.Value {
	switch value := value.(type) {
	case nil:
		return sobek.Null()
	case map[string]any:
		obj := e.vm.NewObject()
		for key, item := range value {
			if err := obj.Set(key, e.plainValue(item)); err != nil {
				panic(err)
			}
		}
		return obj
	case []any:
		items := make([]any, len(value))
		for i, item := range value {
			items[i] = e.plainValue(item)
		}
		return e.vm.NewArray(items...)
	default:
		return e.vm.ToValue(value)
	}
}
