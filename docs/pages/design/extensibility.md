<script ante:meta type="application/json">
{
  "title": "Extensibility",
  "weight": 10,
  "layout": "base.html"
}
</script>

The extension system should be a small Go build kernel with a JavaScript
orchestration layer. JavaScript decides what happens; Go provides fast,
typed capabilities such as HTML rendering, syntax highlighting, file output,
and SQLite generation.

The system should be organized around operations, hooks, and artifacts rather
than callbacks added directly to the current implementation.

```text
CLI / embedding application
        │
        ▼
Operation: new | build | serve
        │
        ▼
Project loader
  - project configuration
  - JavaScript extensions
  - registered hooks, commands, outputs
        │
        ▼
Build kernel
  discover → load → transform → render → emit
                                      │
                         HTML directory / SQLite / custom output
```

Go owns filesystem traversal, parsing, layout composition, DOM rendering,
output transactions, concurrency, built-in implementations, and diagnostics.
JavaScript owns project policy, hook registration, lightweight transformations,
custom commands and scaffolding, and the configuration of Go capabilities.

## Implementation status

The first two foundation steps are implemented. This code is useful even if
antedom never ships project extensions.

- `Project` and `Operation` now form an application layer above `Site`.
  `build`, `serve`, and `new` all enter through that layer instead of keeping
  project behavior in the CLI.
- Operation contexts establish cancellation boundaries. Build cancellation is
  propagated through discovery, rendering, and output.
- A full build discovers pages and parses their metadata once into a
  lightweight `BuildPlan`. This removes the former quadratic behavior in which
  every rendered page rebuilt the complete page list.
- `Page` and `Asset` retain paths and metadata only. Source bytes, parsed DOMs,
  and rendered HTML have a bounded, per-page lifetime.
- Outputs are first-class transactional consumers. The existing directory
  build is implemented by `HTMLOutput` through `Begin`, `WritePage`,
  `WriteAsset`, `Commit`, and `Abort`.
- Existing `Site.Build` and CLI behavior remain compatible.
- The generated-site benchmark covers up to 10,000 posts. A manual synthetic
  10,001-page build completed in about two seconds in the development
  environment, including `go run` compilation overhead. In-process throughput
  is roughly flat from 100 to 10,000 pages (about 9,800 falling to about
  7,100 pages per second in one development environment). These are useful
  directional results, not portable performance guarantees.
- The per-page droop at 10,000 pages is a known residual O(n) cost: every
  render still receives the full page list (the `pages` scope array and the
  linear current-page lookup). It is harmless at this scale and is the next
  scaling cliff somewhere around 100,000 pages. It also belongs to the
  no-extension baseline, so the experiment below must not misattribute it to
  extension overhead.
- The first extension MVP slice is implemented for project builds: an optional
  `antedom.js`, one build-scoped Sobek runtime, an enforced
  `antedom.apiVersion(1)` compatibility gate, registration-order
  `page:document` hooks, deeply read-only host-backed page paths and metadata,
  and an opaque document handle. Configuration and hook errors include the
  extension file, hook, and page context.
- The opaque document handle now provides Go-backed syntax highlighting for
  literal `<pre><code class="language-*">` blocks. JavaScript selects the
  style and unknown-language policy, while DOM traversal, lexing, formatting,
  and replacement remain in Go. The highlighter is invoked through each
  JavaScript hook rather than registered once as a Go transform: the
  register-once fallback existed for the case where the per-page JS crossing
  proved too expensive, and at roughly 2 µs per no-op crossing it was not
  needed. Highlighted blocks are self-contained: the theme's base foreground
  and background are applied inline to the block's `<pre>`, so any theme
  stays readable whether the surrounding site renders light or dark.
  Class-based output with per-color-scheme palettes would need generated
  stylesheets and is deferred.

- Serve runs the same `page:document` hooks as build. A rendered request
  loads `antedom.js` afresh — matching serve's existing per-request page
  discovery — so extension edits take effect immediately, and every request
  gets its own goroutine-confined Sobek runtime (runtimes are not
  goroutine-safe, so concurrent requests must not share one). Hooks receive
  the same plan-backed page model as build, and a test asserts serve and
  build produce byte-identical page output. One consequence: hook state
  cannot span pages in serve the way it can within a single build, because
  each request is a fresh runtime.

