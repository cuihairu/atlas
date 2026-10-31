.PHONY: build run test test-coverage lint docker docker-compose docs docs-dev docs-build clean perf-runner-smoke

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

# ============== 98% 覆盖率门禁 (coverage gate, 2026-10-31) ==============
# 口径与 ci.yml「Test with coverage」一致：./internal/... 交叉覆盖
# (-coverpkg=./internal/...) + 按块去重（保留最大计数）。
# internal/store/storetest 为测试支撑包，从门禁口径剔除（其未覆盖行是
# t.Fatalf 失败分支——诚实覆盖即测试失败；见 todo.md）。
# 前置：SQL/Redis 契约容器（无则 SQL 套件 skip，总覆盖约 82%，门禁必挂）：
#   docker run -d --name atlas-cov-pg   -p 15432:5432 -e POSTGRES_USER=atlas \
#     -e POSTGRES_PASSWORD=atlas -e POSTGRES_DB=atlas_test postgres:16-alpine
#   docker run -d --name atlas-cov-mysql -p 13307:3306 -e MYSQL_USER=atlas \
#     -e MYSQL_PASSWORD=atlas -e MYSQL_DATABASE=atlas_test -e MYSQL_ROOT_PASSWORD=root mysql:8.0
#   docker run -d --name atlas-cov-redis -p 16380:6379 redis:7-alpine
# ========================================================================
COVERAGE_GATE := 98

test-coverage:
	@test -n "$$ATLAS_TEST_DATABASE_URL" && test -n "$$ATLAS_TEST_MYSQL_DSN" && test -n "$$ATLAS_TEST_REDIS_URL" || { \
	  echo "ERROR: ATLAS_TEST_DATABASE_URL / ATLAS_TEST_MYSQL_DSN / ATLAS_TEST_REDIS_URL 未设置（见上方容器说明）"; exit 2; }
	go test ./internal/... -count=1 -coverprofile=coverage.raw -coverpkg=./internal/...
	@awk 'NR==1 {next} { \
	  if (!($$1 in stmt)) {order[++n]=$$1; stmt[$$1]=$$2; cnt[$$1]=0} \
	  if ($$3+0 > cnt[$$1]) cnt[$$1]=$$3 \
	} END { \
	  print "mode: set"; \
	  for (i=1;i<=n;i++) print order[i] " " stmt[order[i]] " " cnt[order[i]] \
	}' coverage.raw > coverage.out
	@awk -v gate=$(COVERAGE_GATE) 'NR==1 {next} { \
	  split($$1, parts, ":"); if (parts[1] ~ /\/storetest\//) next; \
	  tot += $$2; if ($$3+0 == 0) miss += $$2 \
	} END { \
	  pct = 100*(tot-miss)/tot; \
	  printf "coverage: %.2f%% (%d/%d statements, excl storetest) — gate %d%%\n", pct, tot-miss, tot, gate; \
	  if (pct + 0.0001 < gate) { printf "FAIL: coverage below gate\n"; exit 1 } \
	}' coverage.out
	@rm -f coverage.raw coverage.out

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
