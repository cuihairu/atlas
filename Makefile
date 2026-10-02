.PHONY: build run test lint docker docker-compose docs docs-dev docs-build clean

VERSION := v0.1.1
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD   := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X github.com/cuihairu/atlas/internal/version.GitCommit=$(COMMIT) \
           -X github.com/cuihairu/atlas/internal/version.BuildDate=$(BUILD)

build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/atlas ./cmd/atlas

run: build
	ATLAS_STORE=memory ./bin/atlas

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