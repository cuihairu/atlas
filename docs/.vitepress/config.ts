import { defineConfig } from 'vitepress'
import { withMermaid } from 'vitepress-plugin-mermaid'

type MenuItem = { text: string; link?: string; items?: MenuItem[] }

// Chinese pages all live under docs/zh/, so the zh locale's nav / sidebar holds
// Chinese copy only; localize() uniformly prepends /zh to in-site links;
// external absolute URLs (GitHub Releases) are kept as is.
function localize<T extends MenuItem>(items: T[], prefix: string): T[] {
  return items.map((item) => ({
    ...item,
    ...(item.link?.startsWith('/') ? { link: prefix + item.link } : {}),
    ...(item.items ? { items: localize(item.items, prefix) } : {}),
  }))
}

const zhNav: MenuItem[] = [
  { text: '快速上手', link: '/api-quickstart' },
  { text: '场景导览', link: '/scenarios' },
  {
    text: '接入',
    items: [
      { text: '服务器信息配置化', link: '/server-config' },
      { text: '服务器生命周期', link: '/lifecycle' },
      { text: 'Realm 与 Shard', link: '/realms-shards' },
      { text: '数据同步（Message Bus）', link: '/sync' },
      { text: '玩家在线状态', link: '/presence' },
      { text: '配置中心（跨服协调）', link: '/config-center' },
      { text: 'APISIX 接入插件', link: '/apisix' },
      { text: 'SDK（Go / C++ / Python / JS / Java / C#）', link: '/sdk-go' },
    ],
  },
  {
    text: '运维',
    items: [
      { text: '公告与计划维护', link: '/operations' },
      { text: '合服 / 转服 / 迁服', link: '/migration' },
      { text: '高可用', link: '/ha' },
      { text: '部署拓扑', link: '/topology' },
      { text: '性能设计', link: '/performance' },
      { text: '性能基准', link: '/benchmarks' },
      { text: '安全', link: '/security' },
    ],
  },
  {
    text: '参考',
    items: [
      { text: '概念模型', link: '/concepts' },
      { text: '架构设计', link: '/architecture' },
      { text: 'API 参考', link: '/api' },
      { text: '数据模型', link: '/data-model' },
      { text: '安全审计', link: '/security-audit' },
      { text: '路线图', link: '/roadmap' },
      { text: 'GitHub Releases', link: 'https://github.com/cuihairu/atlas/releases' },
    ],
  },
]

const zhSidebar: MenuItem[] = [
  {
    text: '开始',
    items: [
      { text: '简介', link: '/' },
      { text: '快速上手', link: '/api-quickstart' },
      { text: '场景导览', link: '/scenarios' },
      { text: '概念模型', link: '/concepts' },
    ],
  },
  {
    text: '接入',
    items: [
      { text: '服务器信息配置化', link: '/server-config' },
      { text: '服务器生命周期', link: '/lifecycle' },
      { text: 'Realm 与 Shard', link: '/realms-shards' },
      { text: '数据同步', link: '/sync' },
      { text: '玩家在线状态', link: '/presence' },
      { text: '配置中心', link: '/config-center' },
      { text: 'APISIX 接入插件', link: '/apisix' },
    ],
  },
  {
    text: 'SDK',
    items: [
      { text: 'Go SDK', link: '/sdk-go' },
      { text: 'C++ SDK', link: '/sdk-cpp' },
      { text: 'Python SDK', link: '/sdk-python' },
      { text: 'JavaScript SDK', link: '/sdk-js' },
      { text: 'Java SDK', link: '/sdk-java' },
      { text: 'C# SDK', link: '/sdk-csharp' },
    ],
  },
  {
    text: '运维',
    items: [
      { text: '公告与计划维护', link: '/operations' },
      { text: '合服 / 转服 / 迁服', link: '/migration' },
      { text: '高可用', link: '/ha' },
      { text: '部署拓扑', link: '/topology' },
      { text: '性能设计', link: '/performance' },
      { text: '性能基准', link: '/benchmarks' },
      { text: '安全', link: '/security' },
    ],
  },
  {
    text: '参考',
    items: [
      { text: '架构设计', link: '/architecture' },
      { text: 'API 参考', link: '/api' },
      { text: '数据模型', link: '/data-model' },
      { text: '安全审计', link: '/security-audit' },
      { text: '路线图', link: '/roadmap' },
    ],
  },
]

