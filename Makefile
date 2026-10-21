.PHONY: build run test lint docker docker-compose docs docs-dev docs-build clean perf-runner-smoke

VERSION := v0.1.1
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD   := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X github.com/cuihairu/atlas/internal/version.GitCommit=$(COMMIT) \
           -X github.com/cuihairu/atlas/internal/version.BuildDate=$(BUILD)

build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/atlas ./cmd/atlas

run: build
	ATLAS_STORE=memory ./bin/atlas

# ================= 本地测试口径 (local testing scope) =================
# CI 与本地开发按改动面三路分流——改一行文档不该全量编译：
#   1) 文档改动（*.md、docs/**、图片）→ 不编译。主 CI workflow 的 paths-ignore
#      已跳过这些路径；本地无需全量跑。
#   2) 代码改动 → 只跑改动子集：
#      - `go test ./internal/<改动包>/...`
#      - `go test ./sdk/<改动包>/...`
#   3) 全量（make test）→ 仅以下情形触发：
#      go.mod / go.sum / go.work、.github/**、Docker/Compose 配置、发布前。
# ==========================================================================

test:
	go test -v -race -count=1 ./...

lint:
	go vet ./...

docker:
	docker build -t atlas:$(VERSION) -f deployments/docker/Dockerfile .

docker-compose:
	docker compose -f deployments/docker/docker-compose.yml up --build

docs:
	cd docs && npm install

docs-dev: docs
	cd docs && npm run dev

docs-build: docs
	cd docs && npm run build

clean:
	rm -rf bin/ docs/node_modules/ docs/.vitepress/dist/ docs/.vitepress/cache/

# Workstation-sized proof that the §9 runner, topology and report pipeline work.
perf-runner-smoke:
	./deploy/perf/run.sh --scale smoke