- Builds always write the normal HTML tree and may fan each ephemeral
  `RenderedPage` out synchronously to additional outputs. JavaScript outputs
  now receive streaming `begin`, `page`, `asset`, and `end` callbacks plus a
  Go-backed artifact writer. They can produce arbitrary text, JSON, or byte
  files without retaining page DOMs or bodies. The original streaming JSON
  manifest remains as the `antedom.go.jsonManifest()` convenience output.

Rendering is still sequential. Serve mode still discovers the current page
list per request. The MVP extension is not used by `new`. There is
no module loader, general Go capability registry, or incremental dependency
graph yet.

## Outstanding MVP work

The generalized JavaScript output callback has been measured at 10,000 pages
(the results are recorded with the proof-of-concept below), and the
documentation site now configures a real JavaScript sitemap output. Page
text extraction remains intentionally basic concatenated rendered text; a
search implementation may reveal that it needs configurable exclusion of
navigation, scripts, styles, or other layout content.

## Project configuration

A project has one known entry point, such as `antedom.js`:

```js
import { defineProject } from "antedom";
import { html, sqlite, syntaxHighlight } from "antedom/builtins";

export default defineProject({
  apiVersion: 1,

  hooks: {
    "page:document": [
      syntaxHighlight({ theme: "github-dark" }),
    ],

    "new:page": [
      () => ({ meta: { draft: true } }),
    ],
  },

  outputs: {
    site: html({ directory: "public" }),
    search: sqlite({
      file: "public/search.sqlite",
      table: "pages",
    }),
  },
});
```

Hook callbacks receive read-only context objects and return patches: the
`new:page` handler above returns a metadata patch rather than mutating the
page, matching the hook semantics below.

Imports beginning with `antedom` are virtual modules supplied by the Go host.
A Go-backed function such as `syntaxHighlight()` returns an opaque host
function: JavaScript configures it, while the expensive document work remains
in Go.

Do not expose arbitrary Go values to JavaScript through reflection. The host
API should be deliberately small so types, errors, compatibility, and object
lifetimes remain under antedom's control.

## Operations and runtimes

Every invocation creates an operation:

```go
type Operation struct {
    Kind    OperationKind // New, Build, Serve
    Project *Project
    Context context.Context
    Hooks   *HookRegistry
}
```

Each operation gets one project-level Sobek runtime. It loads configuration
and extensions once, holds registered callbacks, lives until the operation
finishes, and is never called concurrently.

Template JavaScript remains separate. Templates should continue to use an
isolated runtime per page because template code can leak globals. This creates
two distinct programming models:

- Extension JavaScript controls an operation and its pipeline.
- Template JavaScript evaluates expressions for one page.

Sobek runtimes are not goroutine-safe. Go may perform page work concurrently,
but JavaScript hooks must be serialized through the extension runtime. A later
optimization may restrict some hooks to planning and convert their results into
immutable Go work. Go-backed transforms can run concurrently without passing
through JavaScript for each node.

## Page and build models

Discovery, metadata extraction, rendering, and output paths should communicate
through a stable page model:

```go
type Page struct {
    SourcePath string
    RelPath    string
    URLPath    string
    OutputPath string
    Format     SourceFormat
    Meta       map[string]any
}

type BuildPlan struct {
    Pages  []*Page
    Assets []*Asset
}
```

The implemented plan deliberately does not progressively retain source, DOM,
and output fields. Rendering creates a separate ephemeral value:

```go
type RenderedPage struct {
    Page     *Page
    Document *html.Node
    HTML     []byte
}
```

Hooks receive coherent domain objects rather than unrelated filenames and
maps, while outputs consume a stable interface rather than depending on the
HTML-directory implementation.

Building the page list once also avoids reparsing every page's metadata for
each rendered page.

## Hook lifecycle

Begin with a small, intentional lifecycle:

