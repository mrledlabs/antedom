package antedom

// Markdown pages: a pages/**/*.md file is CommonMark content,
// rendered to HTML by goldmark before the normal parse/compose/
// directive pipeline. All ante: machinery — layout/fill templates,
// directive-bearing elements — is written as raw HTML inside the
// markdown, following CommonMark's HTML-block rules (blank lines
// around block-level tags). Authoring rules: docs/markdown.md.

import (
	"bytes"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/parser"
	ghtml "github.com/yuin/goldmark/renderer/html"
	"golang.org/x/net/html"
)

// md converts CommonMark to HTML. WithUnsafe passes raw HTML through
// verbatim — pages are trusted input, and the ante: machinery depends
// on it. Headings get slugified ids for fragment links.
var md = goldmark.New(
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	goldmark.WithRendererOptions(ghtml.WithUnsafe()),
)

// parseMarkdown renders CommonMark src and parses the result as a
// page document, ready for Compose and the directive walk.
func parseMarkdown(src []byte) (*html.Node, error) {
	var buf bytes.Buffer
	if err := md.Convert(src, &buf); err != nil {
		return nil, err
	}
	return html.Parse(&buf)
}
