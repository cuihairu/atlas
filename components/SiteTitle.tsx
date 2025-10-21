import { QuartzComponent, QuartzComponentConstructor, QuartzComponentProps } from "../quartz/components/types"
import { i18n } from "../quartz/i18n"
import { pathToRoot } from "../quartz/util/path"

const SiteTitle: QuartzComponent = ({ cfg, fileData }: QuartzComponentProps) => {
  const title = cfg?.pageTitle ?? i18n(cfg.locale).propertyDefaults.title
  // Always link to site root relative to current page. This works both for local dev (/) and
  // GitHub Pages project sites (subpath like /atlas/) without hardcoding absolute URLs.
  const href = pathToRoot((fileData?.slug as any) ?? "index") + "/"
  return (
    <h2 class="page-title">
      <a href={href}>{title}</a>
    </h2>
  )
}

SiteTitle.css = `
.page-title {
  font-size: 1.85rem;
  font-weight: 600;
  margin: 0;
}
.page-title a {
  text-decoration: none;
}
`

export default (() => SiteTitle) satisfies QuartzComponentConstructor
