# Makefile — 常用命令入口（审计 OSS-012）。
#
# 为什么需要它：项目里散着 scripts/*.sh 与 ps1 两套脚本，新人要读一遍才知道
# 「跑起来」需要哪几条；而且有些脚本已经失效（build-all.ps1 在当时的环境里跑不通）。
# Makefile 把这些收成一张可发现的清单 —— `make` 不带参数就列出全部目标。
#
# 约定：`.PHONY` 全部声明；每条目标都能独立执行（不依赖上一条刚跑过）。

SHELL := /bin/bash
.DEFAULT_GOAL := help

# 数据库连接（与 docker-compose.yml、config.yaml 的默认值一致）
PGHOST ?= 127.0.0.1
PGPORT ?= 5432
PGUSER ?= root
PGPASSWORD ?= root
PGDATABASE ?= wp
export PGHOST PGPORT PGUSER PGPASSWORD PGDATABASE

.PHONY: help
help: ## 列出全部可用目标
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

.PHONY: up
up: ## 起本地依赖（PostgreSQL + Redis）
	docker compose up -d

.PHONY: down
down: ## 停依赖（保留数据卷）
	docker compose down

.PHONY: clean-data
clean-data: ## 停依赖并**删除数据卷**（会清空本地数据库）
	@echo '将删除本地数据库与 Redis 数据卷，5 秒内 Ctrl+C 可取消'; sleep 5
	docker compose down -v

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
check: lint ## 静态检查 + CI 同款门禁脚本
	scripts/check-no-internal-error-leak.sh
	scripts/check-i18n-coverage.sh
	scripts/check-service-db-boundary.sh

.PHONY: migrate
# -migrate-only：只执行结构迁移与业务 seed 后退出，不启动 HTTP 服务、不监听端口。
# 这个入口是 2026-09-16 补的 —— 此前没有独立迁移命令，migrate 只能「把依赖起到位、
# 让下一次启动自己把库迁好」，失败混在启动日志里；现在真跑一次迁移，成功与否直接
# 反映在退出码上（CI 也用它准备测试库）。
migrate: up ## 执行数据库迁移与 seed（幂等；先确保依赖已就绪）
	@echo '等待 PostgreSQL 就绪…'
	@for i in $$(seq 1 30); do \
		if docker compose exec -T postgres pg_isready -U $(PGUSER) -d $(PGDATABASE) >/dev/null 2>&1; then \
			echo '✓ PostgreSQL 已就绪，开始执行迁移与 seed'; exit 0; \
		fi; sleep 2; \
	done; echo '✗ PostgreSQL 未在 60 秒内就绪，请查看 docker compose logs postgres' >&2; exit 1
	go run ./cmd -migrate-only
