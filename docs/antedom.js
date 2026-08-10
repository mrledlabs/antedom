antedom.apiVersion(1);

antedom.on("page:document", (page) => {
  page.document.highlight({
    style: "github-dark",
    unknownLanguage: "ignore",
  });
});
