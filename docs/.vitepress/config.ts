import { defineConfig } from 'vitepress'
import { withMermaid } from 'vitepress-plugin-mermaid'

type MenuItem = { text: string; link?: string; items?: MenuItem[] }

// 中文页面全部位于 docs/zh/，因此 zh locale 的 nav / sidebar 只写中文文案，
// 站内链接前缀由 localize() 统一补 /zh；站外绝对地址（GitHub Releases）原样保留。
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

  // 双语站点：根语言（root）= English，中文在 docs/zh/ → /zh/。
  // 主题依据 locales 自动生成切换器，并按 relativePath 逐页对应
  // （docs/zh/api.md ↔ docs/api.md），无需手写语言链接。
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
    // 代码块行号（高亮由 VitePress 内置 shiki 提供）。
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
