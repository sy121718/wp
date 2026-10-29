#!/usr/bin/env bash
# 写表单的组合合法性（形状约束，不是属性名）。
#
# 为什么需要这道门禁：合法的属性组合只有几种，而**错的组合不会报错**——基座会照常提交，
# 只是行为不是写那个属性的人以为的那样。三条真实踩过的例子：
#
#   · bulk 表单没有 [name=ids] → 点「批量删除」提示「没勾选任何项」，永远发不出请求。
#   · data-write-outcome 写在非 bulk 表单上 → 基座忽略它（只对 bulk 有意义），
#     写的人以为能拿到逐条回执。
#   · data-write-nav 带 {id} 却用在 bulk 上 → 批量没有单一 id，{id} 被替成空串，
#     跳到一个不存在的地址。
#
# 判据：
#   1. data-write="bulk" 的表单/按钮所在表单里必须有 [name=ids] 或 [name="ids[]"]
#      （或声明了 data-write-ids-scope 指向别处）。
#   2. data-write-outcome 只能出现在 bulk 上。
#   3. data-write-nav 里的 {id} 只能配 create / update（有单一 id 的动作）。
#   4. data-write-json 点名的每个 name 都必须在同一表单里有对应的
#      <template name="…" data-row>（否则基座组装出空对象，字段静默丢失）。
#
# 退出码 0 = 通过；1 = 有违规。
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT" || exit 1

TPL_DIR="internal/templates/admin"
violations=0
fail() { printf '✗ %s\n' "$1" >&2; violations=$((violations + 1)); }

# 行勾选框常放在被 include 的片段里（block_row.html / product_row.html …），
# 单文件扫不到。先把「哪些模板里出现过 name="ids"」收集起来，判据 1 用得到。
IDS_FRAGMENTS="$(grep -rlE 'name="ids(\[\])?"' "$TPL_DIR" 2>/dev/null | tr '\n' ' ')"

while IFS= read -r file; do
  WB_IDS_FRAGMENTS="$IDS_FRAGMENTS" python3 scripts/write-combinations-scan.py "$file"
  [[ $? -ne 0 ]] && violations=$((violations + 1))
done < <(find "$TPL_DIR" -type f \( -name '*.html' -o -name '*.jet' \) | sort)

if [[ $violations -gt 0 ]]; then
  echo
  echo "写表单契约见 internal/templates/CLAUDE.md「写操作」。" >&2
  exit 1
fi
echo "✓ 写表单组合合法（bulk 有 ids / outcome 只配 bulk / nav 的 {id} 只配 create-update / json 有行模板）"
