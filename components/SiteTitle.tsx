import { QuartzComponent, QuartzComponentConstructor, QuartzComponentProps } from "./quartz/components/types"
import { i18n } from "./quartz/i18n"

const SiteTitle: QuartzComponent = ({ cfg }: QuartzComponentProps) => {
  const title = cfg?.pageTitle ?? i18n(cfg.locale).propertyDefaults.title
  const href = cfg?.baseUrl ? `https://${cfg.baseUrl}` : "/"
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

