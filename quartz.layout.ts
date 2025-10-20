import { PageLayout, SharedLayout } from "./quartz/cfg"
import * as Component from "./quartz/components"
import SiteTitle from "./components/SiteTitle"
import HomeCards from "./components/HomeCards"

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
    Component.ContentMeta({ showReadingTime: true, showComma: true }),
  ],
  left: [
    SiteTitle(),
    Component.Explorer({
      title: "目录",
      folderDefaultState: "open",
      folderClickBehavior: "collapse",
      useSavedState: true,
      // keep default sort/filter/order
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
    Component.ContentMeta({ showReadingTime: true, showComma: true }),
  ],
  left: [
    SiteTitle(),
    Component.Explorer({
      title: "目录",
      folderDefaultState: "open",
      folderClickBehavior: "collapse",
      useSavedState: true,
    }),
  ],
  right: [Component.RecentNotes({ limit: 5, linkToMore: "tags", showTags: true }), Component.Graph()],
  // Keep layout contract consistent with Quartz v4 expectations
  afterBody: [Component.TagList()],
}
