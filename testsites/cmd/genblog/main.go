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

	"micahrl.com/antedom/testsites"
)

func main() {
	n := flag.Int("n", 1000, "number of posts")
	out := flag.String("o", "", "project directory to write (required)")
	flag.Parse()
	if *out == "" {
		log.Fatal("-o is required")
	}
	if err := testsites.Blog(*out, *n); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote a %d-post blog to %s", *n, *out)
}
