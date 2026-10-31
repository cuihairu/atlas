# Atlas Dashboard UI Audit — impeccable (2026-10-31)

审核面：`dashboard/`（React 19 + TS + Vite + antd 5，管理台：总览/服务器/负载热力图/角色搜索/迁移/公告与维护/跨服配置/排障诊断/系统配置/操作审计）。
方式：本地真实渲染 + Playwright/Chromium 截图；桌面 1440×900、移动 390×844（antd 深色档为主，含登录页与亮色抽查）。
截图目录：`~/.hermes/cache/scratch/ui-audit-screenshots/atlas/`（`login-desktop.png`、`overview.png`、`servers.png`、`heatmap[blank].png`、`characters.png`、`migrations.png`、`operations.png`、`crossserver.png`、`diagnose.png`、`system.png`、`audit.png`、`overview-mobile.png`、`servers-mobile.png`、`operations-mobile.png`、`diagnose-mobile.png`，及本地后端复验 `t1-servers-desktop.png`、`t2-servers-narrow.png`、`t2-overview-narrow.png`、`t3-404-desktop.png`）。
依据：无 `DESIGN.md` / `PRODUCT.md`（已按要求记录缺失），本次按 scoped audit 执行，不引入新视觉方向、不动品牌色（品牌橙 `#d97706` 见 #4）。

## 整体 critique（不留情面）

管理台的骨架是结实的：antd 5 主题令牌统一、深/浅色档走同一算法、布局是标准的 Sider+Header+Content 三明治，10 个管理页 + 登录页结构一致，表格/筛选/标签体系齐全——这是"内部运维工具"应有的样子。

但**移动端与窄屏是明显的欠账**：390px 下侧边栏仍保持 200px 全展开，把内容区挤到不足半个屏宽，表格列被压扁到不可读、页面出现横向溢出——这是"手机上看管理台"这一真实场景的直接失败，不是风格偏好。第二个硬伤是路由健壮性：demo 站上直接访问 `/heatmap`（或任何拼错的 URL）会落到一个**静默空白内容区**，没有任何 404 提示——用户不知道自己走错了路。这两处已在本次修复（见 #1/#2/#3）。

其余是零散的打磨项：品牌橙 `#d97706` 上白字只有 3.19:1（低于 AA 4.5:1，用于 primary 按钮与 Logo），以及 antd `message` 静态调用不感知主题上下文的控制台告警。前者是"品牌色 vs 可访问性"的取舍，按"不动品牌色"边界本次未动，**待拍板**。

另需留意（运维面）：demo 站 `atlas.cuihairu.site` 落后源码 163 个 commit，其 `deploy-docker.yml` 为手动触发而非 CI 自动——本次源码修复在重新部署前不会出现在 demo 站上。

## 问题清单（按严重度）

### 1. [高 · 响应式/布局破碎] 窄屏侧边栏不折叠，内容区被挤爆 ✅已修
- **现象**：390px 下 Sider 保持 200px 全展开，Content 区被压到不足 200px；筛选条、表格溢出，页面横向滚动。移动端无折叠交互，图标菜单不可用。
- **截图**：`servers-mobile.png`、`overview-mobile.png`（修复前形态）。
- **定位**：`src/components/Layout.tsx` `<Sider>`（无 `breakpoint`、无 `collapsedWidth`）、`<Content>`（固定 `margin/padding: 24`）。
- **修复**（commit `abe7355`）：Sider 加 `breakpoint="lg"` `collapsedWidth={80}`，窄屏自动折叠为 80px 图标栏；Content 内边距改 `Grid.useBreakpoint()` 驱动（`screens.md === false ? 12 : 24`），并注释说明 antd `useBreakpoint` 首帧为空对象 → 按桌面 24 处理避免闪烁。验证：`t2-servers-narrow.png` / `t2-overview-narrow.png` 侧边栏折叠为图标栏、无横向溢出（`pageOverflow: false`）。

