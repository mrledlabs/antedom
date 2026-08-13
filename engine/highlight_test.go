package antedom

import (
	"bytes"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestHighlightDocument(t *testing.T) {
	doc, err := html.Parse(strings.NewReader(`<html><body>
<pre><code class="language-go extra">package main
func main() {}</code></pre>
<pre><code>plain &amp; unlabelled</code></pre>
<pre style="margin:0"><code class="language-go">var kept int</code></pre>
</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	count, err := highlightDocument(doc, defaultHighlightOptions())
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("highlighted %d blocks, want 2", count)
	}
	var out bytes.Buffer
	if err := html.Render(&out, doc); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<code class="language-go extra"><span`,
		`>package</span>`,
		`<code>plain &amp; unlabelled</code>`,
		// The theme's base colors are applied to the block's <pre>, appended
		// after any existing declarations. The default github theme defines
		// only a background; themes with a base foreground emit color too.
		`<pre style="background-color:#`,
		`<pre style="margin:0;background-color:#`,
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("highlighted document lacks %q:\n%s", want, out.String())
		}
	}
}

func TestHighlightDocumentOptions(t *testing.T) {
	doc, err := html.Parse(strings.NewReader(`<pre><code class="language-no-such-language">x</code></pre>`))
	if err != nil {
		t.Fatal(err)
	}
	if count, err := highlightDocument(doc, defaultHighlightOptions()); err != nil || count != 0 {
		t.Fatalf("ignored missing language = (%d, %v), want (0, nil)", count, err)
	}
	options := defaultHighlightOptions()
	options.UnknownLanguage = "error"
	if _, err := highlightDocument(doc, options); err == nil || !strings.Contains(err.Error(), "no-such-language") {
		t.Fatalf("missing-language error = %v", err)
	}
	options = defaultHighlightOptions()
	options.Style = "no-such-style"
	if _, err := highlightDocument(doc, options); err == nil || !strings.Contains(err.Error(), "no-such-style") {
		t.Fatalf("missing-style error = %v", err)
	}
}
