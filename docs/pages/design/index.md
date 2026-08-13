<script ante:meta type="application/json">
{
  "title": "Design",
  "weight": 70,
  "layout": "base.html"
}
</script>

Design notes for antedom architecture, including implemented features and
planned extensions.

- [Extensibility](extensibility/) — a Go build kernel orchestrated by
  project JavaScript, with lifecycle hooks, Go-backed capabilities, and
  first-class output formats.
- [Page assets](assets/) — page-local opaque files exposed to template
  JavaScript through `page.assets`.
- [Benchmarks](benchmarks/) — benchmark methodology and recordings for the
  build pipeline and extension boundary.
