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
	outputs       []extensionOutput
}

type extensionOutput struct {
	name      string
	file      string
	validate  string
	callbacks *extensionOutputCallbacks
}

type extensionOutputCallbacks struct {
	begin sobek.Callable
	page  sobek.Callable
	asset sobek.Callable
	end   sobek.Callable
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
	if err := api.DefineDataProperty("output", vm.ToValue(ext.output), sobek.FLAG_FALSE, sobek.FLAG_FALSE, sobek.FLAG_TRUE); err != nil {
		return nil, err
	}
	xmlTag := vm.ToValue(ext.xmlTemplate).ToObject(vm)
	if err := xmlTag.DefineDataProperty("join", vm.ToValue(ext.xmlJoin), sobek.FLAG_FALSE, sobek.FLAG_FALSE, sobek.FLAG_TRUE); err != nil {
		return nil, err
	}
	if err := api.DefineDataProperty("xml", xmlTag, sobek.FLAG_FALSE, sobek.FLAG_FALSE, sobek.FLAG_TRUE); err != nil {
		return nil, err
	}
	urlAPI := vm.NewObject()
	if err := urlAPI.DefineDataProperty("resolve", vm.ToValue(ext.resolveURL), sobek.FLAG_FALSE, sobek.FLAG_FALSE, sobek.FLAG_TRUE); err != nil {
		return nil, err
	}
	if err := api.DefineDataProperty("url", urlAPI, sobek.FLAG_FALSE, sobek.FLAG_FALSE, sobek.FLAG_TRUE); err != nil {
		return nil, err
	}
	htmlAPI := vm.NewObject()
	if err := htmlAPI.DefineDataProperty("resolveURLs", vm.ToValue(ext.resolveHTMLURLs), sobek.FLAG_FALSE, sobek.FLAG_FALSE, sobek.FLAG_TRUE); err != nil {
		return nil, err
	}
	if err := api.DefineDataProperty("html", htmlAPI, sobek.FLAG_FALSE, sobek.FLAG_FALSE, sobek.FLAG_TRUE); err != nil {
		return nil, err
	}
	goAPI := vm.NewObject()
	if err := goAPI.DefineDataProperty("jsonManifest", vm.ToValue(ext.jsonManifest), sobek.FLAG_FALSE, sobek.FLAG_FALSE, sobek.FLAG_TRUE); err != nil {
		return nil, err
	}
	if err := api.DefineDataProperty("go", goAPI, sobek.FLAG_FALSE, sobek.FLAG_FALSE, sobek.FLAG_TRUE); err != nil {
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

func (e *projectExtension) jsonManifest(call sobek.FunctionCall) sobek.Value {
	if !e.versionCalled {
		panic(e.vm.NewTypeError("antedom.apiVersion(1) must be called before antedom.go.jsonManifest"))
	}
	arg := call.Argument(0)
	obj, ok := arg.(*sobek.Object)
	if !ok || sobek.IsNull(arg) {
		panic(e.vm.NewTypeError("antedom.go.jsonManifest options must be an object"))
	}
	for _, key := range obj.Keys() {
		if key != "file" {
			panic(e.vm.NewTypeError("unknown antedom.go.jsonManifest option %q", key))
		}
	}
	value := obj.Get("file")
	if value == nil || sobek.IsUndefined(value) {
		panic(e.vm.NewTypeError("antedom.go.jsonManifest file is required"))
	}
	fileValue, ok := value.Export().(string)
	if !ok || fileValue == "" {
		panic(e.vm.NewTypeError("antedom.go.jsonManifest file is required"))
	}
	file, err := cleanArtifactPath(fileValue)
	if err != nil {
		panic(e.vm.NewTypeError("antedom.go.jsonManifest file must stay within the output directory"))
	}
	return e.vm.NewDynamicObject(&outputToken{file: file})
}

func (e *projectExtension) output(call sobek.FunctionCall) sobek.Value {
	if !e.versionCalled {
		panic(e.vm.NewTypeError("antedom.apiVersion(1) must be called before antedom.output"))
	}
	name, ok := call.Argument(0).Export().(string)
	if !ok || name == "" {
		panic(e.vm.NewTypeError("antedom.output name is required"))
	}
	for _, output := range e.outputs {
		if output.name == name {
			panic(e.vm.NewTypeError("antedom output %q is already registered", name))
		}
	}
	arg := call.Argument(1)
	token, tokenOK := arg.Export().(*outputToken)
	var registered extensionOutput
	if tokenOK {
		registered = extensionOutput{name: name, file: token.file}
	} else {
		obj, ok := arg.(*sobek.Object)
		if !ok || sobek.IsNull(arg) {
			panic(e.vm.NewTypeError("antedom.output %q requires an output configuration or antedom.go output", name))
		}
		for _, key := range obj.Keys() {
			switch key {
			case "file", "validate", "begin", "page", "asset", "end":
			default:
				panic(e.vm.NewTypeError("unknown antedom.output option %q", key))
			}
		}
		fileValue := obj.Get("file")
		if fileValue == nil || sobek.IsUndefined(fileValue) {
			panic(e.vm.NewTypeError("antedom.output %q file is required", name))
		}
		file, ok := fileValue.Export().(string)
		if !ok || file == "" {
			panic(e.vm.NewTypeError("antedom.output %q file is required", name))
		}
		var err error
		if file, err = cleanArtifactPath(file); err != nil {
			panic(e.vm.NewTypeError("antedom.output %q file must stay within the output directory", name))
		}
		validate := ""
		if value := obj.Get("validate"); value != nil && !sobek.IsUndefined(value) {
			var ok bool
			validate, ok = value.Export().(string)
			if !ok || validate != "xml" {
				panic(e.vm.NewTypeError("antedom.output %q validate must be %q", name, "xml"))
			}
		}
		callbacks := &extensionOutputCallbacks{}
		for key, target := range map[string]*sobek.Callable{
			"begin": &callbacks.begin, "page": &callbacks.page,
			"asset": &callbacks.asset, "end": &callbacks.end,
		} {
			value := obj.Get(key)
			if value == nil || sobek.IsUndefined(value) {
				continue
			}
			fn, ok := sobek.AssertFunction(value)
			if !ok {
				panic(e.vm.NewTypeError("antedom.output %q %s must be a function", name, key))
			}
			*target = fn
		}
		registered = extensionOutput{name: name, file: file, validate: validate, callbacks: callbacks}
	}
	for _, output := range e.outputs {
		if output.file == registered.file {
			panic(e.vm.NewTypeError("antedom output file %q is already registered", registered.file))
		}
	}
	e.outputs = append(e.outputs, registered)
	return sobek.Undefined()
}

// outputFiles returns each registered output's slash-separated file path.
func (e *projectExtension) outputFiles() []string {
	files := make([]string, len(e.outputs))
	for i, output := range e.outputs {
		files[i] = output.file
	}
	return files
}

func (e *projectExtension) buildOutputs(root string) []Output {
	outputs := make([]Output, 0, len(e.outputs))
	claimed := make(map[string]string)
	for _, output := range e.outputs {
		claimed[output.file] = fmt.Sprintf("output %q", output.name)
	}
	for _, output := range e.outputs {
		if output.callbacks == nil {
			manifest := NewJSONManifestOutput(filepath.Join(root, filepath.FromSlash(output.file)))
			manifest.OutputPath = output.file
			outputs = append(outputs, manifest)
			continue
		}
		outputs = append(outputs, &javascriptArtifactOutput{
			extension: e, config: output, root: root, claimed: claimed,
		})
	}
	return outputs
}

// javascriptArtifactOutput adapts streaming JavaScript lifecycle callbacks to
// the Go Output contract. It never exports a DOM into Sobek.
type javascriptArtifactOutput struct {
	extension *projectExtension
	config    extensionOutput
	root      string
	claimed   map[string]string
	writer    *ArtifactWriter
	value     sobek.Value
	planValue sobek.Value
}

func (o *javascriptArtifactOutput) Begin(ctx context.Context, plan *BuildPlan) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	owner := fmt.Sprintf("output %q", o.config.name)
	for _, page := range plan.Pages {
		if page.OutputPath == o.config.file {
			return fmt.Errorf("artifact %q conflicts with page %s", o.config.file, page.RelPath)
		}
		if _, ok := o.claimed[page.OutputPath]; !ok {
			o.claimed[page.OutputPath] = "page " + page.RelPath
		}
	}
	for _, asset := range plan.Assets {
		if asset.OutputPath == o.config.file {
			return fmt.Errorf("artifact %q conflicts with asset %s", o.config.file, asset.RelPath)
		}
		if _, ok := o.claimed[asset.OutputPath]; !ok {
			o.claimed[asset.OutputPath] = "asset " + asset.RelPath
		}
	}
	o.writer = newArtifactWriter(o.root, owner, o.claimed)
	if err := o.writer.Open(o.config.file); err != nil {
		_ = o.writer.Abort(context.WithoutCancel(ctx))
		o.writer = nil
		return err
	}
	o.value = o.writerValue(o.config.file)
	o.planValue = o.extension.readOnlyValue(map[string]any{
		"pages": len(plan.Pages), "assets": len(plan.Assets),
	})
	if err := o.invoke("begin", o.config.callbacks.begin, o.planValue, o.value); err != nil {
		_ = o.writer.Abort(context.WithoutCancel(ctx))
		o.writer, o.value, o.planValue = nil, nil, nil
		return err
	}
	return nil
}

func (o *javascriptArtifactOutput) WritePage(ctx context.Context, page *RenderedPage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	handle := &outputPageValue{extension: o.extension, page: page}
	value := o.extension.vm.NewDynamicObject(handle)
	err := o.invoke("page", o.config.callbacks.page, value, o.value)
	handle.page = nil
	return err
}

func (o *javascriptArtifactOutput) WriteAsset(ctx context.Context, asset *Asset) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	value := o.extension.readOnlyValue(map[string]any{
		"sourcePath": asset.SourcePath, "relPath": asset.RelPath, "outputPath": asset.OutputPath,
	})
	return o.invoke("asset", o.config.callbacks.asset, value, o.value)
}

