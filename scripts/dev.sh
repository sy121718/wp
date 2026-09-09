#!/usr/bin/env bash
# scripts/dev.sh — 开发期热重载（air 托管 go build + 重启）
#
# 用法：
#   bash scripts/dev.sh              # 前台运行，Ctrl+C 退出
#   bash scripts/dev.sh > /tmp/gowp-air.log 2>&1 &   # 后台运行
#
# 说明：
#   - 自动定位 air：优先 PATH 中的 air，其次 $(go env GOPATH)/bin/air；
#     未安装时提示 go install github.com/air-verse/air@latest。
#   - 配置见项目根 .air.toml：Go/模板/config.yaml 改动自动重编译重启；
#     静态资源（/static 由 gin.Dir 直读文件系统）改完刷新浏览器即可。
set -euo pipefail

cd "$(dirname "$0")/.."

AIR_BIN="${AIR_BIN:-}"
if [ -z "$AIR_BIN" ]; then
    AIR_BIN="$(command -v air || true)"
fi
if [ -z "$AIR_BIN" ]; then
    GOBIN_DIR="$(go env GOPATH)/bin"
    if [ -x "$GOBIN_DIR/air" ]; then
        AIR_BIN="$GOBIN_DIR/air"
    fi
fi
if [ -z "$AIR_BIN" ] || [ ! -x "$AIR_BIN" ]; then
    echo "未找到 air，请先安装：go install github.com/air-verse/air@latest" >&2
    echo "（或设置 AIR_BIN 指向 air 可执行文件）" >&2
    exit 1
fi

exec "$AIR_BIN" -c .air.toml "$@"