```text
project:load

new:prepare
new:page
new:written

build:prepare
pages:discovered

For each page:
  page:load
  page:document
  page:render
  page:rendered

build:rendered
output:prepare
output:complete
build:complete
```

Hook semantics must be explicit:

| Kind | Purpose | Return behavior |
| --- | --- | --- |
| Notification | Observe an event | Ignored |
| Transform | Change a value | Feeds the next handler |
| Decision | Select or skip work | A typed result controls the pipeline |
| Resource lifecycle | Open, write, or commit output | Errors abort and clean up |

For example, `page:document` is a waterfall transform while `build:complete`
is a notification. Avoid APIs that ambiguously permit either mutation or a
returned replacement. Across the JavaScript boundary, context objects should
be read-only and transforms should return validated patches or values.

Large DOM trees should not be exported as ordinary JavaScript objects and then
reconstructed. Expose constrained methods over opaque Go handles instead:

```js
export function transformPage(ctx) {
  ctx.document.query("pre code").forEach((node) => {
    ctx.go.highlight(node, { language: node.attr("class") });
  });
}
```

JavaScript callbacks must not retain Go objects valid only for the duration of
a call unless those objects are explicitly managed handles.

Every hook should have an ID even if initial ordering is registration order.
Dependency ordering can later support `before` and `after` constraints.

## Go-backed capabilities

Optimized Go features are registered capabilities:

```go
type Capability interface {
    Name() string
    Install(*sobek.Runtime, *HostAPI) error
}
```

Likely capability groups include:

- `antedom/render`: render, serialize, and parse HTML fragments.
- `antedom/highlight`: highlight strings or document code blocks.
- `antedom/sqlite`: create transactional outputs and insert records.
- `antedom/fs`: project-relative reads, writes, and globs.
- `antedom/markdown`: parse Markdown with antedom's Go engine.

Paths exposed to JavaScript should be project-relative by default. Go
capability failures become ordinary JavaScript exceptions while retaining the
underlying cause for CLI diagnostics.

## Outputs are first-class sinks

Custom formats should not be late hooks that inspect the generated `public/`
directory. They are direct consumers of the build:

```go
type Output interface {
    Begin(context.Context, *BuildPlan) error
    WritePage(context.Context, *RenderedPage) error
    WriteAsset(context.Context, *Asset) error
    Commit(context.Context) error
    Abort(context.Context) error
}
```

The HTML output writes page files and copied assets. A SQLite output inserts
page content and metadata. JSON and feed outputs can create aggregate files.
Several outputs may run during one build.

`Commit` and `Abort` provide the transaction boundary needed by SQLite and
other aggregate formats. When practical, outputs should write to temporary
destinations and atomically replace their final destinations after success.

A rendered page may retain both forms:

```go
type RenderedPage struct {
    Page     *Page
    Document *html.Node
    HTML     []byte
}
```

Outputs can declare which representation they need. A search database may use
text and metadata without needing serialized HTML.

## The `new` operation

`new` uses the same project runtime and a structured request:

```go
type NewRequest struct {
    Argument string
    Layout   string
    Kind     string
}

type NewPage struct {
    Destination string
    Meta        map[string]any
    Fills       []Fill
    Content     []byte
}
```

Its flow is:

```text
Parse CLI request
  → create the default NewPage in Go
  → run new:prepare and new:page hooks
  → validate the destination
  → write atomically
  → run new:written
```

This preserves the built-in scaffold while letting extensions select an
archetype, add metadata, change the destination, generate related files, or
delegate final rendering to a Go scaffolder.

Extension-defined CLI commands are separate from lifecycle hooks:

```js
export default defineProject({
  commands: {
    import: {
      arguments: "<source>",
      run(ctx, args) { /* ... */ },
    },
  },
});
```

Core command names remain reserved so extensions cannot silently replace
`build`, `serve`, or `new`. The CLI can construct extension commands after
loading the project configuration.

## Modules, dependencies, and compatibility

Separate project configuration, relative local modules, host modules, and
future external packages. Do not design a package registry initially. Start
with a resolver interface:

