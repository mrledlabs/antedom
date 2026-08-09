package antedom

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

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

func (e *projectExtension) pageDocument(ctx context.Context, page *Page, doc *html.Node) error {
	if e == nil || len(e.hooks) == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	document := e.documentValue(doc)
	defer func() { document.doc = nil }()
	pageValue := map[string]any{
		"sourcePath": page.SourcePath,
		"relPath":    page.RelPath,
		"urlPath":    page.URLPath,
		"outputPath": page.OutputPath,
		"format":     string(page.Format),
		"meta":       page.Meta,
		"document":   document.value,
	}
	v := e.readOnlyValue(pageValue)
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

func (e *projectExtension) documentValue(doc *html.Node) *documentHandle {
	handle := &documentHandle{doc: doc}
	highlight := e.vm.ToValue(func(call sobek.FunctionCall) sobek.Value {
		if handle.doc == nil {
			panic(e.vm.NewTypeError("page document handle has expired"))
		}
		options := defaultHighlightOptions()
		arg := call.Argument(0)
		if !sobek.IsUndefined(arg) {
			obj, ok := arg.(*sobek.Object)
			if !ok || sobek.IsNull(arg) {
				panic(e.vm.NewTypeError("document.highlight options must be an object"))
			}
			for _, key := range obj.Keys() {
				switch key {
				case "style", "unknownLanguage":
				default:
					panic(e.vm.NewTypeError("unknown document.highlight option %q", key))
				}
			}
			if value := obj.Get("style"); value != nil && !sobek.IsUndefined(value) {
				options.Style = value.String()
			}
			if value := obj.Get("unknownLanguage"); value != nil && !sobek.IsUndefined(value) {
				options.UnknownLanguage = value.String()
			}
		}
		if options.UnknownLanguage != "ignore" && options.UnknownLanguage != "error" {
			panic(e.vm.NewTypeError("document.highlight unknownLanguage must be %q or %q", "ignore", "error"))
		}
		count, err := highlightDocument(handle.doc, options)
		if err != nil {
			panic(e.vm.NewGoError(err))
		}
		return e.vm.ToValue(count)
	})
	handle.value = e.readOnlyObject(map[string]any{"highlight": highlight})
	return handle
}

type documentHandle struct {
	doc   *html.Node
	value sobek.Value
}

// readOnlyValue exposes JSON-shaped Go data through small host-backed views.
// This avoids copying and recursively freezing a fresh JS object graph for
// every page while still rejecting writes and deletes at every level.
func (e *projectExtension) readOnlyValue(value any) sobek.Value {
	switch value := value.(type) {
	case nil:
		return sobek.Null()
	case map[string]any:
		return e.readOnlyObject(value)
	case []any:
		items := make([]sobek.Value, len(value))
		for i, item := range value {
			items[i] = e.readOnlyValue(item)
		}
		return e.vm.NewDynamicArray(&readOnlyArray{items: items})
	case sobek.Value:
		return value
	default:
		return e.vm.ToValue(value)
	}
}

func (e *projectExtension) readOnlyObject(value map[string]any) sobek.Value {
	values := make(map[string]sobek.Value, len(value))
	keys := make([]string, 0, len(value))
	for key, item := range value {
		keys = append(keys, key)
		values[key] = e.readOnlyValue(item)
	}
	sort.Strings(keys)
	return e.vm.NewDynamicObject(&readOnlyObject{values: values, keys: keys})
}

type readOnlyObject struct {
	values map[string]sobek.Value
	keys   []string
}

func (o *readOnlyObject) Get(key string) sobek.Value   { return o.values[key] }
func (o *readOnlyObject) Set(string, sobek.Value) bool { return false }
func (o *readOnlyObject) Has(key string) bool          { _, ok := o.values[key]; return ok }
func (o *readOnlyObject) Delete(string) bool           { return false }
func (o *readOnlyObject) Keys() []string               { return o.keys }

type readOnlyArray struct {
	items []sobek.Value
}

func (a *readOnlyArray) Len() int { return len(a.items) }
func (a *readOnlyArray) Get(i int) sobek.Value {
	if i < 0 || i >= len(a.items) {
		return nil
	}
	return a.items[i]
}
func (a *readOnlyArray) Set(int, sobek.Value) bool { return false }
func (a *readOnlyArray) SetLen(int) bool           { return false }
