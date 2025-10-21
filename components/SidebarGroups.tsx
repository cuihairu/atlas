import { QuartzComponent, QuartzComponentConstructor, QuartzComponentProps } from "../quartz/components/types"

// Minimal fallback sidebar that lists top-level groups from allFiles
const SidebarGroups: QuartzComponent = ({ allFiles }: QuartzComponentProps) => {
  const slugs: string[] = allFiles
    .map((f: any) => String(f.slug || f.data?.slug || ""))
    .filter(Boolean)
  const groups = Array.from(new Set(
    slugs
      .map((s) => s.split("/")[0])
      .filter((g) => g && g !== "index" && g !== "tags"),
  )).sort()

  if (groups.length === 0) return null

  return (
    <nav class="sidebar-groups">
      <h3>目录(备选)</h3>
      <ul>
        {groups.map((g) => (
          <li><a href={`./${g}/`}>{g}</a></li>
        ))}
      </ul>
    </nav>
  )
}

SidebarGroups.css = `
.sidebar-groups { font-size: .95rem; }
.sidebar-groups h3 { margin: .4rem 0; font-size: 1rem; }
.sidebar-groups ul { list-style: none; padding-left: 0; margin: 0; }
.sidebar-groups li { margin: .15rem 0; }
`

export default (() => SidebarGroups) satisfies QuartzComponentConstructor

