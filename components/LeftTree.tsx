import { QuartzComponent, QuartzComponentConstructor, QuartzComponentProps } from "../quartz/components/types"

type Note = any
type Node = {
  name: string
  displayName: string
  slug?: string
  file?: Note
  children?: Map<string, Node>
}

function stripPrefix(s: string) {
  return s.replace(/^\d+[\-_ ]*/, "")
}

function ensureChild(parent: Node, seg: string): Node {
  if (!parent.children) parent.children = new Map()
  const key = seg
  let child = parent.children.get(key)
  if (!child) {
    child = { name: seg, displayName: seg }
    parent.children.set(key, child)
  }
  return child
}

function buildTree(allFiles: Note[]): Node {
  const root: Node = { name: "", displayName: "" }
  for (const f of allFiles) {
    const slug = String(f.slug || f.data?.slug || "")
    if (!slug) continue
    const parts = slug.split("/")
    const isIndex = parts[parts.length - 1] === "index"
    const upTo = isIndex ? parts.length - 1 : parts.length
    let cur = root
    for (let i = 0; i < upTo; i++) {
      cur = ensureChild(cur, parts[i])
    }
    if (isIndex) {
      const title = f.frontmatter?.title || f.data?.frontmatter?.title
      if (title && title !== "index") cur.displayName = stripPrefix(title)
    } else {
      const leaf: Node = { name: parts[parts.length - 1], displayName: stripPrefix(f.frontmatter?.title || f.data?.frontmatter?.title || parts[parts.length - 1]), slug, file: f }
      if (!cur.children) cur.children = new Map()
      cur.children.set(leaf.name, leaf)
    }
  }
  return root
}

function sortNodes(a: Node, b: Node, level: number) {
  const aIsFolder = !!a.children && a.children.size > 0
  const bIsFolder = !!b.children && b.children.size > 0
  if (aIsFolder !== bIsFolder) return aIsFolder ? -1 : 1
  if (level === 0) {
    const pinned = [
      "domains",
      "data-analytics",
      "protocols-formats",
      "infrastructure",
      "data-systems",
      "distributed-systems",
      "concurrency-parallelism",
      "architecture-design",
      "observability",
      "performance-capacity",
      "languages",
      "frameworks",
    ]
    const rank = (n: string) => {
      const idx = pinned.indexOf(n)
      return idx === -1 ? 999 : idx
    }
    const ra = rank(a.name)
    const rb = rank(b.name)
    if (ra !== rb) return ra - rb
  }
  return (a.displayName || a.name).localeCompare((b.displayName || b.name), undefined, { numeric: true, sensitivity: "base" })
}

function renderDetails(node: Node, basePath: string, level: number): any {
  if (!node.children || node.children.size === 0) return null
  const entries = Array.from(node.children.values())
    .filter((n) => level !== 0 || !["tags", "templates-snippets", "scratch"].includes(n.name))
    .sort((a, b) => sortNodes(a, b, level))

  return (
    <ul class="content" style={{ paddingLeft: level > 0 ? "1.1rem" : "0" }}>
      {entries.map((child) => {
        const isFolder = !!child.children && child.children.size > 0
        const path = basePath ? `${basePath}/${child.name}` : child.name
        return (
          <li>
            {isFolder ? (
              <details open>
                <summary class="folder-title">
                  {stripPrefix(child.displayName || child.name)}
                  <a class="folder-link" href={`./${path}/`} title="打开目录">↗</a>
                </summary>
                {renderDetails(child, path, level + 1)}
              </details>
            ) : (
              <a href={`./${child.slug}`}>{stripPrefix(child.displayName || child.name)}</a>
            )}
          </li>
        )
      })}
    </ul>
  )
}

const LeftTree: QuartzComponent = (props: QuartzComponentProps) => {
  const tree = buildTree(props.allFiles as Note[])
  return (
    <nav class="lefttree">
      <h2>目录</h2>
      {renderDetails(tree, "", 0)}
    </nav>
  )
}

LeftTree.css = `
.lefttree h2 { font-size: 1rem; margin: 0; }
.lefttree summary { list-style: none; cursor: pointer; display: flex; align-items: center; gap: .4rem; }
.lefttree summary::-webkit-details-marker { display: none; }
.lefttree summary .folder-link { margin-left: auto; opacity: .7; text-decoration: none; }
.lefttree summary .folder-link:hover { opacity: 1; }
.lefttree ul { list-style: none; margin: .2rem 0; padding-left: .2rem; }
.lefttree a { color: var(--secondary); text-decoration: none; }
.lefttree a:hover { color: var(--tertiary); }
`

export default (() => LeftTree) satisfies QuartzComponentConstructor
