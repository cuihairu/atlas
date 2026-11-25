import DefaultTheme from 'vitepress/theme'
import type { Theme } from 'vitepress'
import ShowcaseCarousel from './ShowcaseCarousel.vue'
import './custom.css'

export default {
  extends: DefaultTheme,
  enhanceApp({ app }) {
    // Carousel component for the homepage / scenario tour; used directly
    // in markdown as <ShowcaseCarousel …/>
    app.component('ShowcaseCarousel', ShowcaseCarousel)
  },
} satisfies Theme
