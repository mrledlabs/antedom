// Package antedom renders antedom templates:
// valid HTML where templating logic lives in ante:-prefixed attributes
// and the expression language is JavaScript (sobek).
// The logic runs at build time, before any DOM exists — hence the name.
//
// Directive attributes, processed in this order:
//
//	ante:keep               on a <template>: process it but keep the element
//	ante:meta               page metadata (see Site.pageList): the element
//	                     is dropped from output, subtree unevaluated
//	ante:scope              on a <script>: run the body at build time; its
//	                     top-level declarations become scope variables for
//	                     the following siblings and their subtrees, and the
//	                     element is dropped (see scope.go)
//	ante:layout / ante:slot / ante:fill   composition markers, consumed
//	                     by Compose before the walk (see docs/templating.md)
//	ante:if="expr"          remove the element unless expr is truthy
//	ante:for="x[, i] of expr"  repeat the element per item of the array expr
//	ante:text="expr"        replace children with expr as escaped text
//	ante:html="expr"        replace children with expr parsed as HTML
//	ante:name="expr"        set attribute name to expr; false/null/undefined
//	                     drops the attribute, true makes it boolean
//
// A <template> bearing any ante: attribute is antedom's: processed,
// then unwrapped — grouping siblings under one ante:if/ante:for, or
// marking a slot or fill, without emitting a wrapper tag. A plain
// <template> is the client's: shipped verbatim, contents untouched.
// ante:keep processes a template's directives and contents but keeps
// the element, for client-bound templates that need build-time logic.
//
// An element named shortcode-<name> is an inert web component: after
// its own directives run it is replaced by the template
// Engine.Shortcodes loads for name — for a Site,
// layout/shortcode/<name>.html — evaluated in a child scope where
// each attribute is a variable and children is the element's rendered
// inner HTML (see shortcode.go).
//
// Because output is serialized from the parsed tree, it is well-formed
// by construction and all interpolation is contextually escaped.
package antedom

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"

	"github.com/grafana/sobek"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Prefix marks directive and bound attributes.
const Prefix = "ante:"

// Engine holds one sobek runtime and a cache of compiled expressions.
// A sobek runtime is not goroutine-safe; Render serializes on a mutex,
// so one Engine renders one page at a time (use several for parallelism).
type Engine struct {
	mu      sync.Mutex
	vm      *sobek.Runtime
	exprs   map[string]sobek.Callable
	mkScope sobek.Callable // Object.create, for prototype-chained scopes
	depth   int            // current shortcode expansion depth

	// Shortcodes loads the template source behind a shortcode-<name>
	// element (see shortcode.go); nil makes any such element an error.
	Shortcodes func(name string) ([]byte, error)
}

func New() (*Engine, error) {
	vm := sobek.New()
	create, err := vm.RunString("Object.create")
	if err != nil {
		return nil, err
	}
	mkScope, ok := sobek.AssertFunction(create)
	if !ok {
		return nil, fmt.Errorf("Object.create is not callable")
	}
	return &Engine{vm: vm, exprs: map[string]sobek.Callable{}, mkScope: mkScope}, nil
}

// Render parses src as a full HTML document, evaluates its directive
// attributes against data (the root JS scope), and writes the result.
func (e *Engine) Render(w io.Writer, src []byte, data map[string]any) error {
	doc, err := html.Parse(bytes.NewReader(src))
	if err != nil {
		return err
	}
	return e.RenderDoc(w, doc, data)
}

// RenderDoc is Render for an already-parsed (e.g. composed) document.
// It mutates doc.
func (e *Engine) RenderDoc(w io.Writer, doc *html.Node, data map[string]any) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if data == nil {
		data = map[string]any{}
	}
	scope := e.vm.ToValue(data).ToObject(e.vm)
	if err := e.walkChildren(doc, scope); err != nil {
		return err
	}
	return html.Render(w, doc)
}

