import { QuartzComponent, QuartzComponentConstructor, QuartzComponentProps } from "../quartz/components/types"

const DebugInfo: QuartzComponent = ({ allFiles, fileData }: QuartzComponentProps) => {
  try {
    const total = allFiles.length
    const topGroups = Array.from(
      new Set(
        allFiles
          .map((f: any) => String(f.slug || f.data?.slug || ""))
          .filter((s) => s && s.includes("/") && s.split("/").length >= 2)
          .map((s) => s.split("/")[0]),
      ),
    )
      .sort()
      .slice(0, 20)

    const sample = allFiles
      .map((f: any) => String(f.slug || f.data?.slug || ""))
      .filter(Boolean)
      .slice(0, 10)

    return (
      <details class="debug-info">
        <summary>Debug</summary>
        <div>current slug: <code>{String(fileData.slug || "")}</code></div>
        <div>allFiles: <b>{total}</b></div>
        <div>top groups ({topGroups.length}):</div>
        <ul>
          {topGroups.map((g) => (
            <li>{g}</li>
          ))}
        </ul>
        <div>sample slugs:</div>
        <ul>
          {sample.map((s) => (
            <li>{s}</li>
          ))}
        </ul>
      </details>
    )
  } catch (e) {
    return (
      <details class="debug-info">
        <summary>Debug</summary>
        <div>error rendering debug: {String(e)}</div>
      </details>
    )
  }
}

DebugInfo.css = `
.debug-info { font-size: 0.8rem; opacity: 0.9; margin-top: 0.5rem; }
.debug-info summary { cursor: pointer; }
.debug-info ul { margin: 0.25rem 0; padding-left: 1rem; }
`

export default (() => DebugInfo) satisfies QuartzComponentConstructor

