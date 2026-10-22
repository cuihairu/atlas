import DefaultTheme from 'vitepress/theme'
import type { Theme } from 'vitepress'
import ShowcaseCarousel from './ShowcaseCarousel.vue'
import './custom.css'

export default {
  extends: DefaultTheme,
  enhanceApp({ app }) {
    // 首页/场景导览的走马灯组件，markdown 里直接 <ShowcaseCarousel …/>
    app.component('ShowcaseCarousel', ShowcaseCarousel)
  },
} satisfies Theme
