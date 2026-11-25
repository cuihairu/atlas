# Atlas dashboard image: node builds static assets → nginx serves them.
# Same-origin by default: the frontend API_BASE/ADMIN_BASE are empty strings and
# nginx reverse-proxies /v1/admin to the atlas admin port and /v1 to the public
# port — the frontend never needs the backend address, so there is no CORS.
FROM node:22-alpine AS build
# buildx multi-arch build: target architectures (amd64/arm64), written as x64/arm64 in npm package names.
ARG TARGETARCH
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
# npm/cli#4828: optional native dependencies are occasionally skipped by npm ci
# on musl, and vite 8's rolldown fails the build when it cannot find the
# binding. Install the binding explicitly for the target architecture (a
# second-level no-op when already present) so the build does not depend on
# whether that bug reproduces; npm reports EBADPLATFORM on arch mismatch.
RUN npm i --no-save "@rolldown/binding-linux-$(case "${TARGETARCH:-amd64}" in arm64) echo arm64;; *) echo x64;; esac)-musl"
COPY . .
RUN npm run build

FROM nginx:1.27-alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 80
