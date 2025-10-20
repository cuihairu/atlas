import { PageLayout, SharedLayout } from "./quartz/cfg"
import * as Component from "./quartz/components"
import SiteTitle from "./components/SiteTitle"

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
  beforeBody: [Component.ArticleTitle()],
  left: [SiteTitle()],
  right: [Component.Backlinks(), Component.Graph()],
  // Some Quartz emitters iterate over `afterBody`; ensure it's always an array
  afterBody: [],
}

// List page layout (tags/folders)
export const defaultListPageLayout: PageLayout = {
  beforeBody: [Component.ArticleTitle()],
  left: [SiteTitle()],
  right: [Component.Graph()],
  // Keep layout contract consistent with Quartz v4 expectations
  afterBody: [],
}
