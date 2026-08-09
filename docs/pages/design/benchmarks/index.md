<script ante:meta type="application/json">
{
  "title": "Benchmarks",
  "weight": 10,
  "layout": "base.html"
}
</script>


## Running benchmarks

Prereqs:

- Commit (make sure the benchmark is run against a clean git checkout)

Then run the benchmark:

```sh
go test -run '^$' -bench 'BenchmarkBuildBlog' -count 10 -benchtime 1x | tee docs/pages/design/benchmarks/$(date '+%Y%m%d-%H%M%S')-$(git rev-parse --short HEAD).txt
```

Some notes:

- `-run` specifies which tests to run, and `'^$'` matches none of them.
- `-bench` specifies which benchmarks to run, and `BenchmarkBuildBlog` is unanchored,
  so it finds `BenchmarkBuildBlogExtension` and any others with that substring in it as well.
