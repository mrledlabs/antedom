package antedom

import (
	"bytes"
	"strings"
	"testing"
)

func TestScopeScriptDeclarations(t *testing.T) {
	// const, let, var, function, and destructuring all land in scope.
	out := render(t, `<body>
<script ante:scope>
  const a = 'A';
  let b = a.toLowerCase();
  var c = 1 + 2;
  function d(x) { return x + '!' }
  const {e, f = 'F'} = {e: 'E'};
  const [g] = ['G'];
</script>
<p ante:text="a + b + c + d('x') + e + f + g"></p>
</body>`, nil)
	if !strings.Contains(out, "<p>Aa3x!EFG</p>") {
		t.Errorf("bad output: %s", out)
	}
	if strings.Contains(out, "<script") {
		t.Errorf("scope script not dropped: %s", out)
	}
}

func TestScopeScriptSeesOuter(t *testing.T) {
	// Free variables resolve in the surrounding scope.
	out := render(t, `<body><script ante:scope>const loud = name.toUpperCase();</script><p ante:text="loud"></p></body>`,
		map[string]any{"name": "go"})
	if !strings.Contains(out, "<p>GO</p>") {
		t.Errorf("bad output: %s", out)
	}
}

func TestScopeScriptVisibility(t *testing.T) {
	// Declarations reach following siblings and their subtrees, not
	// elements before the script or outside the containing element.
	out := render(t, `<body><div><i ante:text="typeof v"></i><script ante:scope>const v = 'in';</script><span><b ante:text="v"></b></span></div><p ante:text="typeof v"></p></body>`,
		nil)
	for _, want := range []string{"<i>undefined</i>", "<b>in</b>", "<p>undefined</p>"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q in %s", want, out)
		}
	}
}

func TestScopeScriptInShortcode(t *testing.T) {
	// The user-facing point: compute derived values from attributes.
	out := renderShortcodes(t,
		`<body><shortcode-quotefig citeurl="https://example.com/q" citetext="someone">Quote.</shortcode-quotefig></body>`,
		map[string]string{
			"quotefig": `<figure class="quotefig">
  <script ante:scope>
    const caption = ` + "`" + `<a href="${citeurl}">${citetext}</a>` + "`" + `;
  </script>
  <blockquote ante:cite="citeurl"><p ante:html="children"></p></blockquote>
  <figcaption ante:html="caption"></figcaption>
</figure>`,
		}, nil)
	for _, want := range []string{
		`<blockquote cite="https://example.com/q"><p>Quote.</p></blockquote>`,
		`<figcaption><a href="https://example.com/q">someone</a></figcaption>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q in %s", want, out)
		}
	}
}

func TestScopeScriptNonScript(t *testing.T) {
	e, err := New()
	if err != nil {
		t.Fatal(err)
	}
	err = e.Render(&bytes.Buffer{}, []byte(`<body><div ante:scope>nope</div></body>`), nil)
	if err == nil || !strings.Contains(err.Error(), "want <script>") {
		t.Errorf("want host error, got %v", err)
	}
}

func TestScopeScriptSyntaxError(t *testing.T) {
	e, err := New()
	if err != nil {
		t.Fatal(err)
	}
	err = e.Render(&bytes.Buffer{}, []byte(`<body><script ante:scope>const = ;</script></body>`), nil)
	if err == nil {
		t.Error("want parse error, got nil")
	}
}
