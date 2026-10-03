# Atlas 管理台镜像：node 构建静态产物 → nginx 托管。
# 同源直出：前端 API_BASE/ADMIN_BASE 均为空串，nginx 把 /v1/admin 反代到
# atlas 管理口、/v1 反代到公网口——前端无需知道后端地址，跨域不存在。
FROM node:22-alpine AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM nginx:1.27-alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 80
