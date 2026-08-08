<template ante:layout="base.html">

<template ante:fill="main">

# antedom docs

antedom renders valid-HTML templates:
logic in `ante:`-prefixed attributes,
expressions in JavaScript (sobek),
evaluated at build time before any DOM exists.
The directive reference lives in the package doc comment
(`antedom.go`); design docs live here.

These docs are themselves an antedom site —
markdown pages, composed into `layout/base.html`,
served with `go run ./cmd/antedom -site docs`.

- [Templating: layouts, slots, fills](templating.html) —
  base templates, inheritance, and content pages
  via `ante:layout` / `ante:slot` / `ante:fill`.
- [Markdown pages](markdown.html) —
  `pages/**/*.md` as CommonMark content,
  `ante:` machinery as raw HTML, layouts and directives unchanged.
- [Directive demo](demo/) —
  a plain-HTML page in the same site,
  exercising <code ante:text="data.site.directives.length"></code> directives
  from `data/site.json` (that count is one of them, inline in this markdown).

</template>

</template>
