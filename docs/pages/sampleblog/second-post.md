<template ante:layout="base.html">

<script ante:meta type="application/json">
{
  "title": "Second post",
  "date": "2026-07-04",
  "params": {
    "author": "A. N. Author"
  }
}
</script>

<template ante:fill="main">

By <span ante:text="page.params.author">author</span> —
a custom value from the metadata's free-form `params` object.

The middle post, dated <time ante:text="page.date">date</time>.
Its heading, like every page's, is the layout rendering
`title` from the page metadata.

</template>

</template>
