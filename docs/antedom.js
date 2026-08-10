antedom.apiVersion(1);

const siteURL = "https://mrled.github.io/antedom";

function escapeXML(value) {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&apos;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;");
}

antedom.on("page:document", (page) => {
  page.document.highlight({
    style: "github-dark",
    unknownLanguage: "ignore",
  });
});

antedom.output("sitemap", {
  file: "sitemap.xml",

  begin(_, output) {
    output.write('<?xml version="1.0" encoding="UTF-8"?>\n');
    output.write('<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n');
  },

  page(page, output) {
    const location = escapeXML(siteURL + page.urlPath);
    output.write(`  <url><loc>${location}</loc></url>\n`);
  },

  end(_, output) {
    output.write("</urlset>\n");
  },
});
