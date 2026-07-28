.PHONY: wire build build-embed test-unit test-integration test-e2e test-cover-integration test-hot-deploy

VERSION ?= $(shell ./scripts/resolve-version.sh)
LDFLAGS ?= -s -w -X main.Version=$(VERSION)

wire:
	@echo "生成 Wire 代码..."
	@cd cmd/server && go generate
	@echo "Wire 代码生成完成"

build:
	@echo "构建后端（不嵌入前端）..."
	@go build -ldflags="$(LDFLAGS)" -o bin/server ./cmd/server
	@echo "构建完成: bin/server"

build-embed:
	@echo "构建后端（嵌入前端）..."
	@go build -tags embed -ldflags="$(LDFLAGS)" -o bin/server ./cmd/server
	@echo "构建完成: bin/server (with embedded frontend)"

test-unit:
	@go test -tags unit ./... -count=1

test-integration:
	@go test -tags integration ./... -count=1 -race -parallel=8

test-e2e:
	@echo "运行 E2E 测试（需要本地服务器运行）..."
	@go test -tags e2e ./internal/integration/... -count=1 -v

test-cover-integration:
	@echo "运行集成测试并生成覆盖率报告..."
	@go test -tags=integration -cover -coverprofile=coverage.out -count=1 -race -parallel=8 ./...
	@go tool cover -func=coverage.out | tail -1
	@go tool cover -html=coverage.out -o coverage.html
	@echo "覆盖率报告已生成: coverage.html"

test-hot-deploy:
	@bash tests/test-hot-deploy.sh all
	@bash -n hot-deploy.sh lib/hot-deploy-common.sh tests/test-hot-deploy.sh
	@if command -v shellcheck >/dev/null 2>&1; then \
		shellcheck hot-deploy.sh lib/hot-deploy-common.sh tests/test-hot-deploy.sh tests/fixtures/fake-bin/docker tests/fixtures/fake-bin/curl; \
	else \
		echo "shellcheck not installed; skipped"; \
	fi

clean-coverage:
	@rm -f coverage.out coverage.html
	@echo "覆盖率文件已清理"

clean: clean-coverage
	@rm -rf bin/
	@echo "构建产物已清理"
