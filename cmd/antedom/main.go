// Command antedom builds or serves an antedom site: a directory
// holding pages/ (*.html page templates and *.md markdown pages —
// the files bearing ante:layout — everything else passes through),
// layout/ (layout templates named by ante:layout),
// and data/ (*.json files, in scope as data.<name>).
// "antedom build" renders the site to a directory and exits;
// "antedom serve" serves the site over HTTP, rendering per request.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/spf13/cobra"
	"golang.org/x/net/html"

	"micahrl.com/antedom"
)

func main() {
	var siteDir string

	root := &cobra.Command{
		Use:           "antedom",
		Short:         "Build or serve an antedom site",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&siteDir, "site", ".", "site directory holding pages/, layout/, and data/")

	var outDir string
	buildCmd := &cobra.Command{
		Use:   "build",
		Short: "Render the site to a directory and exit",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			pages, err := newSite(siteDir).Build(outDir)
			if err != nil {
				return err
			}
			log.Printf("built %d pages into %s", pages, outDir)
			return nil
		},
	}
	buildCmd.Flags().StringVarP(&outDir, "out", "o", "public", "output directory")

	var listen string
	var reload bool
	serveCmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve the site over HTTP, rendering per request",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			handler := http.Handler(newSite(siteDir).Handler())
			if reload {
				handler = antedom.LiveReload(handler, siteDir)
			}
			log.Printf("antedom serving %s on %s", siteDir, listen)
			return http.ListenAndServe(listen, handler)
		},
	}
	serveCmd.Flags().StringVar(&listen, "listen", "127.0.0.1:35481", "listen address")
	serveCmd.Flags().BoolVar(&reload, "reload", true, "reload browsers when site files change")

	var layout string
	newCmd := &cobra.Command{
		Use:   "new <path>",
		Short: "Create a new page under pages/",
		Long: "Create a new page at <path>, relative to the site's pages/ directory.\n" +
			"A path without an extension gets .md appended.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dest, err := newPage(siteDir, layout, args[0])
			if err != nil {
				return err
			}
			log.Printf("created %s", dest)
			return nil
		},
	}
	newCmd.Flags().StringVarP(&layout, "layout", "l", "base.html", "layout for the new page")

	root.AddCommand(buildCmd, serveCmd, newCmd)

	if err := root.Execute(); err != nil {
		log.Fatal(err)
	}
}

// newPage scaffolds a page at rel (relative to <siteDir>/pages/)
// wrapping the given layout, and returns the path it wrote.
func newPage(siteDir, layout, rel string) (string, error) {
	if filepath.Ext(rel) == "" {
		rel += ".md"
	}
	layoutPath := filepath.Join(siteDir, "layout", layout)
	if _, err := os.Stat(layoutPath); err != nil {
		return "", fmt.Errorf("layout %s: %w", layout, err)
	}
	dest := filepath.Join(siteDir, "pages", rel)
	if _, err := os.Stat(dest); err == nil {
		return "", fmt.Errorf("%s already exists", dest)
	}
	title, _ := json.Marshal(pageTitle(rel))
	var fills strings.Builder
	for _, slot := range openSlots(siteDir, layoutPath) {
		fmt.Fprintf(&fills, "\n<template ante:fill=%q>\n\n</template>\n", slot)
	}
	src := fmt.Sprintf(`<template ante:layout=%q>

<script ante:meta type="application/json">
{
  "title": %s,
  "date": %q
}
</script>
%s
</template>
`, layout, title, time.Now().Format("2006-01-02"), fills.String())
	if err := os.MkdirAll(filepath.Dir(dest), 0o777); err != nil {
		return "", err
	}
	return dest, os.WriteFile(dest, []byte(src), 0o666)
}

// pageTitle derives a starting title from the page path:
// the file name (or, for index pages, the directory name)
// with dashes and underscores as spaces, first letter capitalized.
func pageTitle(rel string) string {
	name := strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
	if name == "index" && filepath.Dir(rel) != "." {
		name = filepath.Base(filepath.Dir(rel))
	}
	name = strings.NewReplacer("-", " ", "_", " ").Replace(name)
	r := []rune(name)
	if len(r) > 0 {
		r[0] = unicode.ToUpper(r[0])
	}
	return string(r)
}

// openSlots returns the slot names a page using the layout can fill,
// in document order: the layout's chain is composed, and slots an
// inner layout already fills are excluded (their single child bears
// ante:fill). ["main"] if the chain can't be read.
func openSlots(siteDir, layoutPath string) []string {
	load := func(name string) (*html.Node, error) {
		src, err := os.ReadFile(filepath.Join(siteDir, "layout", name))
		if err != nil {
			return nil, err
		}
		return html.Parse(bytes.NewReader(src))
	}
	doc, err := load(filepath.Base(layoutPath))
	if err == nil {
		doc, err = antedom.Compose(doc, load)
	}
	if err != nil {
		return []string{"main"}
	}
	var slots []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if slot := nodeAttr(n, antedom.Prefix+"slot"); slot != "" {
			if fill := n.FirstChild; fill == nil || nodeAttr(fill, antedom.Prefix+"fill") == "" {
				slots = append(slots, slot)
				return // fallback content; gone once the slot is filled
			}
			// filled by an inner layout — its fill may expose nested slots
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return slots
}

func nodeAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func newSite(siteDir string) *antedom.Site {
	return &antedom.Site{
		Pages:  filepath.Join(siteDir, "pages"),
		Layout: filepath.Join(siteDir, "layout"),
		Data:   dataFunc(filepath.Join(siteDir, "data")),
	}
}

// dataFunc assembles the page scope: each data/*.json file under
// data.<name>, plus the render timestamp as now.
func dataFunc(dir string) func() (map[string]any, error) {
	return func() (map[string]any, error) {
		data := map[string]any{}
		files, err := filepath.Glob(filepath.Join(dir, "*.json"))
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			src, err := os.ReadFile(f)
			if err != nil {
				return nil, err
			}
			var v any
			if err := json.Unmarshal(src, &v); err != nil {
				return nil, fmt.Errorf("%s: %w", f, err)
			}
			data[strings.TrimSuffix(filepath.Base(f), ".json")] = v
		}
		return map[string]any{
			"now":  time.Now().Format(time.RFC3339),
			"data": data,
		}, nil
	}
}
