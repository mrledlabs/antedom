# antedom

Valid-HTML templating: logic in `ante:` attributes, expressions in
JavaScript, evaluated at build time before any DOM exists.
Docs in [docs/pages/](docs/pages/) — themselves an antedom site.

```sh
go run ./cmd/antedom serve --site docs         # serve the docs site on 127.0.0.1:35481, live-reloading on change
go run ./cmd/antedom build --site docs -o out  # render it to out/
go run ./cmd/antedom new posts/hello --site docs   # scaffold pages/posts/hello.md
go test ./...
```
