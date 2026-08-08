// Command antedom builds or serves an antedom site: a directory
// holding pages/ (page templates, non-HTML files pass through)
// and data/ (*.json files, in scope as data.<name>).
// -build renders the site to a directory and exits;
// otherwise it serves the site over HTTP, rendering per request.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"micahrl.com/antedom"
)

func main() {
	siteDir := flag.String("site", ".", "site directory holding pages/ and data/")
	listen := flag.String("listen", "127.0.0.1:35481", "listen address (serve mode)")
	build := flag.String("build", "", "render the site to this directory and exit")
	flag.Parse()

	site := &antedom.Site{
		Pages: filepath.Join(*siteDir, "pages"),
		Data:  dataFunc(filepath.Join(*siteDir, "data")),
	}
	if *build != "" {
		pages, err := site.Build(*build)
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("built %d pages into %s", pages, *build)
		return
	}
	log.Printf("antedom serving %s on %s", *siteDir, *listen)
	log.Fatal(http.ListenAndServe(*listen, site.Handler()))
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
