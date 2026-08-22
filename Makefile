# ===== 变量 =====
APP            := novapanel
AGENT          := nova-agent
CLI            := novactl
VERSION        ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo v0.0.0-dev)
COMMIT         := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_TIME     := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS        := -s -w \
	-X 'main.version=$(VERSION)' \
	-X 'main.commit=$(COMMIT)' \
	-X 'main.buildTime=$(BUILD_TIME)'
BIN_DIR        := bin
DIST_DIR       := dist
PLATFORMS      := linux/amd64 linux/arm64
GOFLAGS        := -trimpath
PKG            := ./...
COVER_PROFILE  := coverage.out
SERVICE_PKGS   := ./internal/service/...

.DEFAULT_GOAL := help
.PHONY: help doctor install-tools proto web web-dev web-typecheck web-test build build-panel build-agent \
        build-cli run run-watch test test-cover test-golden lint fmt vet migrate \
        migrate-test check-errcode smoke deploy-test install-verify certs docker release audit clean tidy mocks swagger

# ===== 帮助与环境自检 =====
help: ## 打印所有目标
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-16s\033[0m %s\n",$$1,$$2}'

doctor: ## 校验本地工具链版本
	@go version && node -v && pnpm -v

install-tools: ## 安装开发所需 Go 工具
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.61.0

# ===== 前端 =====
web: ## 构建前端并输出到 internal/web/dist（Go embed 目录）
	cd web && pnpm install --frozen-lockfile && pnpm build
	@touch internal/web/dist/.gitkeep

web-typecheck: ## 前端类型检查
	cd web && pnpm typecheck

web-test: ## 前端单元测试
	cd web && pnpm test

web-dev: ## 启动前端开发服务
	cd web && pnpm dev

# ===== 后端构建 =====
build: build-panel build-agent build-cli ## 构建三个二进制（当前平台）

build-panel: ## 构建控制面（前端产物需先执行 make web）
	CGO_ENABLED=0 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(APP)   ./cmd/panel

build-agent: ## 构建被控端
	CGO_ENABLED=0 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(AGENT) ./cmd/agent

build-cli: ## 构建命令行
	CGO_ENABLED=0 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(CLI)   ./cmd/novactl

# ===== 交叉编译与发布 =====
release: web ## 交叉编译 amd64+arm64 并打包
	@rm -rf $(DIST_DIR) && mkdir -p $(DIST_DIR)
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		echo ">> building $$os/$$arch"; \
		for c in panel:$(APP) agent:$(AGENT) novactl:$(CLI); do \
			cmd=$${c%:*}; out=$${c#*:}; \
			CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
			go build $(GOFLAGS) -ldflags "$(LDFLAGS)" \
				-o $(DIST_DIR)/$$os-$$arch/bin/$$out ./cmd/$$cmd || exit 1; \
		done; \
		mkdir -p $(DIST_DIR)/$$os-$$arch/conf $(DIST_DIR)/$$os-$$arch/scripts $(DIST_DIR)/$$os-$$arch/deploy/systemd; \
		cp conf/panel.example.yaml $(DIST_DIR)/$$os-$$arch/conf/; \
		cp scripts/bh scripts/install.sh scripts/uninstall.sh $(DIST_DIR)/$$os-$$arch/scripts/; \
		cp deploy/systemd/novapanel.service $(DIST_DIR)/$$os-$$arch/deploy/systemd/; \
		chmod +x $(DIST_DIR)/$$os-$$arch/scripts/*; \
		COPYFILE_DISABLE=1 tar --no-xattrs -C $(DIST_DIR)/$$os-$$arch -czf \
			$(DIST_DIR)/$(APP)-$(VERSION)-$$os-$$arch.tar.gz . ; \
	done
	@cd $(DIST_DIR) && shasum -a 256 *.tar.gz > SHA256SUMS

# ===== 运行与调试 =====
run: ## 本地运行控制面（配置来自 conf/panel.yaml，可从 conf/panel.example.yaml 复制）
	NOVA_LOG_LEVEL=debug NOVA_LOG_CONSOLE=true go run ./cmd/panel -config ./conf/panel.yaml

certs: ## 生成开发自签证书
	./scripts/gen-dev-certs.sh ./.dev/certs

migrate: ## 执行数据库迁移
	go run ./cmd/novactl migrate up -c ./conf/panel.yaml

migrate-test: ## 三驱动迁移演练 up + down + up
	./scripts/migrate-test.sh

check-errcode: ## 校验错误码常量、statusMap、05 号文档三处一致
	./scripts/check-errcode.sh

smoke: ## 认证链路端到端冒烟（真实启动 panel，含 SPA 与刷新令牌轮换）
	./scripts/auth-smoke.sh

deploy-test: ## 部署脚本单测（install.sh / bh / systemd 模板的配置改写逻辑）
	./scripts/deploy-test.sh

install-verify: ## 带 systemd 的容器内真机演练安装/登录/bh/升级/卸载（需 Docker）
	./scripts/install-verify.sh

# ===== 质量 =====
fmt: ## 格式化
	gofmt -l -w .

vet: ## go vet
	go vet $(PKG)

lint: ## golangci-lint + 前端类型检查（前端尚未接入 ESLint）
	golangci-lint run --timeout 5m
	cd web && pnpm typecheck

test: ## 单元测试（race 检测）
	go test -race -count=1 -timeout 10m $(PKG)

test-golden: ## 仅跑错误码映射 golden 测试
	go test -run 'TestHTTPStatus' ./internal/pkg/errs/...

test-cover: ## service 层覆盖率门槛 70%
	go test -race -coverprofile=$(COVER_PROFILE) -covermode=atomic $(SERVICE_PKGS)
	@go tool cover -func=$(COVER_PROFILE) | tail -1 | \
		awk '{gsub("%","",$$3); if ($$3+0 < 70) {print "覆盖率不足 70%: "$$3"%"; exit 1} else print "覆盖率 "$$3"% 通过"}'

audit: ## 依赖漏洞与过期检查
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

tidy: ## 依赖整理与校验
	go mod tidy && go mod verify

clean: ## 清理构建产物（保留 embed 占位 .gitkeep）
	rm -rf $(BIN_DIR) $(DIST_DIR) $(COVER_PROFILE) web/dist
	find internal/web/dist -mindepth 1 ! -name .gitkeep -delete
