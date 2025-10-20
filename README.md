# Atlas 知识库（Obsidian 兼容 + 双向链接）

基于 Quartz v4 的个人知识库脚手架，原生支持 Obsidian 风格的 `[[wikilinks]]`、反向链接（Backlinks）与站点图谱（Graph）。作为上级仓库的 git submodule 使用。

- 子模块路径：`atlas/`
- 内容目录：`atlas/content/`（可直接作为 Obsidian Vault）
- 预览端口：`http://localhost:8080`

## 快速开始

前置：已安装 Docker 与 docker-compose。

- 启动开发服务（首次会构建镜像）：
  - `cd atlas && docker-compose up -d --build`
- 查看日志：
  - `docker-compose logs -f`
- 停止服务：
  - `docker-compose down`
- 访问页面：
  - 浏览器打开 `http://localhost:8080`

服务启动后，编辑 `content/` 下的 Markdown 会自动生效（Quartz dev 模式自动 rebuild）。

## Obsidian 使用建议

- 将 `atlas/content` 作为 Vault 根目录
- 设置：
  - Editor: Use Wikilinks（启用维基链接）
  - New link format: Relative path to file
  - 自动更新内部链接：开启
- 目录建议：
  - `content/notes`、`content/projects`、`content/technologies`、`content/experiences`、`content/attachments`
- Frontmatter 示例：
  ```yaml
  ---
  id: 20241020-1430
  title: 双向连接实践
  tags: [zettel, linking]
  created: 2024-10-20
  updated: 2024-10-20
  ---
  ```

## 目录结构

```
atlas/
├── content/           # 笔记内容（Obsidian Vault）
├── static/            # 静态资源（图片等）
├── Dockerfile         # Quartz 容器构建
├── docker-compose.yml # 本地预览
├── entrypoint.sh      # 启动脚本（build + serve）
├── quartz.config.ts   # Quartz 站点配置（已启用反向链接 + 图谱）
├── quartz.layout.ts   # 页面布局（右侧 Backlinks / Graph）
└── README.md
```

## 常用命令

- 构建与启动：`docker-compose up -d --build`
- 停止：`docker-compose down`
- 查看日志：`docker-compose logs -f`
- 进入容器：`docker-compose exec quartz sh`
- 手动构建静态站点：`docker-compose run --rm quartz npx quartz build`
  - 构建产物在容器内 `/app/public`，如需导出可用 `docker cp` 或在 compose 里将 `public/` 做绑定卷

### 更新 Quartz 版本

- 编辑 `Dockerfile` 的 `ARG QUARTZ_REF`，替换为所需 tag/commit（例如 `v4.5.2`）
- GitHub Actions 工作流会在 `deploy.yml` 中使用同样的 `QUARTZ_REF` 构建并部署（已同步到 `v4.5.2`）
- 重新构建本地容器预览：`docker-compose build --no-cache && docker-compose up -d`

## 子模块（submodule）管理

本项目作为上级仓库的子模块使用，以下是常见操作：

- 首次拉取子模块：
  - 在上级仓库根目录执行：`git submodule update --init --recursive`
- 更新子模块到远端最新提交：
  - `cd atlas && git pull origin main`
  - 回到上级仓库根目录：`git add atlas && git commit -m "chore(submodule): bump atlas"`
- 在子模块内提交变更并推送：
  - `cd atlas`
  - `git checkout -b feature/xxx`（可选）
  - `git add . && git commit -m "feat: 初始 Quartz 脚手架"`
  - `git push origin HEAD`
- 在上级仓库记录子模块指针：
  - 回到上级仓库：`git add atlas && git commit -m "chore: update atlas submodule ref"`

提示：子模块是独立仓库，需分别在子模块与上级仓库各自提交一次。

## 部署（可选）

静态化：`npx quartz build` 会生成 `public/`，可部署到任意静态托管。

### GitHub Pages（已内置工作流）

本仓库已自带 `.github/workflows/deploy.yml`，推送到 `main` 将自动构建并发布到 GitHub Pages：

1) 在 GitHub → Settings → Pages：Build and deployment 选择 `GitHub Actions`
2) 推送到 `main` 分支后，Actions 会构建到 `build/public` 并发布
3) 访问：https://cuihairu.github.io/atlas

注意：`quartz.config.ts` 中已设置 `baseUrl: "https://cuihairu.github.io/atlas"`，便于生成正确的 canonical/sitemap。

### 手动导出

- 使用容器构建产物：
  - `docker-compose run --rm quartz npx quartz build`
  - 进入容器查看：`docker-compose run --rm quartz sh -lc 'ls -la public'`

## 故障排查

- 8080 端口被占用：修改 `docker-compose.yml` 暴露端口
- 更换镜像源：移除 `Dockerfile` 中的 `npm config set registry` 或替换为你的私有 registry
- 反向链接/图谱不显示：确认 `quartz.layout.ts` 中右侧栏已包含 `Backlinks()` 与 `Graph()`，并且笔记之间存在 `[[wikilinks]]`

---

维护者：@cuihairu
