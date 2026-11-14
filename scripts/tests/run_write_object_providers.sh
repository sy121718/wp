#!/usr/bin/env bash
# JSON 对象提供器的真实执行对账（需要契约生成物，不需要数据库）。
#
# 每个 provider 都必须绑定到真实写端点与 DTO 字段表；脚本真实执行 provider，
# 把产出的顶层键与 DTO 比对，并把通过结果写成台账，供字段门禁摘出该端点。
set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT" || exit 1

if ! command -v node >/dev/null 2>&1; then
  echo "✗ node 不可用：无法执行对象提供器对账" >&2
  exit 1
fi

CONTRACTS="internal/templates/static/js/ui/write-contracts.json"
LEDGER="internal/templates/static/js/ui/write-object-providers.json"
if [[ ! -s "$CONTRACTS" ]]; then
  echo "✗ 写端点契约缺失或为空：$CONTRACTS（先跑 go run ./cmd/write-contracts）" >&2
  exit 1
fi

node scripts/write-object-keys.js "$CONTRACTS" "$LEDGER"
