#!/usr/bin/env bash
# 工作台完整检查入口：Node 仅用于开发验证，不参与生产资产构建。
set -euo pipefail
cd "$(dirname "$0")/.."
if ! command -v node >/dev/null 2>&1; then
    echo "缺少 Node，请通过 vfox 准备测试运行时；工作台检查不能跳过。" >&2
    exit 1
fi
export GOWP_REQUIRE_NODE=1
go run ./cmd/workbench-contracts -check
go test -count=1 ./internal/templates ./internal/builder/... ./internal/module/workbench/inbound/http ./public/test/workbench/feature
