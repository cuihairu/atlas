import { defineConfig } from 'vitepress'

export default defineConfig({
  lang: 'zh-CN',
  title: 'Atlas',
  description: '面向在线游戏的服务器注册、发现与角色目录基础设施',

  head: [
    ['link', { rel: 'icon', href: '/atlas/logo.svg', type: 'image/svg+xml' }],
    ['link', { rel: 'preconnect', href: 'https://fonts.googleapis.com' }],
    ['link', { rel: 'preconnect', href: 'https://fonts.gstatic.com', crossorigin: '' }],
    ['link', {
      href: 'https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700;800&family=Noto+Sans+SC:wght@400;500;600;700&family=JetBrains+Mono:wght@400;500;600&display=swap',
      rel: 'stylesheet',
    }],
  ],

  base: '/atlas/',

  themeConfig: {
    logo: '/logo.svg',

    nav: [
      { text: '快速上手', link: '/api-quickstart' },
      {
        text: '设计',
        items: [
          { text: '架构设计', link: '/architecture' },
          { text: '概念模型', link: '/concepts' },
          { text: 'API 参考', link: '/api' },
          { text: '数据模型', link: '/data-model' },
          { text: '服务器生命周期', link: '/lifecycle' },
          { text: '合服 / 转服 / 迁服', link: '/migration' },
          { text: '数据同步', link: '/sync' },
        ],
      },
      { text: '路线图', link: '/roadmap' },
      { text: 'v0.1.0', link: 'https://github.com/cuihairu/atlas/releases/tag/v0.1.0' },
    ],

    sidebar: [
      {
        text: '开始',
        items: [
          { text: '简介', link: '/' },
          { text: '快速上手', link: '/api-quickstart' },
        ],
      },
      {
        text: '设计文档',
        items: [
          { text: '架构设计', link: '/architecture' },
          { text: '概念模型', link: '/concepts' },
          { text: 'API 参考', link: '/api' },
          { text: '数据模型', link: '/data-model' },
          { text: '服务器生命周期', link: '/lifecycle' },
          { text: '合服 / 转服 / 迁服', link: '/migration' },
          { text: '数据同步', link: '/sync' },
        ],
      },
      {
        text: '规划',
        items: [
          { text: '路线图', link: '/roadmap' },
        ],
      },
    ],

    socialLinks: [
      { icon: 'github', link: 'https://github.com/cuihairu/atlas' },
    ],

    footer: {
      message: 'Atlas — Game Infrastructure Directory',
      copyright: '© 2026 cuihairu',
    },

    search: {
      provider: 'local',
    },

    editLink: {
      pattern: 'https://github.com/cuihairu/atlas/edit/main/docs/:path',
      text: '在 GitHub 上编辑此页',
    },

    outline: {
      level: [2, 3],
      label: '本页目录',
    },

    lastUpdated: {
      text: '最后更新',
    },
  },
})