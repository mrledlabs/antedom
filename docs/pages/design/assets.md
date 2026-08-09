<script ante:meta type="application/json">
{
  "title": "Page assets",
  "weight": 15,
  "layout": "base.html"
}
</script>

A page can see the opaque files in its own source directory — its
bundle — from template scope as `page.assets`. This is antedom's
answer to Hugo's page bundles, built on a primitive antedom already
has: opaque files are discovered, copied verbatim, and served at their
own paths, so the only addition is that the page next to them can read
them at build time. It is also the web-native instinct: an HTML page
already references sibling files by relative URL; `page.assets`
extends the same relationship to build time.

The docs benchmarks page is the motivating case and the dogfood: raw
benchmark recordings sit next to
[design/benchmarks/index.md](/design/benchmarks/), which parses and
tabulates them with ordinary page JavaScript. No repo-specific tooling
feeds the docs site; it stays a vanilla antedom project.

## Scope API

```js
page.assets   // [{name, url, text()}, ...] in name order
```

- `name` — the file's base name.
- `url` — its site URL (opaque files ship at their source-relative
  paths).
- `text()` — the contents as a string. The name deliberately echoes
  the Fetch API's `Response.text()`, with one documented deviation: it
  is synchronous, because template evaluation is.

There are no unmarshal helpers. `JSON.parse` and page JavaScript do
the parsing; that is the point of JavaScript templating.

## Decisions

- **No leaf/branch distinction.** Every page sees the opaque files of
  its source directory, non-recursive. Sibling pages in one directory
  share a bundle. The aim is to get away without Hugo's leaf/branch
  split indefinitely, not just for a first version.
- **Assets are per-page, not global.** The `pages` global does not
  carry asset lists, deliberately and permanently: every page's scope
  already holds the full page list, and multiplying it by asset
  metadata would bloat every render for a feature only the owning page
  needs.
- **Reads are lazy.** Listing a bundle reads nothing; `text()` reads
  on first call, at most once per render. Large files can sit in a
  bundle and flow to the output as streamed copies without ever being
  spooled into memory. A read failure is a thrown JS exception, which
  fails the render with the page's context.
- **Text now, bytes later.** `text()` assumes UTF-8. A `bytes()`
  returning `Uint8Array` is the expected extension when binary
  processing matters; nothing in the model changes for it.

## The purity trade

Template JavaScript was previously pure: a bare runtime with no host
functions. `text()` is the first build-time I/O exposed to templates,
so its constraints are the design:

- Read-only.
- Only files already discovered as the page's own bundle — there is no
  path-taking API at all, so there is nothing to sanitize and no
  traversal to prevent.
- Opaque files only: page sources and layouts are not assets.
- Memoized per render; serve mode re-reads per request, so edits to
  bundle files appear on reload like any other source change.

Templates remain trusted project code, exactly as the
[extensibility](/design/extensibility/) security model states.

## Implementation notes

Discovery already existed: the build plan records every opaque file
with its paths, and a page's bundle is a filter of that list by source
directory. The scope objects are built per render with closure-backed
`text()` functions; Sobek binds a Go `func() (string, error)` as a JS
function that throws the error, so failures surface through the normal
expression-error path with page context. Both the build and serve
paths pass through the same render entry point, so the feature behaves
identically in `build` and `serve`.

Page-local data files partially cover the extension roadmap's
build-level data hook: "put `products.json` next to the page" now
needs no extension. The hook remains the design for site-wide and
computed data.
