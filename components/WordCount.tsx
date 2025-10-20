import { QuartzComponent, QuartzComponentConstructor, QuartzComponentProps } from "../quartz/components/types"
import { classNames } from "../quartz/util/lang"

const WordCount: QuartzComponent = ({ fileData, displayClass }: QuartzComponentProps) => {
  const text = fileData.text ?? ""
  // Rough character count excluding whitespace; good enough for display
  const count = text.replace(/\s+/g, "").length
  if (count === 0) return null
  return (
    <p class={classNames(displayClass, "word-count")}>
      约 {count} 字
    </p>
  )
}

WordCount.css = `
.word-count { color: var(--gray); margin: 0.2rem 0 0.6rem 0; font-size: 0.95rem; }
`

export default (() => WordCount) satisfies QuartzComponentConstructor