// expr compiles a directive expression to a callable of one argument,
// the scope object, whose properties resolve as free variables.
func (e *Engine) expr(src string) (sobek.Callable, error) {
	if fn, ok := e.exprs[src]; ok {
		return fn, nil
	}
	// Non-strict `with` puts the scope object on the JS scope chain;
	// the newline guards against a trailing line comment in src.
	v, err := e.vm.RunString("(function($scope){with($scope){return(" + src + "\n)}})")
	if err != nil {
		return nil, fmt.Errorf("compiling %q: %w", src, err)
	}
	fn, _ := sobek.AssertFunction(v)
	e.exprs[src] = fn
	return fn, nil
}

func (e *Engine) eval(src string, scope *sobek.Object) (sobek.Value, error) {
	fn, err := e.expr(src)
	if err != nil {
		return nil, err
	}
	v, err := fn(sobek.Undefined(), scope)
	if err != nil {
		return nil, fmt.Errorf("evaluating %q: %w", src, err)
	}
	return v, nil
}

func (e *Engine) childScope(parent *sobek.Object) (*sobek.Object, error) {
	v, err := e.mkScope(sobek.Undefined(), parent)
	if err != nil {
		return nil, err
	}
	return v.ToObject(e.vm), nil
}

// walkChildren processes the element children of n. Children are
// snapshotted first because directives remove and replace nodes.
func (e *Engine) walkChildren(n *html.Node, scope *sobek.Object) error {
	var kids []*html.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			kids = append(kids, c)
		}
	}
	return e.walkSiblings(kids, scope)
}

// walkSiblings processes elements in document order. A <script
// ante:scope> advances the scope for the elements after it (see
// scope.go), which is why the walk threads scope through the list.
func (e *Engine) walkSiblings(nodes []*html.Node, scope *sobek.Object) error {
	for _, c := range nodes {
		if c.Type != html.ElementNode {
			continue
		}
		if _, ok := takeAttr(c, Prefix+"scope"); ok {
			next, err := e.runScope(c, scope)
			if err != nil {
				return err
			}
			scope = next
			continue
		}
		if err := e.element(c, scope); err != nil {
			return err
		}
	}
	return nil
}

// element applies the directives of one element, recursing into its
// subtree. It may remove n or replace it with copies of itself.
// A plain <template> — no ante: attributes — is client-bound:
// left verbatim, contents untouched. One with directives is consumed
// (unwrapped) after processing, unless ante:keep retains it.
func (e *Engine) element(n *html.Node, scope *sobek.Object) error {
	if n.DataAtom == atom.Template && !hasDirectives(n) {
		return nil
	}
	_, keep := takeAttr(n, Prefix+"keep")
	return e.apply(n, scope, n.DataAtom == atom.Template && !keep)
}

// apply runs the directive pipeline on n; consume unwraps n at the
// end. It is threaded through ante:for so loop copies, whose
// directive attributes are already stripped, still unwrap.
func (e *Engine) apply(n *html.Node, scope *sobek.Object, consume bool) error {
	// Page metadata is input, not output: already read by pageList,
	// dropped here so it never ships.
	if _, ok := attrVal(n, Prefix+"meta"); ok {
		n.Parent.RemoveChild(n)
		return nil
	}
	// Composition markers already served by Compose (or orphaned in a
	// standalone render); discard so they neither emit nor evaluate.
	takeAttr(n, Prefix+"layout")
	takeAttr(n, Prefix+"slot")
	takeAttr(n, Prefix+"fill")

	if src, ok := takeAttr(n, Prefix+"if"); ok {
		v, err := e.eval(src, scope)
		if err != nil {
			return err
		}
		if !v.ToBoolean() {
			n.Parent.RemoveChild(n)
			return nil
		}
	}
	if src, ok := takeAttr(n, Prefix+"for"); ok {
		return e.loop(n, src, scope, consume)
	}

	if src, ok := takeAttr(n, Prefix+"text"); ok {
		v, err := e.eval(src, scope)
		if err != nil {
			return err
		}
		setChildren(n, &html.Node{Type: html.TextNode, Data: valueString(v)})
	} else if src, ok := takeAttr(n, Prefix+"html"); ok {
		v, err := e.eval(src, scope)
		if err != nil {
			return err
		}
		frag, err := html.ParseFragment(strings.NewReader(valueString(v)), n)
		if err != nil {
			return fmt.Errorf("ante:html %q: %w", src, err)
		}
		setChildren(n, frag...)
	} else if err := e.walkChildren(n, scope); err != nil {
		return err
	}

	// Remaining ante:name attributes bind the attribute name.
	attrs := n.Attr[:0]
	for _, a := range n.Attr {
		if !strings.HasPrefix(a.Key, Prefix) {
			attrs = append(attrs, a)
			continue
		}
		v, err := e.eval(a.Val, scope)
		if err != nil {
			return err
		}
		switch {
		case v == sobek.Undefined() || v == sobek.Null() || v.Export() == false:
			// drop the attribute
		case v.Export() == true:
			attrs = append(attrs, html.Attribute{Key: a.Key[len(Prefix):]})
		default:
			attrs = append(attrs, html.Attribute{Key: a.Key[len(Prefix):], Val: v.String()})
		}
	}
	n.Attr = attrs

	if strings.HasPrefix(n.Data, ShortcodePrefix) {
		return e.expandShortcode(n, scope)
	}
	if consume {
		unwrap(n)
	}
	return nil
}

