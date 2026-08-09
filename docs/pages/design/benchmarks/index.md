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

## Recordings

Newest first. Each heading links to the raw
[benchfmt](https://go.googlesource.com/proposal/+/master/design/14313-benchmark-format.md)
recording; rows average the samples of one benchmark. The recordings
are this page's [assets](/design/assets/), and everything below —
benchfmt parsing included — is computed at build time by this page's
own `ante:scope` script.

<script ante:scope>
  // One recording's raw benchfmt text -> {config, units, rows}.
  // Config lines are "key: value"; result lines are tab-separated:
  // name, iterations, then "value unit" per metric. ns/op is rescaled
  // to ms/op; means are rounded to 4 significant digits. A benchmark
  // without some metric just lacks that key in means (ante:text
  // renders the absent value as an empty cell).
  function parseBench(text) {
    const config = {}, units = [], order = [], byName = {};
    for (const line of text.split("\n")) {

      // Each ^Benchmark.* line is one run of a benchmark
      if (line.startsWith("Benchmark")) {
        const f = line.split("\t").map(s => s.trim()).filter(Boolean);
        const name = f[0].replace(/^Benchmark/, "").replace(/-\d+$/, "");
        const metrics = {};
        for (const cell of f.slice(2)) {
          const parts = cell.split(/\s+/);
          if (parts.length !== 2) continue;
          let val = Number(parts[0]), unit = parts[1];
          if (unit === "ns/op") { val /= 1e6; unit = "ms/op"; }
          metrics[unit] = val;
          if (!units.includes(unit)) units.push(unit);
        }
        if (!byName[name]) { byName[name] = []; order.push(name); }
        byName[name].push(metrics);

      // These lines are key: value lines at the top of the .txt file that record the commit, hardware info, date, etc
      } else {
        const m = line.match(/^([a-z][^:\s]*): (.*)$/);
        if (m) config[m[1]] = m[2];
      }
    }
    const rows = order.map(name => {
      const samples = byName[name], means = {};
      for (const u of units) {
        const vs = samples.map(s => s[u]).filter(v => v !== undefined);
        if (vs.length) means[u] = Number((vs.reduce((a, b) => a + b) / vs.length).toPrecision(4));
      }
      return { name, samples: samples.length, means };
    });
    return { config, units, rows };
  }

  const describe = c => [`commit ${c.commit || "?"}`, c.host, c.cpu, c["go-version"],
    `benchtime ${c.benchtime || "?"} ×${c.count || "?"}`].filter(Boolean).join(" · ");

  const recordings = page.assets
    .filter(a => a.name.endsWith(".txt"))
    .map(a => ({ file: a.name, url: a.url, ...parseBench(a.text()) }))
    .reverse();
</script>

<section ante:for="rec of recordings">
  <h3><a ante:href="rec.url" ante:text="rec.file">recording</a></h3>
  <p ante:text="describe(rec.config)">configuration</p>
  <table>
    <thead>
      <tr>
        <th>benchmark</th>
        <th>samples</th>
        <th ante:for="u of rec.units" ante:text="u">unit</th>
      </tr>
    </thead>
    <tbody>
      <tr ante:for="row of rec.rows">
        <td ante:text="row.name">name</td>
        <td ante:text="row.samples">n</td>
        <td ante:for="u of rec.units" ante:text="row.means[u]">value</td>
      </tr>
    </tbody>
  </table>
</section>
