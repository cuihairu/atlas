# Atlas 管理台镜像：node 构建静态产物 → nginx 托管。
# 同源直出：前端 API_BASE/ADMIN_BASE 均为空串，nginx 把 /v1/admin 反代到
# atlas 管理口、/v1 反代到公网口——前端无需知道后端地址，跨域不存在。
FROM node:22-alpine AS build
# buildx 多架构构建：目标架构（amd64/arm64），npm 包名里写作 x64/arm64。
ARG TARGETARCH
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
# npm/cli#4828：optional 原生依赖在 musl 上偶发被 npm ci 跳过，vite 8 的
# rolldown 找不到绑定就构建失败。按目标架构显式补装绑定（已装齐时为秒级
# no-op），让构建不依赖那个 bug 是否复现；架构不匹配时 npm 会 EBADPLATFORM。
RUN npm i --no-save "@rolldown/binding-linux-$(case "${TARGETARCH:-amd64}" in arm64) echo arm64;; *) echo x64;; esac)-musl"
COPY . .
RUN npm run build

FROM nginx:1.27-alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 80
