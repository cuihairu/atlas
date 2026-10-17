import DefaultTheme from 'vitepress/theme'
import type { Router, Theme } from 'vitepress'
import { inBrowser } from 'vitepress'
import ShowcaseCarousel from './ShowcaseCarousel.vue'
import './custom.css'

const STORAGE_KEY = 'atlas-lang-picked'

/**
 * 首次进入时按浏览器语言选择文档语言（vitepress 官方不提供该能力，
 * GitHub Pages 也不支持 Netlify 式 _redirects，故在主题扩展点实现）。
 *
 * 优先级：
 *   1. localStorage 中用户手动选择过的语言（永远优先）
 *   2. navigator.languages 浏览器/系统语言
 *   3. 保持当前 locale（root = English，/zh/ = 简体中文）
 *
 * 只在「首次且位于根路径 /」时判断一次，避免站内跳转循环。
 */
function pickLocale(): string | null {
  if (!inBrowser) return null
  // 1) 用户手动选择过就不再干预
  const picked = localStorage.getItem(STORAGE_KEY)
  if (picked) return picked
  // 2) 只在根路径做判断
  if (window.location.pathname !== '/') return null
  const langs = (navigator.languages && navigator.languages.length
    ? navigator.languages
    : [navigator.language]
  )
    .map((l) => l.toLowerCase())
  const zh = langs.some((l) => l.startsWith('zh'))
  return zh ? '/zh/' : '/'
}

function redirectToLocale(router: Router) {
  const target = pickLocale()
  if (target && target !== window.location.pathname) {
    router.go(target)
  }
}

export default {
  extends: DefaultTheme,
  enhanceApp({ app }) {
    // 首页/场景导览的走马灯组件，markdown 里直接 <ShowcaseCarousel …/>
    app.component('ShowcaseCarousel', ShowcaseCarousel)
  },
  extendRouter(router) {
    if (inBrowser) {
      // 首屏：按浏览器语言选择
      redirectToLocale(router)
      // 站内导航后：若用户手动切换，记住选择（供下次优先）
      router.onAfterRouteChange = (to: string) => {
        try {
          if (to.startsWith('/zh/')) localStorage.setItem(STORAGE_KEY, '/zh/')
          else if (to === '/' || to.startsWith('/en/'))
            localStorage.setItem(STORAGE_KEY, '/')
        } catch {
          /* localStorage 不可用时静默降级为每次按浏览器语言判断 */
        }
      }
    }
  },
} satisfies Theme
