<template ante:layout="post.html">

<script ante:meta type="application/json">
{
  "title": "First post",
  "date": "2026-06-10"
}
</script>

<template ante:fill="post">

The oldest post, so it lists last:
no `weight`, and its `date` — <time ante:text="page.date">date</time> —
sorts after the newer posts.

</template>

</template>