func (o *javascriptArtifactOutput) Commit(ctx context.Context) error {
	if err := o.invoke("end", o.config.callbacks.end, o.planValue, o.value); err != nil {
		return err
	}
	if o.config.validate == "xml" {
		if err := o.writer.ValidateXML(o.config.file); err != nil {
			return fmt.Errorf("extension %s: output %q validate %s: %w", o.extension.path, o.config.name, o.config.file, err)
		}
	}
	if err := o.writer.Commit(ctx); err != nil {
		return err
	}
	o.writer, o.value, o.planValue = nil, nil, nil
	return nil
}

func (o *javascriptArtifactOutput) Abort(ctx context.Context) error {
	if o.writer == nil {
		return nil
	}
	err := o.writer.Abort(ctx)
	o.writer, o.value, o.planValue = nil, nil, nil
	return err
}

func (o *javascriptArtifactOutput) invoke(stage string, fn sobek.Callable, args ...sobek.Value) error {
	if fn == nil {
		return nil
	}
	if _, err := fn(sobek.Undefined(), args...); err != nil {
		return fmt.Errorf("extension %s: output %q %s: %w", o.extension.path, o.config.name, stage, err)
	}
	return nil
}

// outputPageValue exposes one rendered page to output callbacks. The meta,
// html, and text conversions are memoized so a callback reading a field
// repeatedly converts the body or walks the DOM at most once per page.
type outputPageValue struct {
	extension   *projectExtension
	page        *RenderedPage
	meta        sobek.Value
	html        sobek.Value
	text        sobek.Value
	contentHTML sobek.Value
	contentText sobek.Value
}

