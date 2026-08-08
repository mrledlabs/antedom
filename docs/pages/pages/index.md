<template ante:layout="base.html">

<script ante:meta type="application/json">
{
  "title": "Pages",
  "weight": 3
}
</script>

<template ante:fill="main">

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

A page is any `*.md` or `*.html` file that contains a `<template ante:layout="...">` element with content inside it.
Typically this will include an instance of `<script ante:meta type="application/json">` for page metadata
and at least one `<template ante:fill="...">` to provide content for different slots in the layout.

A `*.md` or `*.html` file that lacks a `<template ante:layout="...">` element is treated as an opaque file ---
not a page, but the same as any other static file.

Here's an example md file that is passed through directly:
[non-page-example.md](non-page-example.md).

</template>

</template>
