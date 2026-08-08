# antedom docs

antedom renders valid-HTML templates:
logic in `ante:`-prefixed attributes,
expressions in JavaScript (sobek),
evaluated at build time before any DOM exists.
The directive reference lives in the package doc comment
(`antedom.go`); design docs live here.

- [Templating: layouts, slots, fills](templating.md) —
  base templates, inheritance, and content pages
  via `ante:layout` / `ante:slot` / `ante:fill`.
