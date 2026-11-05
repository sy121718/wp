#!/usr/bin/env bash
# 写请求组合：成功动作使用节点选择器，JSON 对象提供器不与 multipart 混用。
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT" || exit 1

violations=0
checked=0
while IFS= read -r file; do
  [[ "${file#internal/templates/}" == "${1:-}"* ]] || continue
  checked=$((checked + 1))
  python3 scripts/write-combinations-scan.py "$file"
  [[ $? -ne 0 ]] && violations=$((violations + 1))
done < <(find internal/templates -type f \( -name '*.html' -o -name '*.jet' \) | sort)
if [[ $checked -eq 0 ]]; then
  printf '写请求组合未检查：没有模板；检查扫描范围。\n' >&2
  exit 1
fi

if [[ $violations -gt 0 ]]; then
  printf '写请求组合门禁失败：%d 个模板；见 internal/templates/CLAUDE.md「写操作」。\n' "$violations" >&2
  exit 1
fi
printf '写请求组合静态检查通过：%d 个模板（节点选择器与确定输入编码）。\n' "$checked"
