<script ante:meta type="application/json">
{
  "title": "Pages",
  "weight": 3,
  "layout": "base.html"
}
</script>

Site pages are available via JavaScript. For example:

```html
 <nav>
    <a ante:for="p of pages"
       ante:href="p.path"
       ante:style="p.depth ? `--depth: ${p.depth}` : false"
       ante:aria-current="p.path === page.path ? 'page' : false"
       ante:text="p.title">page</a>
  </nav>
```

A page is any `*.md` or `*.html` file that names a layout —
either the sugar form, a `layout` key in its
`<script ante:meta type="application/json">` metadata
(this page is one; the body fills the layout's default slot),
or the explicit form, a `<template ante:layout="...">` element
containing `<template ante:fill="...">` elements
for the layout's slots (see [templating](/templating/)).

A `*.md` or `*.html` file that names no layout is treated as an opaque file ---
not a page, but the same as any other static file.

Here's an example md file that is passed through directly:
[non-page-example.md](non-page-example.md).

