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
  environment, including `go run` compilation overhead. This is a useful
  directional result, not a portable performance guarantee.

Rendering is still sequential. Serve mode still discovers the current page
list per request. There is no extension runtime, hook registry, module loader,
capability registry, multi-output fan-out, or incremental dependency graph yet.

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
      ({ page }) => {
        page.meta.draft = true;
      },
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

The MVP explicitly excludes `new` hooks, serve integration, custom CLI
commands, relative imports, external packages, generic DOM traversal, hook
dependency ordering, network and filesystem APIs, concurrency, and incremental
rebuilds.

The HTML output should remain enabled by default. Add a small fan-out output
that sends each ephemeral `RenderedPage` synchronously to HTML and manifest
outputs before releasing it. This proves that aggregate formats fit the output
contract without holding all rendered pages in memory.

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

Benchmark four generated sites at 100, 1,000, and 10,000 pages:

1. No configuration file: establishes the current build baseline.
2. An empty configuration file: measures runtime creation and script loading.
3. A no-op `page:document` hook: isolates one JS round trip per page.
4. Go-backed highlighting plus the JSON manifest: measures the representative
   extension workload and output fan-out.

Report wall time, pages per second, allocations, and peak resident memory.
Include pages with zero, one, and several code blocks so highlighting cost is
not confused with hook overhead. A useful initial acceptance target is that an
empty or no-op extension adds no more than roughly 10% to a 10,000-page build;
representative highlighting should scale with the number and size of code
blocks rather than the total site squared.

If the no-op callback is too expensive, do not immediately add concurrency.
First allow JavaScript to register a Go transformer once, so workers invoke Go
directly for each page. That alternative still preserves JavaScript
configuration while removing Sobek from the page hot loop.

## Implementation sequence

1. **Complete:** extract project and operation layers from the CLI without
   adding JavaScript extensions.
2. **Complete:** introduce lightweight `Page` and `BuildPlan` models and
   first-class HTML output while preserving current behavior.
3. Implement the proof-of-concept MVP above and benchmark it before expanding
   the public API.
4. If the experiment succeeds, replace the single script loader with the
   extension runtime, module resolver, and stable `defineProject()` API.
5. Route `new` through a structured request and hooks.
6. Generalize Go-backed capability registration beyond the MVP highlighter and
   manifest output.
7. Add SQLite output to validate transactional non-file-tree output.
8. Add external extension resolution, richer hook ordering, parallel
   rendering, and dependency tracking only as demonstrated needs arise.

Syntax highlighting tests page-level DOM transformation and fast Go callbacks.
SQLite tests aggregate, transactional, non-file-tree output. If both fit
naturally without special cases, the extension boundary is likely sound.
