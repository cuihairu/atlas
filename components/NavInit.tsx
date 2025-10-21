import { QuartzComponent, QuartzComponentConstructor } from "../quartz/components/types"

const NavInit: QuartzComponent = () => null

// Ensure Explorer and other components that listen to the custom 'nav' event
// initialize even when SPA is disabled (first load).
NavInit.afterDOMLoaded = () => {
  const fire = () => {
    try {
      document.dispatchEvent(new CustomEvent("nav"))
    } catch (e) {
      // no-op
    }
  }
  // Fire immediately, then again in a microtask, and on DOMContentLoaded fallback
  fire()
  setTimeout(fire, 0)
  if (document.readyState === "loading") {
    window.addEventListener(
      "DOMContentLoaded",
      () => {
        fire()
      },
      { once: true },
    )
  }
}

export default (() => NavInit) satisfies QuartzComponentConstructor