func (p *outputPageValue) Get(key string) sobek.Value {
	if p.page == nil {
		panic(p.extension.vm.NewTypeError("output page handle has expired"))
	}
	switch key {
	case "sourcePath":
		return p.extension.vm.ToValue(p.page.Page.SourcePath)
	case "relPath":
		return p.extension.vm.ToValue(p.page.Page.RelPath)
	case "urlPath":
		return p.extension.vm.ToValue(p.page.Page.URLPath)
	case "outputPath":
		return p.extension.vm.ToValue(p.page.Page.OutputPath)
	case "format":
		return p.extension.vm.ToValue(string(p.page.Page.Format))
	case "size":
		return p.extension.vm.ToValue(len(p.page.HTML))
	case "meta":
		if p.meta == nil {
			p.meta = p.extension.readOnlyValue(p.page.Page.Meta)
		}
		return p.meta
	case "html":
		if p.html == nil {
			p.html = p.extension.vm.ToValue(string(p.page.HTML))
		}
		return p.html
	case "text":
		if p.text == nil {
			p.text = p.extension.vm.ToValue(text(p.page.Document))
		}
		return p.text
	case "contentHTML":
		if p.contentHTML == nil {
			rendered, err := renderHTMLNodes(p.page.Content)
			if err != nil {
				panic(p.extension.vm.NewGoError(err))
			}
			p.contentHTML = p.extension.vm.ToValue(string(rendered))
		}
		return p.contentHTML
	case "contentText":
		if p.contentText == nil {
			p.contentText = p.extension.vm.ToValue(textNodes(p.page.Content))
		}
		return p.contentText
	default:
		return nil
	}
}
func (*outputPageValue) Set(string, sobek.Value) bool { return false }
func (p *outputPageValue) Has(key string) bool {
	for _, candidate := range p.Keys() {
		if key == candidate {
			return true
		}
	}
	return false
}
func (*outputPageValue) Delete(string) bool { return false }
func (*outputPageValue) Keys() []string {
	return []string{"sourcePath", "relPath", "urlPath", "outputPath", "format", "size", "meta", "html", "text", "contentHTML", "contentText"}
}

