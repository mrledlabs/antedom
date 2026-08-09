<script ante:meta type="application/json">
{
  "title": "Sample blog",
  "layout": "base.html"
}
</script>

Example posts ordered by date.
Each post sets `date` in its page metadata —
a `<script ante:meta>` element in the page source,
antedom's answer to frontmatter — and none sets `weight`,
so dates decide the order, newest first.
(The docs pages you're browsing set `weight` the same way.)
Metadata allows `title`, `weight`, `date`, `layout`, and `params` —
anything else is a build error.

Posts use their own layout — `layout/post.html`,
chained through `base.html` — adding a byline
and newer/older links computed from the `pages` global.
Each post names it in its metadata (the sugar form):
the post source is just metadata and markdown,
no wrapper templates.

<ul>
  <li ante:for="p of pages.filter(q => q.path.startsWith('/sampleblog/') && q.path !== page.path)">
    <a ante:href="p.path" ante:text="p.title">post</a>
    — <time ante:text="p.date">date</time>
  </li>
</ul>

That list is an `ante:for` over the `pages` global,
filtered to this section, already in order.

