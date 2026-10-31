import { defineConfig } from "blume";

// The site has two content folders, read as one root (the repository root):
//   - docs/*.md           the documentation, which is also what GitHub renders; it is the single source and is not copied;
//   - website/docs/*.mdx  the pages that only exist on the site (the landing page, with Cards and Steps).
// The titles of docs/*.md come from their first heading; their frontmatter holds only a unique description (meta description, Open Graph).
export default defineConfig({
  title: "remeris",
  description: "remeris is a release tool written in Go. It reads Knope's knope.toml, changeset files and Git tags, so it can replace Knope in an existing project.",
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
      // A plain followed link on every page (no nofollow): it credits Celest.io, who develops remeris.
      { label: "Developed by Celest.io", href: "https://celest.io" },
      { label: "Releases", href: "https://github.com/celest-io/remeris/releases" },
      { label: "Knope", href: "https://knope.tech" },
    ],
  },

  // Every page carries a schema.org Organization node naming Celest.io as the publisher, which is what search engines and AI agents read
  // to learn who is behind the site. Add Celest.io's other profiles to sameAs, and a link to its remeris page once that exists.
  seo: {
    organization: {
      name: "Celest.io",
      url: "https://celest.io",
      sameAs: ["https://github.com/celest-io"],
    },
  },

  // /docs has no page of its own: send it to the first page of the guide.
  redirects: [{ from: "/docs", to: "/docs/usage", status: 301 }],
});
