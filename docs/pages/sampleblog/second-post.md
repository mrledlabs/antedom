<template ante:layout="post.html">

<script ante:meta type="application/json">
{
  "title": "Second post",
  "date": "2026-07-04",
  "params": {
    "author": "A. N. Author"
  }
}
</script>

<template ante:fill="post">

The middle post.
The byline above shows this post's `params.author` —
a custom value from the metadata's free-form `params` object;
the other posts set no author, so their bylines are date-only.

Its heading, like every page's, is the layout rendering
`title` from the page metadata.

</template>

</template>
