# Stage 1: Build the binary
FROM golang:1.27-alpine AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-X github.com/cuihairu/atlas/internal/version.GitCommit=$(git rev-parse --short HEAD) \
              -X github.com/cuihairu/atlas/internal/version.BuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -o /atlas ./cmd/atlas

# Stage 2: Minimal runtime image
FROM gcr.io/distroless/static-debian12

COPY --from=builder /atlas /atlas

EXPOSE 8080

ENTRYPOINT ["/atlas"]