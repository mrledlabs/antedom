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

## Development

For a serve loop that also restarts on Go source changes, install
[air](https://github.com/air-verse/air) and run it from the repo root:

```sh
go install github.com/air-verse/air@latest
air
```

It builds to `bin/$(go env GOOS)-$(go env GOARCH)/antedom` and re-runs
`serve --site docs` on any non-test `.go` change (see [.air.toml](.air.toml)).
The per-platform path keeps macOS and Linux/Docker builds from clobbering
each other. Content changes under `docs/` are handled by `serve`'s own
live reload and don't trigger a rebuild.
