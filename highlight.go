package antedom

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

type highlightOptions struct {
	Style           string
	UnknownLanguage string
}

func defaultHighlightOptions() highlightOptions {
	return highlightOptions{Style: "github", UnknownLanguage: "ignore"}
}

// highlightDocument highlights literal <pre><code class="language-*"> blocks
// in doc. It runs before antedom directives and shortcodes, so content they
// generate later must request highlighting explicitly.
//
// Calling it again (for example from a second hook) matches highlighted
// blocks anew: the language class survives, text content round-trips through
// the token spans, and the block is re-highlighted and re-counted with the
// later call's style. The MVP accepts that rather than marking blocks done.
func highlightDocument(doc *html.Node, options highlightOptions) (int, error) {
	style, ok := styles.Registry[strings.ToLower(options.Style)]
	if !ok {
		return 0, fmt.Errorf("unknown highlight style %q", options.Style)
	}
	formatter := chromahtml.New(chromahtml.PreventSurroundingPre(true))
	count := 0
	var walk func(*html.Node) error
	walk = func(node *html.Node) error {
		if node.Type == html.ElementNode && node.DataAtom == atom.Code &&
			node.Parent != nil && node.Parent.DataAtom == atom.Pre {
			language := codeLanguage(node)
			if language != "" {
				lexer := lexers.Get(language)
				if lexer == nil {
					if options.UnknownLanguage == "error" {
						return fmt.Errorf("unknown highlight language %q", language)
					}
				} else {
					iterator, err := chroma.Coalesce(lexer).Tokenise(nil, text(node))
					if err != nil {
						return fmt.Errorf("highlighting language %q: %w", language, err)
					}
					var rendered bytes.Buffer
					if err := formatter.Format(&rendered, style, iterator); err != nil {
						return fmt.Errorf("formatting language %q: %w", language, err)
					}
					fragment, err := html.ParseFragment(strings.NewReader(rendered.String()), node)
					if err != nil {
						return fmt.Errorf("parsing highlighted language %q: %w", language, err)
					}
					setChildren(node, fragment...)
					count++
					return nil // do not traverse the generated token spans
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	return count, walk(doc)
}

func codeLanguage(node *html.Node) string {
	for _, attr := range node.Attr {
		if attr.Key != "class" {
			continue
		}
		for class := range strings.FieldsSeq(attr.Val) {
			for _, prefix := range []string{"language-", "lang-"} {
				if language := strings.TrimPrefix(class, prefix); language != class && language != "" {
					return language
				}
			}
		}
	}
	return ""
}
