package antedom

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// renderShortcodes renders src with the given shortcode templates.
func renderShortcodes(t *testing.T, src string, templates map[string]string, data map[string]any) string {
	t.Helper()
	e, err := New()
	if err != nil {
		t.Fatal(err)
	}
	e.Shortcodes = func(name string) ([]byte, error) {
		tpl, ok := templates[name]
		if !ok {
			return nil, fmt.Errorf("no template %q", name)
		}
		return []byte(tpl), nil
	}
	var buf bytes.Buffer
	if err := e.Render(&buf, []byte(src), data); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestShortcodeQuotefig(t *testing.T) {
	out := renderShortcodes(t,
		`<body><shortcode-quotefig cite="https://example.com/q#1" caption="<a href='https://example.com/u'>someone</a>">Ask meets Guess.</shortcode-quotefig></body>`,
		map[string]string{
			"quotefig": `<figure class="quotefig">
  <blockquote ante:cite="cite"><p ante:html="children"></p></blockquote>
  <figcaption ante:html="caption"></figcaption>
</figure>`,
		}, nil)
	for _, want := range []string{
		`<figure class="quotefig">`,
		`<blockquote cite="https://example.com/q#1"><p>Ask meets Guess.</p></blockquote>`,
		`<figcaption><a href="https://example.com/u">someone</a></figcaption>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q in %s", want, out)
		}
	}
	if strings.Contains(out, "shortcode-") {
		t.Errorf("shortcode element survived: %s", out)
	}
}

func TestShortcodeChildrenRendered(t *testing.T) {
	// Directives inside the shortcode's children run in the page scope
	// before expansion; bound attributes on the element evaluate too.
	out := renderShortcodes(t,
		`<body><shortcode-box ante:kind="k"><i ante:text="msg">x</i></shortcode-box></body>`,
		map[string]string{"box": `<div ante:class="kind" ante:html="children"></div>`},
		map[string]any{"k": "note", "msg": "hi"})
	if want := `<div class="note"><i>hi</i></div>`; !strings.Contains(out, want) {
		t.Errorf("want %q in %s", want, out)
	}
}

func TestShortcodeInLoop(t *testing.T) {
	out := renderShortcodes(t,
		`<body><template ante:for="w of words"><shortcode-tag ante:label="w"></shortcode-tag></template></body>`,
		map[string]string{"tag": `<span class="tag" ante:text="label"></span>`},
		map[string]any{"words": []any{"go", "ssg"}})
	if want := `<span class="tag">go</span><span class="tag">ssg</span>`; !strings.Contains(out, want) {
		t.Errorf("want %q in %s", want, out)
	}
}

func TestShortcodeNested(t *testing.T) {
	// A shortcode template may use shortcodes itself.
	out := renderShortcodes(t,
		`<body><shortcode-outer title="T">body</shortcode-outer></body>`,
		map[string]string{
			"outer": `<section><h2 ante:text="title"></h2><shortcode-inner ante:html="children"></shortcode-inner></section>`,
			"inner": `<div class="inner" ante:html="children"></div>`,
		}, nil)
	if want := `<section><h2>T</h2><div class="inner">body</div></section>`; !strings.Contains(out, want) {
		t.Errorf("want %q in %s", want, out)
	}
}

func TestShortcodeCycle(t *testing.T) {
	e, err := New()
	if err != nil {
		t.Fatal(err)
	}
	e.Shortcodes = func(name string) ([]byte, error) {
		return []byte(`<shortcode-loop></shortcode-loop>`), nil
	}
	err = e.Render(&bytes.Buffer{}, []byte(`<body><shortcode-loop></shortcode-loop></body>`), nil)
	if err == nil || !strings.Contains(err.Error(), "deeper than") {
		t.Errorf("want depth error, got %v", err)
	}
}

func TestShortcodeUnknown(t *testing.T) {
	e, err := New()
	if err != nil {
		t.Fatal(err)
	}
	e.Shortcodes = func(name string) ([]byte, error) { return nil, fmt.Errorf("no template %q", name) }
	if err := e.Render(&bytes.Buffer{}, []byte(`<body><shortcode-nope></shortcode-nope></body>`), nil); err == nil {
		t.Error("want load error, got nil")
	}
}

func TestShortcodeUnconfigured(t *testing.T) {
	e, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Render(&bytes.Buffer{}, []byte(`<body><shortcode-x></shortcode-x></body>`), nil); err == nil {
		t.Error("want error with no Shortcodes loader, got nil")
	}
}
