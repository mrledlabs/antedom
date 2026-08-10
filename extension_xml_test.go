package antedom

import (
	"context"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mrled/antedom/testsites"
)

func TestProjectExtensionSafeXMLAndURL(t *testing.T) {
	root := t.TempDir()
	if err := testsites.Blog(root, 0); err != nil {
		t.Fatal(err)
	}
	writeExtension(t, root, ""+`
antedom.apiVersion(1);
antedom.output("xml", {
  file: "safe.xml",
  validate: "xml",
  end(_, output) {
    const title = '<title & "quoted"> π';
    const nested = antedom.xml`+"`"+`<nested>${title}</nested>`+"`"+`;
    const children = antedom.xml.join([
      antedom.xml`+"`"+`<child>${true}</child>`+"`"+`,
      antedom.xml`+"`"+`<child>${42}</child>`+"`"+`,
    ]);
    const location = antedom.url.resolve("https://example.com/base", "/a b?#%/");
    output.write(antedom.xml`+"`"+`<?xml version="1.0" encoding="UTF-8"?>
<root title="${title}">${nested}${children}<location>${location}</location></root>
`+"`"+`);
  },
});
`)
	out := t.TempDir()
	if _, err := NewProject(root).Operation(context.Background(), OperationBuild).Build(out); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(out, "safe.xml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`title="&lt;title &amp; &#34;quoted&#34;&gt; π"`,
		`<nested>&lt;title &amp; &#34;quoted&#34;&gt; π</nested>`,
		`<child>true</child><child>42</child>`,
		`<location>https://example.com/base/a%20b%3F%23%25/</location>`,
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("safe XML missing %q: %s", want, data)
		}
	}
	var decoded any
	if err := xml.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("generated XML is invalid: %v", err)
	}
}

func TestProjectExtensionXMLValidationFailureCleansUp(t *testing.T) {
	root := t.TempDir()
	if err := testsites.Blog(root, 0); err != nil {
		t.Fatal(err)
	}
	writeExtension(t, root, `
antedom.apiVersion(1);
antedom.output("broken", {
  file: "broken.xml",
  validate: "xml",
  end(_, output) { output.write(antedom.xml`+"`"+`<root><open></root>`+"`"+`); },
});
`)
	out := t.TempDir()
	_, err := NewProject(root).Operation(context.Background(), OperationBuild).Build(out)
	if err == nil {
		t.Fatal("malformed XML did not fail the build")
	}
	for _, want := range []string{"antedom.js", `output "broken"`, "validate broken.xml", "element <open> closed by </root>"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
	if _, statErr := os.Stat(filepath.Join(out, "broken.xml")); !os.IsNotExist(statErr) {
		t.Fatalf("invalid XML was published: %v", statErr)
	}
}

func TestProjectExtensionXMLRejectsUnsafeInterpolations(t *testing.T) {
	for name, expression := range map[string]string{
		"undefined": "undefined",
		"object":    "{}",
		"function":  "() => 1",
		"control":   `"bad\u0001text"`,
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := testsites.Blog(root, 0); err != nil {
				t.Fatal(err)
			}
			writeExtension(t, root, `
antedom.apiVersion(1);
antedom.output("xml", {
  file: "bad.xml",
  end(_, output) { output.write(antedom.xml`+"`"+`<root>${`+expression+`}</root>`+"`"+`); },
});
`)
			_, err := NewProject(root).Operation(context.Background(), OperationBuild).Build(t.TempDir())
			if err == nil || !strings.Contains(err.Error(), "antedom.xml interpolation 0") {
				t.Fatalf("unsafe interpolation error = %v", err)
			}
		})
	}
}