func (o *javascriptArtifactOutput) writerValue(file string) sobek.Value {
	write := func(call sobek.FunctionCall) sobek.Value {
		if o.writer == nil {
			panic(o.extension.vm.NewTypeError("artifact writer has expired"))
		}
		var data []byte
		switch exported := call.Argument(0).Export().(type) {
		case string:
			data = []byte(exported)
		case *xmlFragment:
			data = exported.data
		case []byte:
			data = exported
		case sobek.ArrayBuffer:
			data = exported.Bytes()
		default:
			panic(o.extension.vm.NewTypeError("output.write requires a string, Uint8Array, or antedom.xml fragment"))
		}
		if err := o.writer.Write(file, data); err != nil {
			panic(o.extension.vm.NewGoError(err))
		}
		return sobek.Undefined()
	}
	writeJSON := func(call sobek.FunctionCall) sobek.Value {
		jsonObject := o.extension.vm.Get("JSON").ToObject(o.extension.vm)
		stringify, _ := sobek.AssertFunction(jsonObject.Get("stringify"))
		encoded, err := stringify(jsonObject, call.Argument(0))
		if err != nil {
			panic(err)
		}
		if sobek.IsUndefined(encoded) {
			panic(o.extension.vm.NewTypeError("output.writeJSON value is not JSON serializable"))
		}
		if err := o.writer.Write(file, []byte(encoded.String())); err != nil {
			panic(o.extension.vm.NewGoError(err))
		}
		return sobek.Undefined()
	}
	open := func(call sobek.FunctionCall) sobek.Value {
		name, ok := call.Argument(0).Export().(string)
		if !ok || name == "" {
			panic(o.extension.vm.NewTypeError("output.open path is required"))
		}
		clean, err := cleanArtifactPath(name)
		if err != nil {
			panic(o.extension.vm.NewTypeError("output.open path must stay within the output directory"))
		}
		if err := o.writer.Open(clean); err != nil {
			panic(o.extension.vm.NewGoError(err))
		}
		return o.writerValue(clean)
	}
	values := map[string]any{"write": write, "writeJSON": writeJSON}
	if file == o.config.file {
		values["open"] = open
	}
	return o.extension.readOnlyValue(values)
}

type outputToken struct{ file string }

func (*outputToken) Get(string) sobek.Value       { return nil }
func (*outputToken) Set(string, sobek.Value) bool { return false }
func (*outputToken) Has(string) bool              { return false }
func (*outputToken) Delete(string) bool           { return false }
func (*outputToken) Keys() []string               { return nil }

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
	// Sorted keys make JavaScript iteration and JSON.stringify order
	// deterministic and match encoding/json's sorted map keys, so a
	// JavaScript-generated manifest can be byte-identical to the Go one.
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