export default withMermaid({
  title: 'Atlas',

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

  // Internal audit / review records stay out of the site: these pages carry
  // file:line-level source references and superseded conclusions, kept in the
  // repo for reviewers only and not published as public docs. VitePress
  // builds every .md under srcDir by default; absence from nav/sidebar only
  // affects discoverability, not the build scope, so they must be excluded
  // explicitly.
  // (security-audit.md is a public document referenced by the nav and the
  // README docs table, so it is not on this list.)
  srcExclude: [
    '**/audit-*.md',
    '审计-文档一致性.md',
    // Use `**/review/**` rather than `review/**`: the zh mirror also has a
    // `zh/review/` page, whose entire content is a language entry pointing at
    // the English review pages — since the English version is excluded, this
    // one is excluded too.
    '**/review/**',
  ],

  // Bilingual site: root = English, Chinese under docs/zh/ → /zh/.
  // The theme auto-generates the switcher from locales and pairs pages by
  // relativePath (docs/zh/api.md ↔ docs/api.md), so no language links are
  // written by hand.
  locales: {
    root: {
      label: 'English',
      lang: 'en-US',
      description: 'Server registration, discovery and character directory infrastructure for online games',

      themeConfig: {
        nav: [
          { text: 'Quick Start', link: '/api-quickstart' },
          { text: 'Scenarios', link: '/scenarios' },
          {
            text: 'Integration',
            items: [
              { text: 'Server Info as Config', link: '/server-config' },
              { text: 'Server Lifecycle', link: '/lifecycle' },
              { text: 'Realm & Shard', link: '/realms-shards' },
              { text: 'Data Sync (Message Bus)', link: '/sync' },
              { text: 'Player Presence', link: '/presence' },
              { text: 'Config Center (Cross-Server)', link: '/config-center' },
              { text: 'APISIX Plugins', link: '/apisix' },
              { text: 'SDK (Go / C++ / Python / JS / Java / C#)', link: '/sdk-go' },
            ],
          },
          {
            text: 'Operations',
            items: [
              { text: 'Announcements & Planned Maintenance', link: '/operations' },
              { text: 'Merge / Transfer / Migration', link: '/migration' },
              { text: 'High Availability', link: '/ha' },
              { text: 'Deployment Topology', link: '/topology' },
              { text: 'Performance Design', link: '/performance' },
              { text: 'Benchmarks', link: '/benchmarks' },
              { text: 'Security', link: '/security' },
            ],
          },
          {
            text: 'Reference',
            items: [
              { text: 'Concepts', link: '/concepts' },
              { text: 'Architecture', link: '/architecture' },
              { text: 'API Reference', link: '/api' },
              { text: 'Data Model', link: '/data-model' },
              { text: 'Security Audit', link: '/security-audit' },
              { text: 'Roadmap', link: '/roadmap' },
              { text: 'GitHub Releases', link: 'https://github.com/cuihairu/atlas/releases' },
            ],
          },
        ],

        sidebar: [
          {
            text: 'Getting Started',
            items: [
              { text: 'Introduction', link: '/' },
              { text: 'Quick Start', link: '/api-quickstart' },
              { text: 'Scenarios', link: '/scenarios' },
              { text: 'Concepts', link: '/concepts' },
            ],
          },
          {
            text: 'Integration',
            items: [
              { text: 'Server Info as Config', link: '/server-config' },
              { text: 'Server Lifecycle', link: '/lifecycle' },
              { text: 'Realm & Shard', link: '/realms-shards' },
              { text: 'Data Sync', link: '/sync' },
              { text: 'Player Presence', link: '/presence' },
              { text: 'Config Center', link: '/config-center' },
              { text: 'APISIX Plugins', link: '/apisix' },
            ],
          },
          {
            text: 'SDK',
            items: [
              { text: 'Go SDK', link: '/sdk-go' },
              { text: 'C++ SDK', link: '/sdk-cpp' },
              { text: 'Python SDK', link: '/sdk-python' },
              { text: 'JavaScript SDK', link: '/sdk-js' },
              { text: 'Java SDK', link: '/sdk-java' },
              { text: 'C# SDK', link: '/sdk-csharp' },
            ],
          },
          {
            text: 'Operations',
            items: [
              { text: 'Announcements & Planned Maintenance', link: '/operations' },
              { text: 'Merge / Transfer / Migration', link: '/migration' },
              { text: 'High Availability', link: '/ha' },
              { text: 'Deployment Topology', link: '/topology' },
              { text: 'Performance Design', link: '/performance' },
              { text: 'Benchmarks', link: '/benchmarks' },
              { text: 'Security', link: '/security' },
            ],
          },
          {
            text: 'Reference',
            items: [
              { text: 'Architecture', link: '/architecture' },
              { text: 'API Reference', link: '/api' },
              { text: 'Data Model', link: '/data-model' },
              { text: 'Security Audit', link: '/security-audit' },
              { text: 'Roadmap', link: '/roadmap' },
            ],
          },
        ],

        outline: {
          level: [2, 3],
          label: 'On this page',
        },

        editLink: {
          pattern: 'https://github.com/cuihairu/atlas/edit/main/docs/:path',
          text: 'Edit this page on GitHub',
        },

        lastUpdated: {
          text: 'Last updated',
        },
      },
    },

    zh: {
      label: '简体中文',
      lang: 'zh-CN',
      description: '面向在线游戏的服务器注册、发现与角色目录基础设施',

      themeConfig: {
        nav: localize(zhNav, '/zh'),
        sidebar: localize(zhSidebar, '/zh'),

        outline: {
          level: [2, 3],
          label: '本页目录',
        },

        editLink: {
          pattern: 'https://github.com/cuihairu/atlas/edit/main/docs/:path',
          text: '在 GitHub 上编辑此页',
        },

        lastUpdated: {
          text: '最后更新',
        },
      },
    },
  },

  markdown: {
    // Line numbers for code blocks (highlighting is provided by VitePress's built-in shiki).
    lineNumbers: true,
  },

  themeConfig: {
    logo: '/logo.svg',

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
  },
})
