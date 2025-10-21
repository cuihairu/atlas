import { QuartzComponent, QuartzComponentConstructor } from "../quartz/components/types"

// Inject small CSS overrides to ensure Explorer isn't clipped in the left sidebar.
// This is a conservative override; we can relax once upstream styles are confirmed.
const ExplorerFix: QuartzComponent = () => null

ExplorerFix.css = `
/* Give the left sidebar a viewport-based height so children can flex within it */
.left.sidebar { height: calc(100vh - 6rem); /* 6rem = $topSpacing */ }

/* Make Explorer fill remaining space and scroll internally */
.left.sidebar .explorer { display: flex; flex-direction: column; flex: 1 1 auto; min-height: 0; }
.left.sidebar .explorer #explorer-content { flex: 1 1 auto; min-height: 0; overflow-y: auto; overflow-x: hidden; max-height: none; }

/* Let the list itself be visible and not clip */
.left.sidebar .explorer ul.overflow { overflow: initial; }
`

export default (() => ExplorerFix) satisfies QuartzComponentConstructor
