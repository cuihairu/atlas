# Stage 1: Build the binary
FROM golang:1.27-alpine AS builder

WORKDIR /build

# 构建时注入的版本信息（CI 传 commit SHA；本地构建缺省 unknown）。
# 注意：.dockerignore 排除了 .git，不能用 git rev-parse 取。
ARG GIT_COMMIT=unknown

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-X github.com/cuihairu/atlas/internal/version.GitCommit=${GIT_COMMIT} \
              -X github.com/cuihairu/atlas/internal/version.BuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -o /atlas ./cmd/atlas

# 演示舰队模拟器（demo 部署用，见 deployments/docker/docker-compose.deploy.yml）：
# 代替真人游戏服持续注册/心跳，让演示环境的管理台与选服页有活数据。
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-X github.com/cuihairu/atlas/internal/version.GitCommit=${GIT_COMMIT}" \
    -o /demoagents ./cmd/atlas-demoagents

# Stage 2: Minimal runtime image
# nonroot 标签：以 uid/gid 65534 运行，不用 root。
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /atlas /atlas
COPY --from=builder /demoagents /demoagents

# 8080 公网(Discovery/Directory/Routing) · 8081 内网(Registry) · 8082 管理端 · 9090 gRPC
EXPOSE 8080 8081 8082 9090

# 容器健康检查入口：distroless 没有 shell/curl，
# compose 的 healthcheck 直接调 `atlas healthcheck`（探测 :8080 /healthz）。
HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=3 \
    CMD ["/atlas", "healthcheck"]

ENTRYPOINT ["/atlas"]