```go
type ModuleResolver interface {
    Resolve(importer, specifier string) (Module, error)
}
```

The first implementation only needs relative project files and built-in host
modules, possibly with a conventional project-local `extensions/` directory.

Every project declares an API version and loading fails early when that version
is unsupported. Extension interfaces will become compatibility commitments
more quickly than internal Go interfaces.

## Errors and diagnostics

Errors accumulate context as they cross layers:

```text
extension "syntax"
hook "page:document"
page "posts/example.md"
Go capability "highlight"
unsupported language "..."
```

JavaScript errors include source filenames and stack traces. Output errors
identify the output and lifecycle method. Cleanup failures should not hide the
original error.

## Security model

Sobek prevents extensions from loading native code directly, but it is not a
security sandbox after filesystem, network, SQLite, or Go callbacks are
exposed. Extensions should initially be documented as trusted project code.

Capability-scoped APIs still provide useful boundaries:

- Constrain filesystem access to project and output roots by default.
- Do not provide network access unless explicitly enabled.
- Propagate cancellation and deadlines through the operation context.
- Limit recursion and generated artifacts.
- Never expose Go reflection.
- Dispose of the runtime after its operation.

Supporting hostile third-party code should be treated as a separate future
project, not implied by running JavaScript in Sobek.

## Serve mode and dependencies

Serve mode distinguishes the long-lived server from each build generation:

- Load project configuration.
- Watch project files and imported extension modules.
- Create fresh operation/build state for each rebuild.
- Publish a generation only after it succeeds.
- Recreate the extension runtime when configuration code changes.

Today's MVP is a degenerate form of this: with no generations yet, each
rendered request is its own generation — it re-plans the site and reloads the
extension. When generations arrive, plan and extension loading move out of
the request path and are recreated per generation instead.

Extensions and capabilities should be able to report dependencies:

```js
ctx.dependencies.watch("data/products.json");
```

Go-backed capabilities report their dependencies automatically. This can grow
into a precise live-reload dependency graph.

## Extension proof-of-concept MVP

The broader design is larger than necessary to answer the two immediate
questions:

1. Is project-level JavaScript a pleasant way to configure custom behavior?
2. Is one Sobek callback per page fast enough, and can expensive work stay in
   Go without retaining every page DOM?

A smaller vertical slice can answer both before implementing modules, custom
commands, SQLite, dependency ordering, or incremental builds.

### Scope

Load one optional, trusted `antedom.js` file as a plain script for `build` only.
Do not implement ES modules or imports yet. Install one global host object:

```js
antedom.apiVersion(1);

antedom.on("page:document", (page) => {
  if (page.meta.params?.highlight !== false) {
    page.document.highlight({ style: "github" });
  }
});

antedom.output("manifest", antedom.go.jsonManifest({
  file: "pages.json",
}));
```

Support only:

- One extension runtime per build.
- One hook, `page:document`, called after composition and before serialization.
- Read-only page paths and metadata exported to JavaScript.
- An opaque document handle with one Go-backed operation, initially syntax
  highlighting.
- One optional Go-backed aggregate output, preferably a JSON manifest rather
  than SQLite. JSON tests output fan-out and lifecycle without first choosing a
  SQLite driver or schema API.
- Registration order only, with useful filenames, hook names, page paths, JS
  stacks, and Go causes in errors.

`page:document` runs after Markdown parsing and layout composition but before
antedom directives and shortcode expansion. Automatic highlighting therefore
applies to literal source and Markdown fenced-code blocks only. Content later
introduced or replaced by directive evaluation (`ante:html`, `ante:text`) or
shortcode expansion is not rescanned automatically; the code that generates
such content must use an explicit highlighting operation. A string/fragment
highlighting capability can be added when a concrete generator needs it. This
keeps the phase predictable and avoids adding a speculative final-output DOM
hook.

