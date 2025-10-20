import { PageLayout, SharedLayout } from "./quartz/cfg"
import * as Component from "./quartz/components"
import SiteTitle from "./components/SiteTitle"
import HomeCards from "./components/HomeCards"
import WordCount from "./components/WordCount"

// Toggles for meta display
const SHOW_READING_TIME = true
const SHOW_WORD_COUNT = true

// Components shared across all pages
export const sharedPageComponents: SharedLayout = {
  head: Component.Head(),
  header: [Component.Search(), Component.Darkmode()],
  // Ensure emitters (e.g., 404Page) always have an iterable afterBody
  afterBody: [],
  footer: Component.Footer(),
}

// Single content page layout
export const defaultContentPageLayout: PageLayout = {
  beforeBody: [
    Component.Breadcrumbs({
      rootName: "首页",
      spacerSymbol: "›",
      resolveFrontmatterTitle: true,
      hideOnRoot: true,
      showCurrentPage: true,
    }),
    HomeCards(),
    Component.ArticleTitle(),
    Component.ContentMeta({ showReadingTime: SHOW_READING_TIME, showComma: true }),
    ...(SHOW_WORD_COUNT ? [WordCount()] : []),
  ],
  left: [
    SiteTitle(),
    Component.Explorer({
      title: "目录",
      folderDefaultState: "open",
      folderClickBehavior: "collapse",
      useSavedState: true,
      filterFn: (node) => !["tags","98-templates-snippets","99-scratch"].includes(node.name),
      // Pin common top-level folders
      sortFn: (a, b) => {
        const isFolderA = !a.file
        const isFolderB = !b.file
        if (isFolderA !== isFolderB) return isFolderA ? -1 : 1
        const pinned = [
          "95-domains",
          "62-data-analytics",
          "30-protocols-formats",
          "50-infrastructure",
          "60-data-systems",
          "70-distributed-systems",
          "75-concurrency-parallelism",
          "80-architecture-design",
          "85-observability",
          "86-performance-capacity",
          "20-languages",
          "20-frameworks",
        ]
        const rank = (n: string) => {
          const idx = pinned.indexOf(n)
          return idx === -1 ? 999 : idx
        }
        const ra = rank(a.name)
        const rb = rank(b.name)
        if (ra !== rb) return ra - rb
        return a.displayName.localeCompare(b.displayName, undefined, { numeric: true, sensitivity: "base" })
      },
      // Map folder nodes to use the title from their index.md if present
      mapFn: (node) => {
        if (!node.file) {
          const idxChild = node.children.find((c: any) => c.file && c.name === "index")
          if (idxChild && idxChild.file?.frontmatter?.title && idxChild.file.frontmatter.title !== "index") {
            node.displayName = idxChild.file.frontmatter.title
          }
        }
      },
      order: ["filter","map","sort"],
    }),
  ],
  right: [
    Component.RecentNotes({ limit: 5, linkToMore: "tags", showTags: true }),
    Component.Backlinks(),
    Component.Graph(),
  ],
  // Some Quartz emitters iterate over `afterBody`; ensure it's always an array
  afterBody: [Component.TagList()],
}

// List page layout (tags/folders)
export const defaultListPageLayout: PageLayout = {
  beforeBody: [
    Component.Breadcrumbs({
      rootName: "首页",
      spacerSymbol: "›",
      resolveFrontmatterTitle: true,
      hideOnRoot: true,
      showCurrentPage: true,
    }),
    Component.ArticleTitle(),
    Component.ContentMeta({ showReadingTime: SHOW_READING_TIME, showComma: true }),
    ...(SHOW_WORD_COUNT ? [WordCount()] : []),
  ],
  left: [
    SiteTitle(),
    Component.Explorer({
      title: "目录",
      folderDefaultState: "open",
      folderClickBehavior: "collapse",
      useSavedState: true,
      filterFn: (node) => !["tags","98-templates-snippets","99-scratch"].includes(node.name),
      sortFn: (a, b) => {
        const isFolderA = !a.file
        const isFolderB = !b.file
        if (isFolderA !== isFolderB) return isFolderA ? -1 : 1
        const pinned = [
          "95-domains",
          "62-data-analytics",
          "30-protocols-formats",
          "50-infrastructure",
          "60-data-systems",
          "70-distributed-systems",
          "75-concurrency-parallelism",
          "80-architecture-design",
          "85-observability",
          "86-performance-capacity",
          "20-languages",
          "20-frameworks",
        ]
        const rank = (n: string) => {
          const idx = pinned.indexOf(n)
          return idx === -1 ? 999 : idx
        }
        const ra = rank(a.name)
        const rb = rank(b.name)
        if (ra !== rb) return ra - rb
        return a.displayName.localeCompare(b.displayName, undefined, { numeric: true, sensitivity: "base" })
      },
      mapFn: (node) => {
        if (!node.file) {
          const idxChild = node.children.find((c: any) => c.file && c.name === "index")
          if (idxChild && idxChild.file?.frontmatter?.title && idxChild.file.frontmatter.title !== "index") {
            node.displayName = idxChild.file.frontmatter.title
          }
        }
      },
      order: ["filter","map","sort"],
    }),
  ],
  right: [Component.RecentNotes({ limit: 5, linkToMore: "tags", showTags: true }), Component.Graph()],
  // Keep layout contract consistent with Quartz v4 expectations
  afterBody: [Component.TagList()],
}
