import { defineConfig, godoc } from "sourcey";

export default defineConfig({
  name: "Phero Go API",
  siteUrl: "https://henomis.github.io",
  baseUrl: "/phero/docs/api",
  prettyUrls: false,
  repo: "https://github.com/henomis/phero",
  editBranch: "main",
  theme: {
    preset: "default",
    colors: {
      primary: "#4ecdc4",
      light: "#83e8df",
      dark: "#173f43"
    }
  },
  navigation: {
    tabs: [
      {
        tab: "Go API",
        slug: "go-api",
        source: godoc({
          module: "../..",
          packages: ["./..."],
          mode: "live",
          includeTests: true,
          includeUnexported: false,
          hideUndocumented: false,
          exclude: [
            "github.com/henomis/phero/examples",
            "github.com/henomis/phero/internal",
            "github.com/henomis/phero/tests"
          ],
          sourceBasePath: ""
        })
      }
    ]
  },
  navbar: {
    links: [
      { label: "Phero docs", href: "../index.html" },
      { label: "Repository", href: "https://github.com/henomis/phero" },
      { label: "pkg.go.dev", href: "https://pkg.go.dev/github.com/henomis/phero" }
    ]
  },
  footer: {
    links: [
      { label: "Phero", href: "https://github.com/henomis/phero" },
      { label: "Generated with Sourcey 3.6.5", href: "https://github.com/sourcey/sourcey" }
    ]
  }
});
