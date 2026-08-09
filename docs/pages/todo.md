<script ante:meta type="application/json">
{
  "title": "To do",
  "weight": 80,
  "layout": "base.html"
}
</script>

Plans and ideas

- Archetypes

  When creating a new page, lay down an archetype that provides sample or boilerplate content.

  These need to be able to both run template logic at creation time,
  and include template directives for running at render time.
  Not sure how to do this yet.

- Extensible CLI

  The CLI should be extensible, turning this into a general site builder tool.
  Anything you might use a wrapper or something in scripts/ to do should be callable from antedom itself.
  This might end up being powerful enough that we don't need archetypes.

  Extensible logic should work in a VS Code plugin or other programs that we might want to build in the future.

- Programmable output

  More than just text manipulation, we should be able to build sqlite databases and images from inputs.

  Must be exposed to extension system too.

- Hugo-like mounts

  So we can pull content and layout from multiple locations.
  Will enable themes.

- Reusable themes

- Dependency tree

  A site should be able to depend on extensions and themes.
  The theme reasoning is just like Hugo.
  Extensions would be great too ---
  someone could build an extension that pulled data from some source
  and a site could depend on that functionality without needing a theme that affects the UI.

- Authentication for some/all of a site

  Would be great if this could be handled via the extension points.

- Listen on a UNIX socket

  This bit me with the ellipsis project.
  Would allow running the daemon without exposing it to everything on localhost.

- Syntax highlighting for code blocks

