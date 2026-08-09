<template ante:layout="base.html">

<script ante:meta type="application/json">
{
  "title": "Shortcodes",
  "weight": 5
}
</script>

<template ante:fill="main">



## Model

A shortcode is an *inert web component*:
an element that looks like a custom element,
but is expanded once, at build time —
no client JS, no registry, nothing ships.
It is antedom's answer to Hugo's shortcodes,
and all you need to know to write one is HTML and JS,
like everywhere else in antedom.

- **Use site:** any element named `shortcode-<name>`,
  in a page (markdown or HTML), a layout, or a fill.
- **Template:** `layout/shortcode/<name>.html`,
  an HTML fragment that replaces the element.
  It is ordinary antedom HTML:
  directives, `<template>` unwrapping, scope scripts,
  and nested shortcodes
  all work inside it (nesting caps at 10 deep, cutting cycles).

The template evaluates in a child scope of the page's,
so page data is visible, plus:

- each attribute of the use-site element, as a string variable —
  `citeurl="…"` becomes `citeurl`
  (a name that isn't a JS identifier is reachable as `$scope["data-x"]`);
- `children`, the element's inner HTML, serialized.

Both are *rendered first*:
the element runs its own directive pipeline before expansion,
so `ante:citeurl="expr"` on the use site evaluates into `citeurl`,
and directives inside the children run in the page scope.

## Scope scripts

Directive expressions fit in an attribute;
anything bigger goes in a scope script,
in a shortcode template or anywhere else in antedom:

```html
<script ante:scope>
  const caption = `<a href="${citeurl}">${citetext}</a>`;
</script>
```

`<script ante:scope>` runs its body once, at build time,
where it stands in the tree.
Top-level declarations — `var`, `let`, `const`,
`function`, `class`, destructuring included —
become scope variables for the element's
following siblings and their subtrees;
nothing escapes the containing element.
Free variables resolve in the surrounding scope
(`citeurl` above is the shortcode attribute),
and the element itself is dropped from output.

There is no DOM at build time — that's the point of antedom —
so there's no `this.getAttribute()` or `document`;
inputs are already variables in scope.

## Example

`layout/shortcode/quotefig.html`:

```html
<figure class="quotefig">
  <script ante:scope>
    const caption = `<a href="${citeurl}">${citetext}</a>`;
  </script>
  <blockquote ante:cite="citeurl">
    <p ante:html="children"></p>
  </blockquote>
  <figcaption ante:html="caption"></figcaption>
</figure>
```

A page writes:

```html
<shortcode-quotefig citeurl="https://ask.metafilter.com/55153/Whats-the-middle-ground-between-FU-and-Welcome#830421" citetext="tangerine on MetaFilter">
This is a classic case of Ask Culture meets Guess Culture.
</shortcode-quotefig>
```

and the built page contains:

```html
<figure class="quotefig">
  <blockquote cite="https://ask.metafilter.com/55153/Whats-the-middle-ground-between-FU-and-Welcome#830421">
    <p>This is a classic case of Ask Culture meets Guess Culture.</p>
  </blockquote>
  <figcaption>
    <a href="https://ask.metafilter.com/55153/Whats-the-middle-ground-between-FU-and-Welcome#830421">tangerine on MetaFilter</a>
  </figcaption>
</figure>
```

Live, on this page:

<shortcode-quotefig citeurl="https://ask.metafilter.com/55153/Whats-the-middle-ground-between-FU-and-Welcome#830421" citetext="tangerine on MetaFilter">
  This is a classic case of Ask Culture meets Guess Culture.
</shortcode-quotefig>

## In markdown

The usual [CommonMark raw-HTML rules](/markdown/) apply:

- Keep the opening tag on one line;
  a tag whose attributes span lines is not a raw HTML block.
- No blank lines inside the element passes its content through
  verbatim, as above — the template supplies the `<p>`.
- Blank lines after the open tag and before the close tag
  make the content markdown again; it arrives in `children`
  already wrapped in `<p>`s, so pair that style with a template
  that doesn't add its own.

## Notes

- Attribute values reach the template as plain strings.
  The template chooses how they land:
  `ante:text` escapes, `ante:html` parses
  (the `caption` above is HTML a scope script built from them).
- The prefix is mandatory: only `shortcode-*` elements expand,
  and one with no template in `layout/shortcode/` is a build error —
  as is any shortcode in a site with no `layout/` at all.

</template>

</template>
