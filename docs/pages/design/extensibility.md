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
    URLPath    string
    OutputPath string
    Format     SourceFormat

    Meta       map[string]any
    Source     []byte
    Document   *html.Node
    Rendered   []byte
}

type BuildPlan struct {
    Pages  []*Page
    Assets []*Asset
}
```

The pipeline progressively populates these fields. Hooks then receive coherent
domain objects rather than unrelated filenames and maps, and outputs consume a
stable result rather than depending on the HTML-directory implementation.

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
    Data     map[string]any
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

## Implementation sequence

1. Extract project and operation layers from the current CLI without adding
   JavaScript extensions.
2. Introduce `Page`, `BuildPlan`, and first-class HTML output while preserving
   current behavior.
3. Add the extension runtime, module resolver, API version, and a few build
   hooks.
4. Route `new` through a structured request and hooks.
5. Add Go-backed capability registration.
6. Implement syntax highlighting end to end.
7. Add JSON or SQLite output to validate the output abstraction.
8. Add external extension resolution and richer hook ordering only after those
   examples work.

Syntax highlighting tests page-level DOM transformation and fast Go callbacks.
SQLite tests aggregate, transactional, non-file-tree output. If both fit
naturally without special cases, the extension boundary is likely sound.
