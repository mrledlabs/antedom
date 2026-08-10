package antedom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// OutputGroup sends one build synchronously to several outputs. Pages remain
// ephemeral: every child consumes a RenderedPage before BuildWith releases it.
type OutputGroup struct {
	Outputs []Output
	begun   int
	started bool
	done    bool
}

func NewOutputGroup(outputs ...Output) *OutputGroup {
	return &OutputGroup{Outputs: outputs}
}

func (g *OutputGroup) Begin(ctx context.Context, plan *BuildPlan) error {
	if g.started {
		return fmt.Errorf("output group has already been used")
	}
	g.started = true
	for i, output := range g.Outputs {
		if output == nil {
			_ = g.abortBegun(context.WithoutCancel(ctx))
			g.done = true
			return fmt.Errorf("output %d: nil output", i)
		}
		if err := output.Begin(ctx, plan); err != nil {
			abortErr := g.abortBegun(context.WithoutCancel(ctx))
			g.done = true
			return errors.Join(fmt.Errorf("output %d begin: %w", i, err), abortErr)
		}
		g.begun++
	}
	return nil
}

func (g *OutputGroup) WritePage(ctx context.Context, page *RenderedPage) error {
	if !g.started || g.done {
		return fmt.Errorf("output group is not active")
	}
	for i := 0; i < g.begun; i++ {
		if err := g.Outputs[i].WritePage(ctx, page); err != nil {
			return fmt.Errorf("output %d write page: %w", i, err)
		}
	}
	return nil
}

func (g *OutputGroup) WriteAsset(ctx context.Context, asset *Asset) error {
	if !g.started || g.done {
		return fmt.Errorf("output group is not active")
	}
	for i := 0; i < g.begun; i++ {
		if err := g.Outputs[i].WriteAsset(ctx, asset); err != nil {
			return fmt.Errorf("output %d write asset: %w", i, err)
		}
	}
	return nil
}

func (g *OutputGroup) Commit(ctx context.Context) error {
	if !g.started || g.done {
		return fmt.Errorf("output group is not active")
	}
	for i := 0; i < g.begun; i++ {
		if err := g.Outputs[i].Commit(ctx); err != nil {
			return fmt.Errorf("output %d commit: %w", i, err)
		}
	}
	g.done = true
	return nil
}

func (g *OutputGroup) Abort(ctx context.Context) error {
	if !g.started || g.done {
		return nil
	}
	err := g.abortBegun(ctx)
	g.done = true
	return err
}

func (g *OutputGroup) abortBegun(ctx context.Context) error {
	var errs []error
	for i := g.begun - 1; i >= 0; i-- {
		if err := g.Outputs[i].Abort(ctx); err != nil {
			errs = append(errs, fmt.Errorf("output %d abort: %w", i, err))
		}
	}
	g.begun = 0
	return errors.Join(errs...)
}

// JSONManifestOutput streams one metadata record per rendered page into a
// JSON array and atomically publishes it on Commit. It never retains a page,
// document, or rendered body.
type JSONManifestOutput struct {
	File       string
	OutputPath string // optional slash-separated path for collision checks

	temp     *os.File
	tempPath string
	first    bool
}

func NewJSONManifestOutput(file string) *JSONManifestOutput {
	return &JSONManifestOutput{File: file}
}

func (o *JSONManifestOutput) Begin(ctx context.Context, plan *BuildPlan) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if o.temp != nil {
		return fmt.Errorf("JSON manifest output has already begun")
	}
	if o.OutputPath != "" {
		for _, page := range plan.Pages {
			if page.OutputPath == o.OutputPath {
				return fmt.Errorf("JSON manifest output %q conflicts with page %s", o.OutputPath, page.RelPath)
			}
		}
		for _, asset := range plan.Assets {
			if asset.OutputPath == o.OutputPath {
				return fmt.Errorf("JSON manifest output %q conflicts with asset %s", o.OutputPath, asset.RelPath)
			}
		}
	}
	if err := os.MkdirAll(filepath.Dir(o.File), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(o.File), "."+filepath.Base(o.File)+".tmp-*")
	if err != nil {
		return err
	}
	o.temp, o.tempPath, o.first = temp, temp.Name(), true
	if _, err := o.temp.WriteString("[\n"); err != nil {
		_ = o.Abort(context.WithoutCancel(ctx))
		return err
	}
	return nil
}

func (o *JSONManifestOutput) WritePage(ctx context.Context, page *RenderedPage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if o.temp == nil {
		return fmt.Errorf("JSON manifest output has not begun")
	}
	if !o.first {
		if _, err := o.temp.WriteString(",\n"); err != nil {
			return err
		}
	}
	o.first = false
	record := struct {
		Path       string         `json:"path"`
		OutputPath string         `json:"outputPath"`
		Format     SourceFormat   `json:"format"`
		Size       int            `json:"size"`
		Meta       map[string]any `json:"meta"`
	}{
		Path:       page.Page.URLPath,
		OutputPath: page.Page.OutputPath,
		Format:     page.Page.Format,
		Size:       len(page.HTML),
		Meta:       page.Page.Meta,
	}
	return json.NewEncoder(o.temp).Encode(record)
}

func (o *JSONManifestOutput) WriteAsset(context.Context, *Asset) error { return nil }

func (o *JSONManifestOutput) Commit(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if o.temp == nil {
		return fmt.Errorf("JSON manifest output has not begun")
	}
	if _, err := o.temp.WriteString("]\n"); err != nil {
		return err
	}
	if err := o.temp.Sync(); err != nil {
		return err
	}
	if err := o.temp.Close(); err != nil {
		return err
	}
	o.temp = nil
	if err := os.Rename(o.tempPath, o.File); err != nil {
		return err
	}
	o.tempPath = ""
	return nil
}

func (o *JSONManifestOutput) Abort(context.Context) error {
	var closeErr error
	if o.temp != nil {
		closeErr = o.temp.Close()
		o.temp = nil
	}
	if o.tempPath == "" {
		return closeErr
	}
	removeErr := os.Remove(o.tempPath)
	o.tempPath = ""
	if os.IsNotExist(removeErr) {
		removeErr = nil
	}
	return errors.Join(closeErr, removeErr)
}
