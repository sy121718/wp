#!/usr/bin/env bash
# 工作台完整检查入口：Node 仅用于开发验证，不参与生产资产构建。
#
# 定向执行：本脚本不再无条件跑整个模板/构建大包。
#   --contracts   只跑生成契约检查（go run ./cmd/workbench-contracts -check）
#   --go          只跑工作台 Go 用例
#   --node        只跑 Node 契约用例
#   默认          三者都跑（等价于原行为，但目标缺失会明确报错）
set -euo pipefail
cd "$(dirname "$0")/.."
if ! command -v node >/dev/null 2>&1; then
    echo "缺少 Node，请通过 vfox 准备测试运行时；工作台检查不能跳过。" >&2
    exit 1
fi

# 检查目标必须真实存在：路径搬走后不能让检查静默通过。
require_dir() {
    if [[ ! -d "$1" ]]; then
        echo "✗ 工作台检查目标不存在：$1（包移动后必须同步更新本脚本）" >&2
        exit 1
    fi
}
for dir in cmd/workbench-contracts internal/module/workbench/inbound/http public/test/workbench/feature; do
    require_dir "$dir"
done

export GOWP_REQUIRE_NODE=1

mode="${1:-all}"
case "$mode" in
    --contracts|--go|--node|all) ;;
    *) echo "未知参数：$mode（可用 --contracts / --go / --node）" >&2; exit 1 ;;
esac

if [[ "$mode" == "--contracts" || "$mode" == "all" ]]; then
    go run ./cmd/workbench-contracts -check
fi

if [[ "$mode" == "--go" || "$mode" == "all" ]]; then
    go test -count=1 ./internal/module/workbench/inbound/http ./public/test/workbench/feature
fi

if [[ "$mode" == "--node" || "$mode" == "all" ]]; then
    go test -count=1 ./internal/templates ./internal/builder/...
fi
