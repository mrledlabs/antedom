package antedom

// Layout composition: ante:layout / ante:slot / ante:fill.
// A page names its layout explicitly; a layout may name another,
// forming a chain that ends at a base document. Fills fold in
// outward-in — base first, the page last — each level filling
// whatever slots are still open. Design: docs/templating.md.

import (
	"fmt"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Compose resolves doc's ante:layout chain and returns the base
// document with every level's fills grafted in. load parses the file
// named by an ante:layout attribute. A doc with no ante:layout is
// returned unchanged. Fills fold outward-in — base first, the page
// last — and each fill grafts into the first still-open slot of its
// name in document order, so a chain of default (bare ante:slot)
// slots pipes content through: each level's bare ante:fill lands in
// the bare slot one level out. Slot children are replaced by the fill
// element (fallbacks stay where nothing fills); the ante:slot/
// ante:fill attributes are left in place for the directive walk to
// consume, so marker <template>s still unwrap.
func Compose(doc *html.Node, load func(name string) (*html.Node, error)) (*html.Node, error) {
	const maxDepth = 10
	var levels [][]*html.Node // fills, innermost (page) first
	for {
		root := findAttr(doc, Prefix+"layout")
		if root == nil {
			break
		}
		if len(levels) == maxDepth {
			return nil, fmt.Errorf("ante:layout: chain deeper than %d", maxDepth)
		}
		name, _ := takeAttr(root, Prefix+"layout")
		levels = append(levels, detachFills(root))
		next, err := load(name)
		if err != nil {
			return nil, fmt.Errorf("ante:layout %q: %w", name, err)
		}
		doc = next
	}
	filled := map[*html.Node]bool{}
	for i := len(levels) - 1; i >= 0; i-- {
		for _, fill := range levels[i] {
			name, _ := attrVal(fill, Prefix+"fill")
			slot := find(doc, func(el *html.Node) bool {
				v, ok := attrVal(el, Prefix+"slot")
				return ok && v == name && !filled[el]
			})
			if slot != nil {
				setChildren(slot, fill)
				filled[slot] = true
			}
		}
	}
	return doc, nil
}

// detachFills removes and returns root's element children bearing
// ante:fill; other children are dropped with root.
func detachFills(root *html.Node) []*html.Node {
	var fills []*html.Node
	for c := root.FirstChild; c != nil; {
		next := c.NextSibling
		if c.Type == html.ElementNode {
			if _, ok := attrVal(c, Prefix+"fill"); ok {
				root.RemoveChild(c)
				fills = append(fills, c)
			}
		}
		c = next
	}
	return fills
}

// findAttr returns the first element (depth-first) with attribute
// key. Plain <template> subtrees are client-bound and not searched.
func findAttr(n *html.Node, key string) *html.Node {
	return find(n, func(el *html.Node) bool {
		_, ok := attrVal(el, key)
		return ok
	})
}

func find(n *html.Node, match func(*html.Node) bool) *html.Node {
	if n.Type == html.ElementNode {
		if match(n) {
			return n
		}
		if n.DataAtom == atom.Template && !hasDirectives(n) {
			return nil
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if got := find(c, match); got != nil {
			return got
		}
	}
	return nil
}

func attrVal(n *html.Node, key string) (string, bool) {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val, true
		}
	}
	return "", false
}
