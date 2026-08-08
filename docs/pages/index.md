<template ante:layout="base.html">

<script ante:meta type="application/json">
{
  "title": "antedom",
  "weight": 1
}
</script>

<template ante:fill="main">

antedom renders valid-HTML templates:
logic in `ante:`-prefixed attributes,
expressions in JavaScript (sobek),
evaluated at build time before any DOM exists.
The directive reference lives in the package doc comment
(`antedom.go`); design docs live here.

These docs are themselves an antedom site —
markdown pages, composed into `layout/base.html`,
served with `go run ./cmd/antedom -site docs`.

- [Templating: layouts, slots, fills](templating/) —
  base templates, inheritance, and content pages
  via `ante:layout` / `ante:slot` / `ante:fill`.
- [Markdown pages](markdown/) —
  `pages/**/*.md` as CommonMark content,
  `ante:` machinery as raw HTML, layouts and directives unchanged.
- [sampleblog](sampleblog/) —
  example posts ordered by `date` from page metadata,
  a `<script ante:meta>` element in the page source
  (the docs pages order themselves with `weight` the same way).
- [Directive demo](demo/) —
  a plain-HTML page in the same site,
  exercising <code ante:text="data.site.directives.length"></code> directives
  from `data/site.json` (that count is one of them, inline in this markdown).

</template>

</template>