### 2. [高 · 响应式/布局破碎] 宽表在窄屏被压扁/截断 ✅已修
- **现象**：固定多列表格（服务器/角色/迁移/审计/跨服/排障/系统配置等 18 处）在窄屏下列宽被压缩到不可读，或超出容器被截断，无横向滚动兜底。
- **截图**：`operations-mobile.png`、`diagnose-mobile.png`（修复前：列被挤压）。
- **定位**：`src/pages/*.tsx` 全部 `<Table>`（Servers / Characters / Migrations / AuditLog / CrossServer×5 / Operations×2 / PlayerDiagnose×2 / SystemConfig×3 / ServerDetail / operationsBatch）。
- **修复**（commit `5997ca6`）：统一 `scroll={{ x: 'max-content' }}`（无尾逗号，规避 oxlint 解析）。验证：`t1-servers-desktop.png` / `t2-servers-narrow.png` 表按自然列宽渲染、容器内横向滚动，不再挤压/截断。

### 3. [高 · 路由健壮性] 未知路由渲染静默空白内容区 ✅已修
- **现象**：demo 站直接访问 `/heatmap` 时内容区空白（bundle 分析：无对应路由与 i18n key，属陈旧构建而非源码缺陷）；任何拼错的 URL 都得到无提示的空白页，用户无从得知路由不存在。
- **截图**：`heatmap[blank].png`（空白形态）；修复后 `t3-404-desktop.png`。
- **定位**：`src/App.tsx`（无 catch-all 路由）、`src/i18n.ts`（缺 `notFound`/`backHome` 文案）。
- **修复**（commit `06e4a6b`）：新增 `<Route path="*">` 渲染 `Result status="404"`（"404 / 页面不存在或已被移除 / 返回总览"按钮），zh/en 双语文案补齐。验证：`t3-404-desktop.png` 完整 404 页。

### 4. [中 · a11y 对比度] 品牌橙 `#d97706` 上白字 3.19:1 — 待拍板
- **现象**：`ConfigProvider colorPrimary: '#d97706'`（primary 按钮"返回总览"等）与 Logo 上的白字 3.19:1 < 4.5:1（AA）。14px 按钮正文。
- **定位**：`src/App.tsx`（colorPrimary）、`src/components/Layout.tsx`（Logo）。
- **建议**：品牌色 vs 可访问性取舍。本次按"不动品牌色"边界**未改**；若要达标，需产品同意微调明度至 ≥4.5:1，或 primary 按钮文案加粗。**待用户拍板**。

### 5. [低 · 技术债] antd `message` 静态调用不感知主题上下文 — 待定
- **现象**：控制台多处 `Warning: [antd: message] Static function can not consume context like dynamic theme. Please use 'App' component instead.`（本次复验截图亦复现）。
- **定位**：`src/` 内使用 `message.xxx()` 静态调用的页面。
- **建议**：改用 `App.useApp()` 的 `message` 实例（`<AntApp>` 已在 `App.tsx` 挂载）。待定。

### 6. [信息] demo 站为陈旧构建 — 运维
- **现象**：`atlas.cuihairu.site` 落后源码 163 个 commit（bundle 分析佐证），不含上述修复；`deploy-docker.yml` 为 `workflow_dispatch` 手动触发，非 CI 自动部署。
- **建议**：属交付/运维面；建议把 demo 部署纳入自动流水线，并在重新部署后再核验 demo 站上的本次修复。

### 7. [信息] heatmap 数据页源级观察
- **现象**：demo 站 `/heatmap` 空白，经 bundle 分析确认为陈旧构建（非源码缺陷）；源码 `Heatmap.tsx` 渲染逻辑正常、无错误。复验本地后端下数据页可正常出图。

### 8. [信息] 设计权威缺失
- `DESIGN.md` / `PRODUCT.md` 不存在。本次按 scoped audit 处理；后续若做正式视觉升级，应先走 `impeccable init` 采集产品上下文再定方向。

## 结果

| 项 | 状态 |
| --- | --- |
| 问题总数 | 5（不含信息项） |
| 当场修复 | 3（#1 移动端侧边栏、#2 宽表窄屏、#3 未知路由） |
| 列入 TODO | 2（#4 品牌橙拍板、#5 antd message 上下文） |
| 信息项 | 3（#6 demo 陈旧构建、#7 heatmap 源级观察、#8 设计权威缺失） |
