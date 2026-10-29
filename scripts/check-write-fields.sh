#!/usr/bin/env bash
# 写表单的字段名对账：模板里的 name="X" 必须出现在目标 DTO 的字段表里。
#
# 为什么需要这道门禁：字段名写错的表现是**静默丢弃** —— 模板写 name="sort"、
# DTO 叫 sortOrder，两边都不报错，服务端收到一个它不认识的键（严格白名单下整条 400，
# 宽松绑定下**静默忽略**），用户看到「保存成功但没改」。
#
# 数据来源：`cmd/write-contracts` 生成物（internal/templates/static/js/ui/write-contracts.json）。
# 它把「端点 → DTO → 字段表」这条链固定下来，DTO 一侧由 form tag 决定
#（批次 2 已给全部写 DTO 补齐，99 条存量登记已清零）。
#
# 判据：
#   1. 每个写表单的字段名必须在它 data-write-url 对应 DTO 的字段表里；
#   2. DTO 上标了 binding:"required" 的字段必须出现在表单里。
#
# 已知豁免（写进豁免表，必须逐条写理由）：
#   · 端点不在契约文件里（如极少数用路径参数而非请求体的写端点）—— 逐条登记。
#
# 退出码 0 = 通过；1 = 有对不上的字段。
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT" || exit 1

CONTRACTS="internal/templates/static/js/ui/write-contracts.json"
if [[ ! -f "$CONTRACTS" ]]; then
  echo "✗ 找不到写端点契约文件 $CONTRACTS —— 先跑 go run ./cmd/write-contracts" >&2
  exit 1
fi

python3 scripts/write-fields-scan.py "$CONTRACTS" internal/templates/admin