Repeated `highlight` calls on the same document are possible: two registered
hooks may each call it, or one hook may call it more than once. A highlighted
block keeps its `language-*` class, so a later call matches the block again,
re-tokenizes its text content, replaces the earlier token spans, and counts
the block again. Text content round-trips, so output remains correct, but the
work is repeated, the last call's style wins, and returned counts include
already-highlighted blocks. The MVP deliberately does not detect or skip
previously highlighted blocks; if repeated calls become a real pattern, a
marker class or handle-level memo can address it later.

`antedom.apiVersion` is a call rather than a declaration in the script form,
so the loader must enforce it: if `antedom.js` exists but never calls
`apiVersion`, or calls it after registering a hook or output, the build fails.
The compatibility gate cannot be skipped silently.

The MVP explicitly excludes `new` hooks, custom CLI
commands, relative imports, external packages, generic DOM traversal, hook
dependency ordering, network and filesystem APIs, concurrency, and incremental
rebuilds. Serve integration was added after the initial slice: serve runs the
same hook per rendered request with a per-request extension load, as described
in the implementation status.

The HTML output remains enabled by default. `OutputGroup` sends each ephemeral
`RenderedPage` synchronously to HTML and configured outputs before releasing
it. A general JavaScript output can stream one or several artifacts:

```js
antedom.output("sitemap", {
  file: "sitemap.xml",

  begin(build, output) {
    output.write('<?xml version="1.0"?>\n<urlset>\n');
  },

  page(page, output) {
    output.write(`<url><loc>${page.urlPath}</loc></url>\n`);
  },

  end(build, output) {
    output.write("</urlset>\n");
    output.open("robots.txt").write("User-agent: *\n");
  },
});
```

The configuration has one required default `file` and optional `begin`,
`page`, `asset`, and `end` functions. `begin` and `end` receive a read-only
build summary containing page and asset counts. `page` receives read-only
paths, URL, source format, rendered byte size, metadata, and lazily converted
`html` and `text`; it never receives the DOM. `asset` receives read-only paths.
The writer provides `write(string | Uint8Array)`, `writeJSON(value)`, and
`open(relativePath)`. A child writer returned by `open` writes that artifact
but cannot open further files.

Paths are confined to the output directory and cannot collide with planned
pages, assets, or another registered output. Every artifact streams to a
temporary file and is renamed only on commit. The JSON manifest convenience
avoids a JavaScript callback per page:

```js
antedom.output("manifest", antedom.go.jsonManifest({
  file: "pages.json",
}));
```

A three-sample benchmark (three builds per sample) on Linux/arm64 compared the
Go manifest with a JavaScript-generated manifest whose output is asserted
byte-for-byte identical. At 10,001 pages, Go averaged 1.549 s and JavaScript
1.610 s: the generalized path added about 61 ms per build, 6.1 microseconds per
page, or 3.9% wall time. JavaScript allocated about 44 MB and 800,000 objects
more per build (roughly 4.4 KB and 80 allocations per page), largely from
constructing JavaScript objects and `JSON.stringify`. Wall time is acceptable
for the MVP, while allocation reduction is the clearest future optimization.

`OutputGroup` begins children in registration order,
forwards pages and assets in that order, and aborts begun children in reverse
order after a failure. This is coordinated best-effort cleanup, not an atomic
transaction across unrelated destinations: the current HTML output writes
directly to its final tree and cannot undo files already written, and a later
child can still fail after an earlier child commits. True cross-output atomic
publication would require a separate prepare/publish phase.

### Why this is enough

The JavaScript callback proves project policy and the Sobek-to-Go call path.
The highlighter proves that expensive work can be configured in JavaScript but
executed against the live Go DOM without exporting and reconstructing the
tree. The manifest proves multiple and aggregate outputs. Together they cross
every important boundary in the proposed architecture while avoiding most of
the permanent API surface.

`antedom.js` is intentionally a script in the MVP. Its registration calls can
later become the implementation behind `defineProject()` and virtual modules;
the MVP syntax should therefore be documented as experimental.

### Performance experiment

The experiment needs two small `testsites` extensions first: generated posts
with zero, one, and several fenced code blocks, and an option to write an
`antedom.js` into the generated project.

Benchmark four configurations of generated sites at 100, 1,000, and 10,000
pages:

