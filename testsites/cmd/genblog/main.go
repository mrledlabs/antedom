// Command genblog writes a synthetic blog-shaped antedom project for
// manual timing and serve-mode testing:
//
//	go run ./testsites/cmd/genblog -n 1000 -o /tmp/blog1k
//	time go run ./cmd/antedom build --site /tmp/blog1k -o /tmp/blog1k-out
//
// Timed builds are also available as benchmarks without leaving a
// directory behind: go test -bench BuildBlog -benchtime 1x
package main

import (
	"flag"
	"log"

	"github.com/mrled/antedom/testsites"
)

func main() {
	n := flag.Int("n", 1000, "number of posts")
	out := flag.String("o", "", "project directory to write (required)")
	directives := flag.Int("directives", 0, "ante:text spans per post")
	unique := flag.Bool("unique-exprs", false, "distinct expression source per span")
	loopItems := flag.Int("loop-items", 0, "ante:for items per post")
	scopeStatements := flag.Int("scope-statements", 0, "ante:scope statements per post")
	flag.Parse()
	if *out == "" {
		log.Fatal("-o is required")
	}
	options := testsites.BlogOptions{
		Directives:      *directives,
		UniqueExprs:     *unique,
		LoopItems:       *loopItems,
		ScopeStatements: *scopeStatements,
	}
	if err := testsites.BlogWithOptions(*out, *n, options); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote a %d-post blog to %s", *n, *out)
}
