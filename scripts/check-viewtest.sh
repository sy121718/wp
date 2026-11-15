#!/usr/bin/env bash
# 视图集成测试：真实 Jet 渲染作用域、失败出口、后台公共上下文与写入口。
# 不连接数据库；真实浏览器用例由 GOWP_HTML_UI_BROWSER=1 显式开启。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT" || exit 1

go test ./public/test/viewtest/... -count=1
