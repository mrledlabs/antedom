package antedom

// This file contains HTML-fragment capabilities for generated outputs.
// resolveURLs parses a processed content fragment, resolves URL-bearing
// attributes against the page's canonical URL, and serializes the fragment
// again without adding a document, head, or body wrapper.

import (
	"bytes"
	"fmt"
	"net/url"
	"strings"

	"github.com/grafana/sobek"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

func (e *projectExtension) resolveHTMLURLs(call sobek.FunctionCall) sobek.Value {
	e.requireVersion("antedom.html.resolveURLs")
	fragment, fragmentOK := call.Argument(0).Export().(string)
	baseValue, baseOK := call.Argument(1).Export().(string)
	if !fragmentOK || !baseOK || baseValue == "" {
		panic(e.vm.NewTypeError("antedom.html.resolveURLs requires an HTML fragment and absolute page URL"))
	}
	base, err := url.Parse(baseValue)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" || base.User != nil {
		panic(e.vm.NewTypeError("antedom.html.resolveURLs page URL must be an absolute HTTP(S) URL without credentials"))
	}

	context := &html.Node{Type: html.ElementNode, DataAtom: atom.Div, Data: "div"}
	nodes, err := html.ParseFragment(strings.NewReader(fragment), context)
	if err != nil {
		panic(e.vm.NewGoError(fmt.Errorf("parsing HTML fragment: %w", err)))
	}
	for _, node := range nodes {
		if err := resolveNodeURLs(node, base); err != nil {
			panic(e.vm.NewGoError(err))
		}
	}
	var result bytes.Buffer
	for _, node := range nodes {
		if err := html.Render(&result, node); err != nil {
			panic(e.vm.NewGoError(err))
		}
	}
	return e.vm.ToValue(result.String())
}

func resolveNodeURLs(node *html.Node, base *url.URL) error {
	if node.Type == html.ElementNode {
		for i := range node.Attr {
			attribute := &node.Attr[i]
			if !htmlURLAttribute(attribute.Key) || strings.TrimSpace(attribute.Val) == "" {
				continue
			}
			reference, err := url.Parse(attribute.Val)
			if err != nil {
				return fmt.Errorf("resolving <%s> %s=%q: %w", node.Data, attribute.Key, attribute.Val, err)
			}
			attribute.Val = base.ResolveReference(reference).String()
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if err := resolveNodeURLs(child, base); err != nil {
			return err
		}
	}
	return nil
}

func htmlURLAttribute(name string) bool {
	switch strings.ToLower(name) {
	case "href", "src", "poster", "cite", "action", "formaction":
		return true
	default:
		return false
	}
}
