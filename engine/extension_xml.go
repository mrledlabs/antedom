package antedom

// This file implements the safe text-output primitives exposed to project
// JavaScript. antedom.xml is a tagged template whose dynamic values are
// escaped in Go; antedom.url.resolve turns page paths into canonical absolute
// HTTP(S) URLs beneath a configured base. Complete XML artifacts are validated
// by ArtifactWriter before publication (see output.go).

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/grafana/sobek"
)

// xmlFragment is an opaque, already-escaped fragment produced by the
// antedom.xml tagged template. Static template text remains markup; every
// interpolated scalar is escaped with encoding/xml.
type xmlFragment struct {
	data []byte
}

func (*xmlFragment) Get(string) sobek.Value       { return nil }
func (*xmlFragment) Set(string, sobek.Value) bool { return false }
func (*xmlFragment) Has(string) bool              { return false }
func (*xmlFragment) Delete(string) bool           { return false }
func (*xmlFragment) Keys() []string               { return nil }

func (e *projectExtension) xmlTemplate(call sobek.FunctionCall) sobek.Value {
	e.requireVersion("antedom.xml")
	stringsValue := call.Argument(0)
	stringsObject, ok := stringsValue.(*sobek.Object)
	if !ok || stringsObject.ClassName() != "Array" {
		panic(e.vm.NewTypeError("antedom.xml must be used as a tagged template"))
	}
	length := int(stringsObject.Get("length").ToInteger())
	if length != len(call.Arguments) {
		panic(e.vm.NewTypeError("antedom.xml received an invalid template"))
	}
	var result bytes.Buffer
	for i := 0; i < length; i++ {
		static := stringsObject.Get(strconv.Itoa(i))
		if static == nil || sobek.IsUndefined(static) {
			panic(e.vm.NewTypeError("antedom.xml received an invalid template"))
		}
		result.WriteString(static.String())
		if i+1 == length {
			continue
		}
		if err := appendXMLInterpolation(&result, call.Argument(i+1)); err != nil {
			panic(e.vm.NewTypeError("antedom.xml interpolation %d: %s", i, err))
		}
	}
	return e.vm.NewDynamicObject(&xmlFragment{data: result.Bytes()})
}

func (e *projectExtension) xmlJoin(call sobek.FunctionCall) sobek.Value {
	e.requireVersion("antedom.xml.join")
	value := call.Argument(0)
	array, ok := value.(*sobek.Object)
	if !ok || array.ClassName() != "Array" {
		panic(e.vm.NewTypeError("antedom.xml.join requires an array of antedom.xml fragments"))
	}
	var result bytes.Buffer
	length := int(array.Get("length").ToInteger())
	for i := 0; i < length; i++ {
		item := array.Get(strconv.Itoa(i))
		if item == nil || sobek.IsUndefined(item) {
			panic(e.vm.NewTypeError("antedom.xml.join item %d must be an antedom.xml fragment", i))
		}
		fragment, ok := item.Export().(*xmlFragment)
		if !ok {
			panic(e.vm.NewTypeError("antedom.xml.join item %d must be an antedom.xml fragment", i))
		}
		result.Write(fragment.data)
	}
	return e.vm.NewDynamicObject(&xmlFragment{data: result.Bytes()})
}

func appendXMLInterpolation(dst *bytes.Buffer, value sobek.Value) error {
	if fragment, ok := value.Export().(*xmlFragment); ok {
		dst.Write(fragment.data)
		return nil
	}
	switch value.Export().(type) {
	case string, bool, int64, float64:
		text := value.String()
		if !utf8.ValidString(text) {
			return fmt.Errorf("contains invalid UTF-8")
		}
		for _, r := range text {
			if !validXMLCharacter(r) {
				return fmt.Errorf("contains illegal XML character U+%04X", r)
			}
		}
		if err := xml.EscapeText(dst, []byte(text)); err != nil {
			return err
		}
		return nil
	default:
		return fmt.Errorf("must be a string, number, boolean, or antedom.xml fragment")
	}
}

func validXMLCharacter(r rune) bool {
	return r == 0x09 || r == 0x0A || r == 0x0D ||
		r >= 0x20 && r <= 0xD7FF ||
		r >= 0xE000 && r <= 0xFFFD ||
		r >= 0x10000 && r <= 0x10FFFF
}

func (e *projectExtension) resolveURL(call sobek.FunctionCall) sobek.Value {
	e.requireVersion("antedom.url.resolve")
	baseValue, baseOK := call.Argument(0).Export().(string)
	pathValue, pathOK := call.Argument(1).Export().(string)
	if !baseOK || baseValue == "" || !pathOK {
		panic(e.vm.NewTypeError("antedom.url.resolve requires a base URL and path"))
	}
	base, err := url.Parse(baseValue)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		panic(e.vm.NewTypeError("antedom.url.resolve base must be an absolute HTTP(S) URL without credentials, query, or fragment"))
	}
	base.Path = strings.TrimSuffix(base.Path, "/") + "/"
	base.RawPath = ""
	reference := &url.URL{Path: strings.TrimPrefix(pathValue, "/")}
	resolved := base.ResolveReference(reference)
	if !strings.HasPrefix(resolved.Path, base.Path) {
		panic(e.vm.NewTypeError("antedom.url.resolve path must stay beneath the base URL"))
	}
	return e.vm.ToValue(resolved.String())
}

func (e *projectExtension) requireVersion(api string) {
	if !e.versionCalled {
		panic(e.vm.NewTypeError("antedom.apiVersion(1) must be called before %s", api))
	}
}
