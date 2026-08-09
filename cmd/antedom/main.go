// Command antedom builds or serves an antedom site: a directory
// holding pages/ (*.html page templates and *.md markdown pages —
// the files bearing ante:layout — everything else passes through),
// layout/ (layout templates named by ante:layout),
// and data/ (*.json files, in scope as data.<name>).
// "antedom build" renders the site to a directory and exits;
// "antedom serve" serves the site over HTTP, rendering per request.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

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

	root.AddCommand(buildCmd, serveCmd)

	if err := root.Execute(); err != nil {
		log.Fatal(err)
	}
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
