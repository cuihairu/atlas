import { PageLayout, SharedLayout } from "./quartz/cfg"
import * as Component from "./quartz/components"
import SiteTitle from "./components/SiteTitle"
import HomeCards from "./components/HomeCards"
import CatalogTree from "./components/CatalogTree"
import WordCount from "./components/WordCount"
// DebugInfo intentionally not shown per user request
import NavInit from "./components/NavInit"
import ExplorerFix from "./components/ExplorerFix"

// Toggles for meta display
const SHOW_READING_TIME = true
const SHOW_WORD_COUNT = true

// Components shared across all pages
export const sharedPageComponents: SharedLayout = {
  head: Component.Head(),
  header: [Component.Search(), Component.Darkmode()],
  // Add a tiny initializer to dispatch 'nav' on first load (non-SPA)
  afterBody: [NavInit(), ExplorerFix()],
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
    CatalogTree(),
    Component.ContentMeta({ showReadingTime: SHOW_READING_TIME, showComma: true }),
    ...(SHOW_WORD_COUNT ? [WordCount()] : []),
  ],
  left: [
    SiteTitle(),
    Component.Explorer({
      title: "目录",
      folderDefaultState: "collapsed",
      folderClickBehavior: "collapse",
      useSavedState: false,
      filterFn: (node) => !["tags", "templates-snippets", "scratch"].includes(node.name),
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
      folderDefaultState: "collapsed",
      folderClickBehavior: "collapse",
      useSavedState: false,
      filterFn: (node) => !["tags", "templates-snippets", "scratch"].includes(node.name),
    }),
  ],
  right: [Component.RecentNotes({ limit: 5, linkToMore: "tags", showTags: true }), Component.Graph()],
  // Keep layout contract consistent with Quartz v4 expectations
  afterBody: [Component.TagList()],
}