// loop expands `ante:for="item[, index] of expr"`: one processed copy of n
// per array element, each under a child scope binding the loop names.
func (e *Engine) loop(n *html.Node, src string, scope *sobek.Object, consume bool) error {
	names, arrExpr, ok := strings.Cut(src, " of ")
	if !ok {
		return fmt.Errorf(`ante:for %q: want "name[, index] of expr"`, src)
	}
	itemName, idxName, _ := strings.Cut(strings.TrimSpace(names), ",")
	itemName, idxName = strings.TrimSpace(itemName), strings.TrimSpace(idxName)

	v, err := e.eval(arrExpr, scope)
	if err != nil {
		return err
	}
	arr := v.ToObject(e.vm)
	length := arr.Get("length").ToInteger()

	for i := int64(0); i < length; i++ {
		iter, err := e.childScope(scope)
		if err != nil {
			return err
		}
		iter.Set(itemName, arr.Get(strconv.FormatInt(i, 10)))
		if idxName != "" {
			iter.Set(idxName, i)
		}
		copy := clone(n)
		n.Parent.InsertBefore(copy, n)
		if err := e.apply(copy, iter, consume); err != nil {
			return err
		}
	}
	n.Parent.RemoveChild(n)
	return nil
}

// hasDirectives reports whether n carries any ante: attribute.
func hasDirectives(n *html.Node) bool {
	for _, a := range n.Attr {
		if strings.HasPrefix(a.Key, Prefix) {
			return true
		}
	}
	return false
}

func takeAttr(n *html.Node, key string) (string, bool) {
	for i, a := range n.Attr {
		if a.Key == key {
			n.Attr = append(n.Attr[:i], n.Attr[i+1:]...)
			return a.Val, true
		}
	}
	return "", false
}

func setChildren(n *html.Node, kids ...*html.Node) {
	for n.FirstChild != nil {
		n.RemoveChild(n.FirstChild)
	}
	for _, k := range kids {
		n.AppendChild(k)
	}
}

// unwrap replaces n with its children.
func unwrap(n *html.Node) {
	for n.FirstChild != nil {
		c := n.FirstChild
		n.RemoveChild(c)
		n.Parent.InsertBefore(c, n)
	}
	n.Parent.RemoveChild(n)
}

func clone(n *html.Node) *html.Node {
	c := &html.Node{
		Type:     n.Type,
		DataAtom: n.DataAtom,
		Data:     n.Data,
		Attr:     append([]html.Attribute(nil), n.Attr...),
	}
	for k := n.FirstChild; k != nil; k = k.NextSibling {
		c.AppendChild(clone(k))
	}
	return c
}

func valueString(v sobek.Value) string {
	if v == sobek.Undefined() || v == sobek.Null() {
		return ""
	}
	return v.String()
}
