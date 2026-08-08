<template ante:layout="base.html">

<template ante:fill="main">

# sampleblog

Example posts ordered by date.
Each post sets `date` in its page metadata —
a `<script ante:meta>` element in the page source,
antedom's answer to frontmatter — and none sets `weight`,
so dates decide the order, newest first.
(The docs pages you're browsing set `weight` the same way.)
Metadata allows `title`, `weight`, `date`, and `params` —
anything else is a build error.

<ul>
  <li ante:for="p of pages.filter(q => q.path.startsWith('/sampleblog/') && q.path !== page.path)">
    <a ante:href="p.path" ante:text="p.title">post</a>
    — <time ante:text="p.date">date</time>
  </li>
</ul>

That list is an `ante:for` over the `pages` global,
filtered to this section, already in order.

</template>

</template>
