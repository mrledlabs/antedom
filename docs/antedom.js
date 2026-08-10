antedom.apiVersion(1);

const siteURL = "https://mrled.github.io/antedom";
const feed = {
  title: "Antedom sample blog",
  description: "Posts from the Antedom documentation sample blog",
  path: "/feed.xml",
};

function rssDate(value) {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) {
    throw new Error(`RSS date must use YYYY-MM-DD: ${value}`);
  }
  const date = new Date(`${value}T00:00:00Z`);
  if (Number.isNaN(date.valueOf()) || date.toISOString().slice(0, 10) !== value) {
    throw new Error(`invalid RSS date: ${value}`);
  }
  return date.toUTCString();
}

antedom.on("page:document", (page) => {
  page.document.highlight({
    style: "github-dark",
    unknownLanguage: "ignore",
  });
});

antedom.output("sitemap", {
  file: "sitemap.xml",
  validate: "xml",

  begin(_, output) {
    output.write(antedom.xml`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
`);
  },

  page(page, output) {
    if (page.meta.params?.sitemap === false) return;

    const location = antedom.url.resolve(siteURL, page.urlPath);
    output.write(antedom.xml`  <url>
    <loc>${location}</loc>
`);
    if (page.meta.date) {
      output.write(antedom.xml`    <lastmod>${page.meta.date}</lastmod>
`);
    }
    output.write(antedom.xml`  </url>
`);
  },

  end(_, output) {
    output.write(antedom.xml`</urlset>
`);
  },
});

const feedEntries = [];

antedom.output("rss", {
  file: "feed.xml",
  validate: "xml",

  page(page) {
    if (!page.meta.date || page.meta.params?.feed === false) return;

    feedEntries.push({
      title: page.meta.title,
      date: page.meta.date,
      published: rssDate(page.meta.date),
      url: antedom.url.resolve(siteURL, page.urlPath),
      html: page.html,
    });
  },

  end(_, output) {
    feedEntries.sort((a, b) => b.date.localeCompare(a.date));
    const items = feedEntries.map((entry) => antedom.xml`    <item>
      <title>${entry.title}</title>
      <link>${entry.url}</link>
      <guid isPermaLink="true">${entry.url}</guid>
      <pubDate>${entry.published}</pubDate>
      <description>${entry.html}</description>
    </item>
`);
    const feedURL = antedom.url.resolve(siteURL, feed.path);
    const newestDate = feedEntries.length
      ? antedom.xml`<lastBuildDate>${feedEntries[0].published}</lastBuildDate>`
      : antedom.xml``;
    output.write(antedom.xml`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:atom="http://www.w3.org/2005/Atom">
  <channel>
    <title>${feed.title}</title>
    <link>${siteURL}</link>
    <description>${feed.description}</description>
    ${newestDate}
    <atom:link href="${feedURL}" rel="self" type="application/rss+xml"/>
${antedom.xml.join(items)}  </channel>
</rss>
`);
  },
});
