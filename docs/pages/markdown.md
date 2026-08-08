<template ante:layout="base.html">

<script ante:meta type="application/json">
{
  "title": "Markdown pages",
  "weight": 4
}
</script>

<template ante:fill="main">

Status: implemented
(`parseMarkdown` in `markdown.go`; `.md` handling in `site.go`).

## Model

A `pages/**/*.md` file bearing `ante:layout` is a page
of CommonMark content.
It is rendered to HTML (goldmark) first,
then flows through the normal pipeline —
parse, `ante:layout` composition, directive walk —
exactly as an `.html` page would.
One without `ante:layout` is not a page:
it passes through verbatim, like any other opaque file
([/pages/non-page-example.md](/pages/non-page-example.md) is one).
Every page renders to an `index.html` in its own directory,
so `x.md` serves at `/x/` and `md/index.md` at `/md/`;
the `.md` file itself is never served.
Layouts stay `.html`.

All `ante:` machinery — layout/fill templates,
directive-bearing elements — is written as raw HTML
inside the markdown.
Goldmark passes raw HTML through verbatim
(pages are trusted input),
so directives survive to the walk untouched.

Frontmatter is a `<script ante:meta>` JSON element
in the page source, markdown or not
(see [sampleblog](/sampleblog/));
site-wide data comes from `data/`, as everywhere in antedom.

## CommonMark blank lines required

CommonMark (and therefore Goldmark) require a blank line after every block level open tag and before every block level close tag.
I would have liked to avoid this, but the implementation was too complex, at least for now.

## Examples

- **Inline HTML in paragraphs.**
  Raw inline HTML is an inline token in CommonMark;
  markdown keeps flowing around *and inside* the tags:

  ```markdown
  Version <code ante:text="v"></code> is *current*.
  ```

  Inline directives mid-paragraph work with no ceremony.

- **Code.** Fenced blocks and inline code spans containing tags
  (`` `<div>` ``) are escaped by goldmark, never parsed as elements.

- **Heading anchors.** Headings get slugified `id`s
  (`## Hello World` → `id="hello-world"`).

## Example site

These docs are the example site:
every page under `docs/pages/` — this file included —
is a markdown page wrapped in layout/fill templates,
composed into `docs/layout/base.html`
with data from `docs/data/site.json`.
Serve with `go run ./cmd/antedom -site docs`;
`TestDocsSite` builds it in the normal test run.

</template>

</template>
