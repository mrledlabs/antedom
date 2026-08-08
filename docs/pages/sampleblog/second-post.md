<template ante:layout="base.html">

<script ante:meta type="application/json">
{
  "date": "2026-07-04",
  "params": {
    "author": "A. N. Author"
  }
}
</script>

<template ante:fill="main">

# Second post

By <span ante:text="page.params.author">author</span> —
a custom value from the metadata's free-form `params` object.

The middle post, dated <time ante:text="page.date">date</time>.
Its title comes from the `<h1>` above;
a `title` in the page metadata would override it.

</template>

</template>
