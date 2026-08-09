package antedom

// Scope scripts: <script ante:scope> runs its body once, at build
// time, where it stands in the tree. Top-level declarations — var,
// let, const, function, class, destructuring included — become scope
// variables for the element's following siblings and their subtrees
// (a child scope, so nothing escapes the containing element). The
// element itself is dropped from output. Free variables resolve in
// the surrounding scope, so inside a shortcode template a scope
// script sees the use-site attributes and children, and can compute
// derived values for the rest of the template.

import (
	"fmt"
	"strings"

	"github.com/grafana/sobek"
	"github.com/grafana/sobek/ast"
	"github.com/grafana/sobek/parser"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// runScope evaluates one <script ante:scope> (already stripped of
// the attribute), removes it from the tree, and returns a child of
// scope holding the script's top-level declarations.
func (e *Engine) runScope(sc *html.Node, scope *sobek.Object) (*sobek.Object, error) {
	if sc.DataAtom != atom.Script {
		return nil, fmt.Errorf("ante:scope on <%s>, want <script>", sc.Data)
	}
	src := text(sc)
	sc.Parent.RemoveChild(sc)
	fn, err := e.scopeFn(src)
	if err != nil {
		return nil, err
	}
	child, err := e.childScope(scope)
	if err != nil {
		return nil, err
	}
	if _, err := fn(sobek.Undefined(), child); err != nil {
		return nil, fmt.Errorf("ante:scope: %w", err)
	}
	return child, nil
}

// scopeFn compiles a scope script body to a callable of the scope
// object: the body runs with the scope on the JS scope chain (like
// directive expressions), then each top-level declared name is copied
// onto the scope object, where descendant directives resolve it.
func (e *Engine) scopeFn(src string) (sobek.Callable, error) {
	names, err := scopeNames(src)
	if err != nil {
		return nil, fmt.Errorf("ante:scope: %w", err)
	}
	var b strings.Builder
	b.WriteString("(function($scope){with($scope){")
	b.WriteString(src)
	b.WriteString("\n")
	for _, name := range names {
		fmt.Fprintf(&b, ";$scope[%q]=%s", name, name)
	}
	b.WriteString("}})")
	wrapped := b.String()
	if fn, ok := e.exprs[wrapped]; ok {
		return fn, nil
	}
	v, err := e.vm.RunString(wrapped)
	if err != nil {
		return nil, fmt.Errorf("ante:scope: compiling: %w", err)
	}
	fn, _ := sobek.AssertFunction(v)
	e.exprs[wrapped] = fn
	return fn, nil
}

// scopeNames parses src and returns the names its top-level
// statements declare.
func scopeNames(src string) ([]string, error) {
	prog, err := parser.ParseFile(nil, "", src, 0)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, st := range prog.Body {
		switch s := st.(type) {
		case *ast.VariableStatement:
			for _, bd := range s.List {
				bindingNames(bd.Target, &names)
			}
		case *ast.LexicalDeclaration:
			for _, bd := range s.List {
				bindingNames(bd.Target, &names)
			}
		case *ast.FunctionDeclaration:
			if s.Function.Name != nil {
				names = append(names, s.Function.Name.Name.String())
			}
		case *ast.ClassDeclaration:
			if s.Class.Name != nil {
				names = append(names, s.Class.Name.Name.String())
			}
		}
	}
	return names, nil
}

// bindingNames collects the identifiers a binding target declares,
// recursing through destructuring patterns and their defaults.
func bindingNames(t ast.Expression, names *[]string) {
	switch p := t.(type) {
	case *ast.Identifier:
		*names = append(*names, p.Name.String())
	case *ast.ArrayPattern:
		for _, el := range p.Elements {
			if el != nil {
				bindingNames(el, names)
			}
		}
		if p.Rest != nil {
			bindingNames(p.Rest, names)
		}
	case *ast.ObjectPattern:
		for _, pr := range p.Properties {
			switch q := pr.(type) {
			case *ast.PropertyShort:
				*names = append(*names, q.Name.Name.String())
			case *ast.PropertyKeyed:
				bindingNames(q.Value, names)
			}
		}
		if p.Rest != nil {
			bindingNames(p.Rest, names)
		}
	case *ast.AssignExpression: // pattern default: {x = 1}
		bindingNames(p.Left, names)
	}
}
