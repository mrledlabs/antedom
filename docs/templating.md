# Templating: layouts, slots, fills

Status: designed, not yet implemented
(`partials` in the demo data's directive table).

## Model

A layout is a complete HTML document with holes;
a content page names its layout and fills the holes.
Both are valid HTML, logic in `ante:` attributes, as everywhere in antedom.

Three attributes:

- `ante:layout="<file>"` — on the root element of a content page
  (or of another layout), names the layout to fill,
  resolved in the site's `layout/` directory.
  A page without `ante:layout` renders standalone, as today.
- `ante:slot="<name>"` — layout side: marks a hole.
  The element's children are the fallback;
  a matching fill replaces them.
  The element itself always stays.
- `ante:fill="<name>"` — content side: on a top-level element of the page,
  grafted as-is into the children of the matching slot.

There is no slot-specific `<template>` behavior.
A `<template>` antedom processes is unwrapped (see below),
and the wrapper-vs-no-wrapper distinction falls out of that:
`<template ante:slot>` emits its filled children with no wrapper,
`<template ante:fill>` contributes sibling elements with no wrapper,
any other element keeps its tag in the output.

## `<template>` and the client

A literal `<template>` in served HTML is inert —
browsers render nothing and client JS clones its contents —
so pages must be able to ship one.
The rule (Vue's, translated):

- A `<template>` with any `ante:` attribute is antedom's:
  processed, then unwrapped.
  This covers grouping under `ante:if`/`ante:for`
  as well as `ante:slot`/`ante:fill`.
- A plain `<template>` is the client's:
  shipped verbatim, contents untouched
  (antedom does not descend into it).
- `ante:keep` processes the element's other directives
  and its contents, but keeps the element:
  for client-bound templates that need build-time logic,
  e.g. `<template ante:keep ante:if="...">`.

In one sentence:
a `<template>` you hand to antedom is consumed,
one you don't is yours,
and `ante:keep` hands it over without giving it up.

Note this refines the current engine,
which unwraps every `<template>` unconditionally;
the change must land before the slot work builds on it.

## Rendering

1. Resolve the `ante:layout` chain from the page up to a layout
   that declares none.
2. Fold outward-in: start from the base document,
   then apply each level's fills, the page's last.
   Each level fills whatever slots are still open in the accumulated tree,
   so a page can fill a base slot its intermediate layout never mentions.
   Unfilled slots keep their fallback children.
3. Run the normal directive walk once over the merged document.
   Directives inside fills evaluate here (so `ante:for` etc. just work),
   and every processed `<template>` unwraps
   (plain ones ship to the client — see below).

## Restricted contexts: head and tables

The HTML parser rebuilds the tree per spec before antedom sees it,
and it relocates elements it does not allow in context:
non-metadata elements are ejected from `<head>` into `<body>`,
and non-table content is foster-parented out in front of a `<table>`.
`<template>` is permitted in both, so in those contexts
the slot must be a `<template ante:slot>`;
elsewhere any suitable element works.
The same applies to fill content:
wrap context-sensitive fragments (`<td>`, `<li>`, head elements)
in `<template ante:fill>` so the parser leaves them intact.

`<title>` is RCDATA — markup inside it is text, never elements —
so the page title cannot be slotted.
It flows through data instead:
the layout writes `<title ante:text="...">`.

## Example

Site tree (no automatic lookup — a page names its layout explicitly):

```
layout/base.html
layout/hello.html
pages/index.html
pages/hello/index.html
```

`layout/base.html` — the document skeleton:

```html
<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title ante:text="data.site.title">site</title>
  <link rel="stylesheet" href="/style.css">
  <template ante:slot="head"></template>
</head>
<body>
  <nav><a href="/">home</a></nav>
  <main ante:slot="main">
    <p>nothing here yet</p>
  </main>
  <footer ante:text="`rendered at ${now}`">timestamp</footer>
</body>
</html>
```

`layout/hello.html` — an intermediate layout:
fills the base's `main`, declares its own `content` slot:

```html
<template ante:layout="base.html">
  <template ante:fill="main">
    <nav class="section"><a href="/hello/">hello index</a></nav>
    <article ante:slot="content">
      <p>pick a page</p>
    </article>
  </template>
</template>
```

`pages/hello/index.html` — a content page:

```html
<template ante:layout="hello.html">
  <template ante:fill="head">
    <link rel="stylesheet" href="style.css">
  </template>
  <template ante:fill="content">
    <h1 ante:text="data.site.title">title</h1>
    <ul>
      <li ante:for="d of data.demo.directives" ante:text="d.name">directive</li>
    </ul>
  </template>
</template>
```

Rendered output (abbreviated):

```html
<!DOCTYPE html>
<html><head>
  <meta charset="utf-8">
  <title>ellipsis</title>
  <link rel="stylesheet" href="/style.css">
  <link rel="stylesheet" href="style.css">
</head>
<body>
  <nav><a href="/">home</a></nav>
  <main>
    <nav class="section"><a href="/hello/">hello index</a></nav>
    <article>
      <h1>ellipsis</h1>
      <ul><li>ante:if</li><li>ante:for</li>…</ul>
    </article>
  </main>
  <footer>rendered at …</footer>
</body></html>
```

Tracing the markers:
`<main ante:slot="main">` kept its tag,
its fallback replaced by the intermediate's fill,
whose `<template>` wrapper unwrapped to nav + article siblings.
`<article ante:slot="content">` kept its tag,
children from the page.
`<template ante:slot="head">` unwrapped to just the page's `<link>`.
A page filling nothing would get every fallback.

`pages/index.html` can use `base.html` directly:

```html
<template ante:layout="base.html">
  <template ante:fill="main">
    <h1>antedom</h1>
    <p>…</p>
  </template>
</template>
```

## Non-goals (for now)

- No automatic layout lookup by directory (Hugo-style);
  a page names its layout explicitly.
- No validation of misplaced slots
  (e.g. a `<div ante:slot>` in `<head>` is silently ejected by the parser);
  a tokenizer-based lint could come later.
- No `<slot>` element or `slot=` attribute support:
  one spelling, the `ante:` attributes.
