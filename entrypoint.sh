#!/bin/sh

set -e

echo "🚀 Starting Quartz site..."

# Seed example content if empty
if [ -z "$(ls -A /app/content 2>/dev/null)" ]; then
  echo "📁 content/ is empty, creating a starter index.md"
  mkdir -p /app/content
  cat > /app/content/index.md <<'EOF'
---
title: "Atlas 知识库"
enableToc: true
---

# 欢迎使用 Atlas

这是一个兼容 Obsidian 的知识库，支持 `[[双向链接]]`、标签和悬浮预览。

在 Obsidian 中，将 `atlas/content` 作为 Vault，即可无缝编辑。

示例：链接到 [[关于|About]] 页面。

EOF
  cat > /app/content/about.md <<'EOF'
---
title: About
---

This is the About page. Back to [[index|Home]].

EOF
fi

# Build + serve
if [ "$NODE_ENV" = "production" ]; then
  echo "🏗️  Building (production)..."
  npx quartz build
  echo "🌐 Serving..."
  npx quartz serve --port=3000
else
  echo "🛠️  Dev mode: build & watch on :3000"
  npx quartz build --serve --port=3000
fi
