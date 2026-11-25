.PHONY: build run proto test test-coverage lint docker docker-compose docs docs-dev docs-build clean perf-runner-smoke

VERSION := v0.1.1
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD   := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X github.com/cuihairu/atlas/internal/version.GitCommit=$(COMMIT) \
           -X github.com/cuihairu/atlas/internal/version.BuildDate=$(BUILD)

build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/atlas ./cmd/atlas

run: build
	ATLAS_STORE=memory ./bin/atlas

# Regenerate api/pb (both the first comment in api/proto/atlas.proto and the
# generated file header say "make proto").
# The toolchain must match the committed file headers, or the diff carries
# version noise:
#   protoc 3.21.12 · protoc-gen-go v1.36.12
#     go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12
#   protoc-gen-go-grpc v1.6.2
#     go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.2
# The module= prefix strips the module path from go_package so output lands in
# api/pb/ (verified byte-for-byte identical to the committed files).
proto:
	protoc --proto_path=. \
		--go_out=module=github.com/cuihairu/atlas:. \
		--go-grpc_out=module=github.com/cuihairu/atlas:. \
		api/proto/atlas.proto

# ================= Local testing scope =================
# CI and local development split into three paths by change scope — a one-line
# docs edit should not trigger a full build:
#   1) Docs changes (*.md, docs/**, images) → no build. The main CI workflow's
#      paths-ignore already skips these paths; no need to run everything locally.
#   2) Code changes → run only the affected subset:
#      - `go test ./internal/<changed-package>/...`
#      - `go test ./sdk/<changed-package>/...`
#   3) Full run (make test) → only in these cases:
#      go.mod / go.sum / go.work, .github/**, Docker/Compose config, pre-release.
# ==========================================================================

test:
	go test -v -race -count=1 ./...

# ============== 98% coverage gate (2026-10-31) ==============
# Same scope as ci.yml's "Test with coverage": cross-coverage of ./internal/...
# (-coverpkg=./internal/...) + per-block dedup (keep the max count).
# internal/store/storetest is a test-support package and is excluded from the
# gate (its uncovered lines are t.Fatalf failure branches — honest coverage
# would mean the test failed; see todo.md).
# Prerequisites: SQL/Redis contract containers (without them the SQL suites
# skip, total coverage drops to ~82% and the gate fails):
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
	@dirs="$$(go list -f '{{.Dir}}' ./...)"; \
	if [ -z "$$dirs" ]; then \
		echo "go list returned no packages; refusing to run gofmt with no args"; \
		exit 1; \
	fi; \
	files="$$(gofmt -l $$dirs)"; \
	if [ -n "$$files" ]; then \
		echo "gofmt needed for:"; \
		echo "$$files"; \
		exit 1; \
	fi
	@command -v golangci-lint >/dev/null 2>&1 || { echo "golangci-lint not found: install the CI-pinned version — go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0"; exit 1; }
	@test "$$(golangci-lint version 2>/dev/null | grep -o 'version 2\.15\.0' | head -1)" = "version 2.15.0" || { echo "golangci-lint must be v2.15.0 (CI-pinned); have: $$(golangci-lint version 2>/dev/null | head -1) — go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0"; exit 1; }
	golangci-lint run ./...

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
