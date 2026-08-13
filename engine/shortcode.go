package antedom

// Shortcodes: inert web components, expanded once at build time.
// An element named shortcode-<name> is replaced, at the end of its
// directive pipeline, by the fragment template Engine.Shortcodes
// loads for name — for a Site, layout/shortcode/<name>.html. The
// template is ordinary antedom HTML evaluated in a child scope where
// each attribute of the element is a variable ($scope["data-x"] for
// names that are not JS identifiers) and children is the element's
// inner HTML, both fully rendered before expansion. Templates may use
// shortcodes themselves; cycles are cut by a depth cap.
// Authoring guide: docs/pages/shortcodes.md.

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/grafana/sobek"
	"golang.org/x/net/html"
)

// ShortcodePrefix marks elements expanded as shortcodes.
const ShortcodePrefix = "shortcode-"

// maxShortcodeDepth caps template-in-template expansion, cutting cycles.
const maxShortcodeDepth = 10

// expandShortcode replaces n — a shortcode-<name> element whose own
// directives have already run, so bound attributes are evaluated and
// children rendered — with its template. The fragment is parsed in
// n's parent context, spliced in place of n, then walked like any
// other content so its directives (and nested shortcodes) evaluate.
func (e *Engine) expandShortcode(n *html.Node, scope *sobek.Object) error {
	if e.Shortcodes == nil {
		return fmt.Errorf("<%s>: no shortcode templates configured", n.Data)
	}
	if e.depth >= maxShortcodeDepth {
		return fmt.Errorf("<%s>: shortcodes nested deeper than %d", n.Data, maxShortcodeDepth)
	}
	name := strings.TrimPrefix(n.Data, ShortcodePrefix)
	src, err := e.Shortcodes(name)
	if err != nil {
		return fmt.Errorf("<%s>: %w", n.Data, err)
	}
	frag, err := html.ParseFragment(bytes.NewReader(src), n.Parent)
	if err != nil {
		return fmt.Errorf("<%s>: %w", n.Data, err)
	}
	sc, err := e.childScope(scope)
	if err != nil {
		return err
	}
	for _, a := range n.Attr {
		sc.Set(a.Key, a.Val)
	}
	var children bytes.Buffer
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if err := html.Render(&children, c); err != nil {
			return err
		}
	}
	sc.Set("children", children.String()) // after attrs: always the inner HTML
	for _, f := range frag {
		n.Parent.InsertBefore(f, n)
	}
	n.Parent.RemoveChild(n)
	e.depth++
	defer func() { e.depth-- }()
	return e.walkSiblings(frag, sc)
}
