package antedom

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type eventOutput struct {
	name       string
	events     *[]string
	failBegin  bool
	failPage   bool
	failCommit bool
}

func (o *eventOutput) event(name string) { *o.events = append(*o.events, o.name+"."+name) }
func (o *eventOutput) Begin(context.Context, *BuildPlan) error {
	o.event("begin")
	if o.failBegin {
		return errors.New("begin failed")
	}
	return nil
}
func (o *eventOutput) WritePage(context.Context, *RenderedPage) error {
	o.event("page")
	if o.failPage {
		return errors.New("page failed")
	}
	return nil
}
func (o *eventOutput) WriteAsset(context.Context, *Asset) error {
	o.event("asset")
	return nil
}
func (o *eventOutput) Commit(context.Context) error {
	o.event("commit")
	if o.failCommit {
		return errors.New("commit failed")
	}
	return nil
}
func (o *eventOutput) Abort(context.Context) error { o.event("abort"); return nil }

func TestOutputGroupLifecycle(t *testing.T) {
	var events []string
	group := NewOutputGroup(
		&eventOutput{name: "html", events: &events},
		&eventOutput{name: "json", events: &events},
	)
	ctx := context.Background()
	if err := group.Begin(ctx, &BuildPlan{}); err != nil {
		t.Fatal(err)
	}
	if err := group.WriteAsset(ctx, &Asset{}); err != nil {
		t.Fatal(err)
	}
	if err := group.WritePage(ctx, &RenderedPage{}); err != nil {
		t.Fatal(err)
	}
	if err := group.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"html.begin", "json.begin",
		"html.asset", "json.asset",
		"html.page", "json.page",
		"html.commit", "json.commit",
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

func TestOutputGroupFailureCleanup(t *testing.T) {
	ctx := context.Background()
	t.Run("begin", func(t *testing.T) {
		var events []string
		group := NewOutputGroup(
			&eventOutput{name: "html", events: &events},
			&eventOutput{name: "json", events: &events, failBegin: true},
		)
		if err := group.Begin(ctx, &BuildPlan{}); err == nil {
			t.Fatal("begin returned no error")
		}
		want := []string{"html.begin", "json.begin", "html.abort"}
		if !reflect.DeepEqual(events, want) {
			t.Fatalf("events = %v, want %v", events, want)
		}
	})
	t.Run("write", func(t *testing.T) {
		var events []string
		group := NewOutputGroup(
			&eventOutput{name: "html", events: &events},
			&eventOutput{name: "json", events: &events, failPage: true},
		)
		if err := group.Begin(ctx, &BuildPlan{}); err != nil {
			t.Fatal(err)
		}
		if err := group.WritePage(ctx, &RenderedPage{}); err == nil {
			t.Fatal("write returned no error")
		}
		if err := group.Abort(ctx); err != nil {
			t.Fatal(err)
		}
		want := []string{"html.begin", "json.begin", "html.page", "json.page", "json.abort", "html.abort"}
		if !reflect.DeepEqual(events, want) {
			t.Fatalf("events = %v, want %v", events, want)
		}
	})
	t.Run("commit", func(t *testing.T) {
		var events []string
		group := NewOutputGroup(
			&eventOutput{name: "html", events: &events},
			&eventOutput{name: "json", events: &events, failCommit: true},
		)
		if err := group.Begin(ctx, &BuildPlan{}); err != nil {
			t.Fatal(err)
		}
		if err := group.Commit(ctx); err == nil {
			t.Fatal("commit returned no error")
		}
		if err := group.Abort(ctx); err != nil {
			t.Fatal(err)
		}
		want := []string{"html.begin", "json.begin", "html.commit", "json.commit", "json.abort", "html.abort"}
		if !reflect.DeepEqual(events, want) {
			t.Fatalf("events = %v, want %v", events, want)
		}
	})
}

func TestJSONManifestOutput(t *testing.T) {
	file := filepath.Join(t.TempDir(), "nested", "pages.json")
	output := NewJSONManifestOutput(file)
	ctx := context.Background()
	if err := output.Begin(ctx, &BuildPlan{}); err != nil {
		t.Fatal(err)
	}
	page := &Page{
		URLPath: "/hello/", OutputPath: "hello/index.html", Format: SourceMarkdown,
		Meta: map[string]any{"title": "Hello"},
	}
	if err := output.WritePage(ctx, &RenderedPage{Page: page, HTML: []byte("hello")}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("manifest published before commit: %v", err)
	}
	if err := output.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var records []struct {
		Path string         `json:"path"`
		Size int            `json:"size"`
		Meta map[string]any `json:"meta"`
	}
	if err := json.Unmarshal(data, &records); err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Path != "/hello/" || records[0].Size != 5 || records[0].Meta["title"] != "Hello" {
		t.Fatalf("manifest records = %#v", records)
	}
}

func TestJSONManifestAbortRemovesTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	output := NewJSONManifestOutput(filepath.Join(dir, "pages.json"))
	if err := output.Begin(context.Background(), &BuildPlan{}); err != nil {
		t.Fatal(err)
	}
	if err := output.Abort(context.Background()); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("abort left files: %v", entries)
	}
}
