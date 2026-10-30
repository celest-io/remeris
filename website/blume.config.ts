import { defineConfig } from "blume";

// The site has two content folders, read as one root (the repository root):
//   - docs/*.md           the documentation, which is also what GitHub renders; it is the single source and is not copied;
//   - website/docs/*.mdx  the pages that only exist on the site (the landing page, with Cards and Steps).
// The titles of docs/*.md come from their first heading, so they need no frontmatter.
export default defineConfig({
  title: "remeris",
  description: "A release tool written in Go that reads Knope's configuration: a drop-in replacement for Knope.",
  logo: "/logo.svg",

  content: {
    root: "..",
    include: ["docs/*.md", "website/docs/*.mdx"],
  },

  // dir: this project is website/ inside the repository, so "Edit on GitHub" resolves ../docs/usage.md to docs/usage.md.
  github: { owner: "celest-io", repo: "remeris", branch: "main", dir: "website" },

  theme: { accent: "purple", mode: "system", toggle: true },

  // A static site: the default local Orama search, llms.txt and the sitemap are built into dist/.
  deployment: { site: "https://remeris.dev" },

  navigation: {
    sidebar: [
      "/",
      {
        label: "Guide",
        collapsed: false,
        items: ["/docs/usage", "/docs/configuration", "/docs/differences"],
      },
      {
        label: "Maintainers",
        collapsed: false,
        items: ["/docs/maintaining", "/docs/releasing"],
      },
      {
        label: "For implementers",
        collapsed: false,
        // The title of the page is long ("Remeris: spec for a Go reimplementation of Knope"), so the sidebar names it.
        items: [{ label: "Specification", href: "/docs/spec" }],
      },
    ],
    actions: [{ label: "GitHub", href: "https://github.com/celest-io/remeris" }],
  },

  footer: {
    copyright: "Remeris is released under the Apache License 2.0.",
    links: [
      { label: "Releases", href: "https://github.com/celest-io/remeris/releases" },
      { label: "Knope", href: "https://knope.tech" },
    ],
  },

  // /docs has no page of its own: send it to the first page of the guide.
  redirects: [{ from: "/docs", to: "/docs/usage", status: 301 }],
});
