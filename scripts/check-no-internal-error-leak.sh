#!/usr/bin/env bash
# check-no-internal-error-leak.sh — 禁止后台页面直出内部错误（审计 CQ-009）。
#
# 为什么需要一条脚本而不是靠人记：这个模式写起来太顺手了 ——
# `c.String(500, err.Error())` 一行搞定，评审时也「看起来没问题」。
# 但它把表名、SQL 片段、路径直接铺在页面上，而后台不是可信边界。
#
# 判据只有一条：dashboard 的 handler 里出现「输出调用的实参里带 err.Error()」。
# 命中即失败，并打印出来让人改用 pageError / pageErrorBadRequest。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TARGET="$ROOT/internal/module/dashboard/inbound/http"

if [ ! -d "$TARGET" ]; then
  echo "找不到目标目录：$TARGET" >&2
  exit 2
fi

HITS=$(grep -rn --include='*.go' -E '(c\.String|response\.ErrorWithMessage)\([^)]*\.Error\(\)' "$TARGET" | grep -v '_test.go' | grep -vE '^[^:]+:[0-9]+:[[:space:]]*//' || true)

if [ -n "$HITS" ]; then
  echo "✗ 后台页面直出了内部错误（会泄露表名 / SQL / 路径）：" >&2
  echo "$HITS" >&2
  echo "" >&2
  echo "  改用 internal/module/dashboard/inbound/http/page_error.go 的" >&2
  echo "  pageError(c, scene, err) / pageErrorBadRequest(c, scene, err)：" >&2
  echo "  详情记日志，对外只给 dashboardenums.MsgInternalError。" >&2
  exit 1
fi

echo "✓ 未发现直出内部错误的调用点"