1. No configuration file: establishes the current build baseline.
2. A minimal `antedom.apiVersion(1)` configuration: measures runtime creation
   and script loading. A truly empty configuration is invalid.
3. A no-op `page:document` hook: isolates one JS round trip per page.
4. Go-backed highlighting plus the JSON manifest: measures the representative
   extension workload and output fan-out.

The no-op hook must receive the same read-only page export as a real hook,
so configuration 3 prices the full boundary crossing and configuration 4 adds
only the workload.

Report wall time, pages per second, allocations, and peak resident memory.
Include pages with zero, one, and several code blocks so highlighting cost is
not confused with hook overhead. A useful initial acceptance target is that an
empty or no-op extension adds no more than roughly 10% to a 10,000-page build;
representative highlighting should scale with the number and size of code
blocks rather than the total site squared.

The 10% figure is a gate, not the durable metric: it is relative to today's
sequential baseline, and later work such as parallel rendering will shrink
that baseline until an unchanged absolute JavaScript cost busts the
percentage. Record the absolute per-page cost of the no-op round trip
(microseconds per page) as the number to track across builds of antedom
itself.

If the no-op callback is too expensive, do not immediately add concurrency.
First allow JavaScript to register a Go transformer once, so workers invoke Go
directly for each page. That alternative still preserves JavaScript
configuration while removing Sobek from the page hot loop.

### Initial extension benchmark

The first implementation copied and deeply froze a new JavaScript object graph
for every page. At 10,000 posts, its no-op hook took about 1.59 seconds,
allocated 924 MB cumulatively, and added about 15% over baseline. This missed
the provisional gate and showed that page-export construction, rather than
runtime loading, was the immediate problem.

The optimized implementation exposes the same immutable inputs through Sobek
read-only dynamic objects and arrays. It rejects writes and deletes at every
level without copying and recursively freezing the complete graph. Three
one-iteration ARM64 development runs produced these average directional
10,000-post results:

| Configuration | Time | Pages/second | Time/page | Allocated bytes |
| --- | ---: | ---: | ---: | ---: |
| No `antedom.js` | 1.425 s | 7,018 | 142.5 µs | 772 MB |
| Minimal `apiVersion(1)` | 1.435 s | 6,970 | 143.5 µs | 775 MB |
| One no-op page hook | 1.445 s | 6,924 | 144.5 µs | 794 MB |

The optimized complete no-op boundary is about 1.4% slower than baseline in
these runs, or roughly 2 µs/page, and is inside the provisional 10% gate. Its
cumulative allocation is about 22 MB above baseline, compared with roughly
152 MB above baseline for the copied/frozen representation. Keep tracking the
absolute per-page cost as the more durable metric.

## Implementation sequence

1. **Complete:** extract project and operation layers from the CLI without
   adding JavaScript extensions.
2. **Complete:** introduce lightweight `Page` and `BuildPlan` models and
   first-class HTML output while preserving current behavior.
3. **Complete:** the project script, API gate, page hook, Go-backed highlighter,
   synchronous output fan-out, and streaming JSON manifest are implemented and
   benchmarkable. The optimized per-page JS boundary is inside the provisional
   performance gate.
4. If the experiment succeeds, replace the single script loader with the
   extension runtime, module resolver, and stable `defineProject()` API.
   Include a build-level data hook that injects values into template scope —
   likely the most common real-world extension, deliberately absent from the
   MVP because it exercises no risky boundary. Page-local data no longer
   needs it: a page reads its own bundle through the core
   [page assets](/design/assets/) feature; the hook remains the design for
   site-wide and computed data.
5. Route `new` through a structured request and hooks.
6. Generalize Go-backed capability registration beyond the MVP highlighter and
   manifest output.
7. Add SQLite output to validate transactional non-file-tree output.
8. Add external extension resolution, richer hook ordering, parallel
   rendering, and dependency tracking only as demonstrated needs arise.

Syntax highlighting tests page-level DOM transformation and fast Go callbacks.
SQLite tests aggregate, transactional, non-file-tree output. If both fit
naturally without special cases, the extension boundary is likely sound.
