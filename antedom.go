// Package antedom renders antedom templates:
// valid HTML where templating logic lives in ante:-prefixed attributes
// and the expression language is JavaScript (sobek).
// The logic runs at build time, before any DOM exists — hence the name.
//
// Directive attributes, processed in this order:
//
//	ante:if="expr"          remove the element unless expr is truthy
//	ante:for="x[, i] of expr"  repeat the element per item of the array expr
//	ante:text="expr"        replace children with expr as escaped text
//	ante:html="expr"        replace children with expr parsed as HTML
//	ante:name="expr"        set attribute name to expr; false/null/undefined
//	                     drops the attribute, true makes it boolean
//
// A <template> element is unwrapped after processing, so it can group
// siblings under one ante:if/ante:for without emitting a wrapper tag.
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
	e.mu.Lock()
	defer e.mu.Unlock()

	doc, err := html.Parse(bytes.NewReader(src))
	if err != nil {
		return err
	}
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
	for _, c := range kids {
		if err := e.element(c, scope); err != nil {
			return err
		}
	}
	return nil
}

// element applies the directives of one element, recursing into its
// subtree. It may remove n or replace it with copies of itself.
func (e *Engine) element(n *html.Node, scope *sobek.Object) error {
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
		return e.loop(n, src, scope)
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

	if n.DataAtom == atom.Template {
		unwrap(n)
	}
	return nil
}

// loop expands `ante:for="item[, index] of expr"`: one processed copy of n
// per array element, each under a child scope binding the loop names.
func (e *Engine) loop(n *html.Node, src string, scope *sobek.Object) error {
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
		if err := e.element(copy, iter); err != nil {
			return err
		}
	}
	n.Parent.RemoveChild(n)
	return nil
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
