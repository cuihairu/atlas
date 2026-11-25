# Stage 1: Build the binary
FROM golang:1.27.2-alpine AS builder

WORKDIR /build

# Version info injected at build time (CI passes the commit SHA; local builds default to unknown).
# Note: .dockerignore excludes .git, so `git rev-parse` cannot be used here.
ARG GIT_COMMIT=unknown

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-X github.com/cuihairu/atlas/internal/version.GitCommit=${GIT_COMMIT} \
              -X github.com/cuihairu/atlas/internal/version.BuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -o /atlas ./cmd/atlas

# Demo fleet simulator (for demo deployments; see
# deployments/docker/docker-compose.deploy.yml): stands in for real game servers,
# continuously registering/heartbeating so the demo dashboard and server-picker
# pages have live data.
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-X github.com/cuihairu/atlas/internal/version.GitCommit=${GIT_COMMIT}" \
    -o /demoagents ./cmd/atlas-demoagents

# Stage 2: Minimal runtime image
# nonroot tag: runs as uid/gid 65534, not root.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /atlas /atlas
COPY --from=builder /demoagents /demoagents

# 8080 public(Discovery/Directory/Routing) · 8081 internal(Registry) · 8082 admin · 9090 gRPC
EXPOSE 8080 8081 8082 9090

# Container health check entry: distroless has no shell/curl, so the compose
# healthcheck calls `atlas healthcheck` directly (probes :8080 /healthz).
HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=3 \
    CMD ["/atlas", "healthcheck"]

ENTRYPOINT ["/atlas"]
