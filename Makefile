# Makefile — 常用命令入口（审计 OSS-012）。
#
# 为什么需要它：项目里散着 scripts/*.sh 与 ps1 两套脚本，新人要读一遍才知道
# 「跑起来」需要哪几条；而且有些脚本已经失效（build-all.ps1 在当时的环境里跑不通）。
# Makefile 把这些收成一张可发现的清单 —— `make` 不带参数就列出全部目标。
#
# 约定：`.PHONY` 全部声明；每条目标都能独立执行（不依赖上一条刚跑过）。

SHELL := /bin/bash
.DEFAULT_GOAL := help

# 本机 PostgreSQL 连接；开发依赖由本机服务管理，不通过 Docker 启停。
# 本机迁移通常使用管理员角色：可通过 make migrate PGUSER=... PGPASSWORD=... 覆盖。
PGHOST ?= 127.0.0.1
PGPORT ?= 5432
PGUSER ?= $(USER)
PGPASSWORD ?=
PGDATABASE ?= wp
export PGHOST PGPORT PGUSER PGPASSWORD PGDATABASE

.PHONY: help
help: ## 列出全部可用目标
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

.PHONY: dev
dev: ## 开发模式（air 热重载）
	bash scripts/dev.sh

.PHONY: build
build: ## 编译整个项目
	go build ./...

.PHONY: run
run: build ## 编译并直接运行（无热重载）
	go run ./cmd

.PHONY: test
# 全量测试并发度。同机实测（16 核 / 147 个包全绿）：-p 8 = 271s、-p 16 = 227s、-p 24 = 205s。
# 收益递减的原因不在核数：并发越高单个包耗时越被争用放大（product/feature 从 57s 涨到 171s），
# 总时长已由「最慢的那个包」决定，跑长尾时 CPU 空闲率能到 76%。
# 上限是 PG 的 max_connections=100（-p 24 时连接峰值 51）。要更快得砍最慢包，不是加并发。
TEST_PARALLEL ?= $(shell nproc)
test: ## 全量测试（feature + unit；默认按 CPU 核数并发，可用 TEST_PARALLEL=8 覆盖）
	go test -p $(TEST_PARALLEL) ./... -count=1

.PHONY: test-short
test-short: ## 快速测试（不依赖数据库的包；与 CI 的 unit job 同覆盖面）
	go test ./pkg/... ./internal/... ./cmd/... ./config/... ./public/migrations/... -count=1

.PHONY: lint
lint: ## 格式与静态检查
	gofmt -l internal/ pkg/ public/ cmd/ config/
	go vet ./...

.PHONY: check
check: lint ## 静态检查 + 门禁（scripts/check-all.sh 的统一入口，无外部依赖那一组）
	bash scripts/check-all.sh

.PHONY: check-db
check-db: ## 只跑需要 PostgreSQL 的门禁（须先 make migrate；CI 的 integration job 同款）
	bash scripts/check-all.sh --db-only

.PHONY: check-all
check-all: ## 门禁全量（无依赖组 + 数据库组）
	bash scripts/check-all.sh --with-db

.PHONY: test-race
test-race: ## 竞态检测（CI 目前无 -race，这是手动入口：核心五域）
	go test -race -p 4 ./pkg/... ./internal/pipeline/... ./internal/builder/... \
		./internal/module/publication/... ./internal/module/presentation/... -count=1 -timeout 20m

.PHONY: migrate
# -migrate-only：只执行结构迁移与业务 seed 后退出，不启动 HTTP 服务、不监听端口。
# 开发环境使用本机 PostgreSQL/Redis；本目标只检查本机 PostgreSQL，不依赖 Docker。
# migrate 走**管理连接**（超级用户）执行 DDL，因此这里把 RLS 角色探针关掉：
# `require_rls_role=true` 是**服务侧**的门禁（连进去的业务角色不许绕过 RLS），而迁移
# 与运维脚本用的是同一个 database 组件、却必须用超级用户跑 DDL —— 探针在这个场景没有
# 意义（迁移不读业务数据、不受 RLS 约束）。依据见 pkg/database/rls_probe.go 的注释：
# 「默认 require_rls_role=false 是刻意的：迁移与运维脚本用管理连接（超级用户）执行 DDL」。
# **只影响这一条命令**：服务的启动路径与 database.run_migrations 的语义都不动。
# 绝不要用「给应用角色加 DDL 权限」来解决 —— 那是把工程隔离拆掉换方便。
migrate: ## 执行数据库迁移与 seed（幂等；需要管理连接）
	@echo '等待 PostgreSQL 就绪（$(PGHOST):$(PGPORT)）…'
	@for i in $$(seq 1 30); do \
		if pg_isready -h $(PGHOST) -p $(PGPORT) -U $(PGUSER) -d $(PGDATABASE) >/dev/null 2>&1; then \
			echo '✓ PostgreSQL 已就绪，开始执行迁移与 seed'; break; \
		fi; \
		if [ $$i -eq 30 ]; then echo '✗ PostgreSQL 未在 60 秒内就绪，请检查本机 PostgreSQL 服务' >&2; exit 1; fi; \
		sleep 2; \
	done
	GOWP_DATABASE_HOST='$(PGHOST)' \
	GOWP_DATABASE_PORT='$(PGPORT)' \
	GOWP_DATABASE_USER='$(PGUSER)' \
	GOWP_DATABASE_DBNAME='$(PGDATABASE)' \
	$(if $(strip $(PGPASSWORD)),GOWP_DATABASE_PASSWORD='$(PGPASSWORD)') \
	GOWP_DATABASE_REQUIRE_RLS_ROLE=false \
	go run ./cmd -migrate-only
