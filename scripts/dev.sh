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
#   - 是否在 air 重启时自动执行迁移由 database.run_migrations 控制；false 时不会隐式改库，
#     需使用管理连接手动执行 make migrate 或 go run ./cmd -migrate-only。
set -euo pipefail

cd "$(dirname "$0")/.."

# 站点公开根地址（含路径前缀）。影响 sitemap/robots、canonical、结构化数据、hreflang
# 与语言切换链接 —— 不设的话这些地方输出站内相对路径，本地看着正常、上线却是错的。
# 开发环境的访问面挂在 /site 前缀下，所以默认值带 /site；部署时按自己的域名覆盖。
# 站点基址 = 站点对外可访问的**根**地址。
#
# 不带 /site 前缀：访问面已挂在根上（站点独占域名根），canonical / og:url /
# sitemap / hreflang / 全部站内链接都从这个值派生 —— 带上前缀会让它们全部指向
# /site/...，而那是控制台内部的兼容入口，不是对外地址。
export WP_SITE_BASE_URL="${WP_SITE_BASE_URL:-http://127.0.0.1:8080}"

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
