#!/usr/bin/env bash
# 控件层不得依赖传输层（htmx）。
#
# 为什么需要这道门禁：UI 控件是「界面行为」，htmx 是「传输层」。控件直接调 htmx API
# 的后果不是洁癖问题，是两条实际故障：
#
#   1. **层次倒置**：任何不用 htmx 的页面（访问面按能力裁剪脚本）都会因为
#      `htmx is not defined` 而让整个控件崩掉 —— 而它原本只是「这个交互不生效」。
#   2. **换传输层要改所有控件**：drawer.js 曾为「模板克隆后让 hx-* 生效」直接调
#      `htmx.process(body)`；product-category-tree.js 直接调 `htmx.ajax('GET', …)`。
#      换任何方案都要动它们。
#
# 本项目踩过的具体形态：
#   · drawer.js 的 htmx.process —— 已改为广播 `wb:rescan`，由 ui/htmx-bridge.js 翻译
#   · product-category-tree.js 的 htmx.ajax —— 已改为模板声明 hx-get + hx-trigger 防抖
#   · index.js 的 htmx:after:swap 重扫 —— 已改为 MutationObserver（htmx 事件仅作可选加速）
#
# 判据：ui/*.js（排除 htmx.min.js 与 htmx-bridge.js 这一个适配层）里
#   **不得出现 htmx API 调用**（htmx.process / htmx.ajax / htmx.on / htmx.trigger /
#   htmx.find / htmx.remove / defineExtension / registerExtension）。
#
# 注释里提到 htmx 是允许的（解释历史与原因）。
#
# 退出码 0 = 通过；1 = 有违规。
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT" || exit 1

UI_DIR="internal/templates/static/js/ui"
violations=0

# 允许直接调 htmx API 的文件（**适配层**，不是控件）。加文件要写理由。
#
# htmx-bridge.js  控件广播 wb:rescan → htmx.process（控件与传输层的通用适配）
# json-swap.js    写动作的局部刷新协议：服务端回 JSON，本文件按 JSON 里的 html
#                 换入目标。它注册 htmx 扩展是**传输层实现细节** —— 页面只写
#                 hx-ext="json-swap"，控件层（select/drawer/modal…）完全不知情。
ALLOW="htmx-bridge.js json_swap.js"

for f in "$UI_DIR"/*.js; do
  base="$(basename "$f")"
  [[ "$base" == "htmx.min.js" ]] && continue
  skip=0
  for ok in $ALLOW; do [[ "$base" == "$ok" ]] && skip=1; done
  [[ $skip -eq 1 ]] && continue

  # 去掉注释再判（注释里说明 htmx 是允许的）。
  hits="$(python3 - "$f" <<'PY'
import re, sys
src = open(sys.argv[1], encoding='utf-8').read()
# 去 /* … */ 与 // … 注释
src = re.sub(r'/\*.*?\*/', '', src, flags=re.S)
src = re.sub(r'(?m)^\s*//.*$', '', src)
pats = [
    r'\bhtmx\.(process|ajax|on|off|trigger|find|findAll|closest|remove|addClass|removeClass|toggleClass|takeClass|swap|values|parseInterval|defineExtension|registerExtension)\b',
]
out = []
for pat in pats:
    for m in re.finditer(pat, src):
        line = src[:m.start()].count('\n') + 1
        out.append(f'{line}:{m.group(0)}')
print('\n'.join(out))
PY
)"
  if [[ -n "$hits" ]]; then
    while IFS= read -r hit; do
      [[ -z "$hit" ]] && continue
      printf '✗ %s:%s：控件直接调 htmx API —— 控件层不得依赖传输层\n' "$base" "$hit" >&2
      violations=$((violations + 1))
    done <<< "$hits"
  fi
done

# 反向自检：白名单里的适配层必须真的存在（拼错名字会让它静默变成「全部放行」）。
for ok in $ALLOW; do
  if [[ ! -f "$UI_DIR/$ok" ]]; then
    printf '✗ 白名单里的适配层 %s 不存在 —— 拼错会让门禁静默放行所有文件\n' "$ok" >&2
    violations=$((violations + 1))
  fi
done

if [[ $violations -gt 0 ]]; then
  echo
  echo "控件与传输层的边界见 internal/templates/CLAUDE.md 与 ui/htmx-bridge.js 文件头。" >&2
  exit 1
fi
printf '✓ 控件层零 htmx 依赖（适配层 %s 除外）\n' "$ALLOW"
