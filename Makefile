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
test: ## 全量测试（feature + unit；-p 8 并发，实测 271s）
	go test -p 8 ./... -count=1

.PHONY: test-short
test-short: ## 快速测试（不依赖数据库的包）
	go test ./pkg/... ./internal/builder/... ./internal/pipeline/... -count=1

.PHONY: lint
lint: ## 格式与静态检查
	gofmt -l internal/ pkg/ public/
	go vet ./...

.PHONY: check
check: lint ## 静态检查 + CI 同款门禁脚本
	scripts/check-no-internal-error-leak.sh
	scripts/check-i18n-coverage.sh

.PHONY: migrate
# 迁移与 seed 随进程启动自动执行（cmd/main.go → routers → migrations.RunSeeds），
# 没有独立的 migrate 子命令，所以这里不做假动作：目标是「把依赖起到位，让下一次启动
# 自己把库迁好」，并对尚未就绪的依赖给出明确等待，而不是让 go run 抛一个连接错误。
migrate: up ## 起依赖并等待就绪（迁移随应用启动自动执行）
	@echo '等待 PostgreSQL 就绪…'
	@for i in $$(seq 1 30); do \
		if docker compose exec -T postgres pg_isready -U $(PGUSER) -d $(PGDATABASE) >/dev/null 2>&1; then \
			echo '✓ PostgreSQL 已就绪，启动应用时会自动跑迁移与 seed'; exit 0; \
		fi; sleep 2; \
	done; echo '✗ PostgreSQL 未在 60 秒内就绪，请查看 docker compose logs postgres' >&2; exit 1
