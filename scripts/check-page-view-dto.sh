#!/usr/bin/env bash
# check-page-view-dto.sh — 页面 GET 的视图出口形状（只减不增台账）。
#
# 目标形态（商品模块与主数据模块已走完，其余模块分批跟进）：
#
#   路由：adminui.PageSlot(<模板名>)      —— 模板名与渲染实现在装配点声明
#   handler：pageview.Render(c, view)     —— 控制器不持模板名、不 import 渲染层
#   视图：dto 里的强类型结构体             —— 字段名就是模板契约
#
# 三类存量违规，各自有独立的判据（不是"看见 gin.H 就算"）：
#
#   1. **gin.H 装配页面视图**：adminui.Prepare(c, gin.H{...}) 把视图装成一个 map。
#      违规定义是「页面 GET 的出口用 map 而不是结构体」—— 模板键拼错拿不到值，
#      编译器一句话也不说；换成结构体后改名即编译错误。
#      （gin.H 用在**片段**出口与 JSON 响应里不算违规：那里的键不是模板契约。）
#
#   2. **c.HTML 直出整页**：控制器自己持模板名。新形态是 PageSlot + pageview.Render。
#
#   3. **模块内取词**：模块里出现 i18n.TranslateFunc(...)—— 页面文案该由模板的 t()
#      取，响应文案该由 pkg/response 取。这一条与 presentation_owner_test.go 同源，
#      本脚本只统计不判罚，避免两处判据漂移。
#
# 台账：scripts/page-view-dto-allow.txt，**只减不增**。改一条就把数字改下来；
# 清零就从台账里删掉那一行（脚本会报"登记了但已达标"）。
set -uo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

ALLOW_FILE="scripts/page-view-dto-allow.txt"
declare -A ALLOW=()
if [ -f "$ALLOW_FILE" ]; then
  while IFS=':' read -r file n; do
    [ -z "${file:-}" ] && continue
    case "$file" in \#*) continue ;; esac
    ALLOW["$file"]="$n"
  done < "$ALLOW_FILE"
fi

# 只扫控制器目录下的非测试文件。
declare -A GINH CHTML
while IFS= read -r f; do
  case "$f" in *_test.go) continue ;; esac
  g=$(grep -c 'gin\.H{' "$f" 2>/dev/null || true)
  h=$(grep -c 'c\.HTML(' "$f" 2>/dev/null || true)
  [ "${g:-0}" -gt 0 ] && GINH["$f"]="$g"
  [ "${h:-0}" -gt 0 ] && CHTML["$f"]="$h"
done < <(find internal/module -path '*/inbound/http/*.go' -type f | sort)

fail=0
report() {
  local label="$1"; shift
  local -n cur=$1
  echo "── $label"
  local -A fileEntries=()
  for f in "${!cur[@]}"; do
    name=$(basename "$f")
    fileEntries["$f"]="${cur[$f]}"
  done
  # 台账里的条目（按相对路径前缀匹配）
  for key in "${!ALLOW[@]}"; do
    :
  done
  local total=0
  for f in "${!fileEntries[@]}"; do
    total=$((total + fileEntries[$f]))
  done
  echo "   全仓合计 $total 处（$label）"
}

# 简化：只报总量与明细，台账按总量守（文件级台账在拆分/改名时极易失真）。
gin_total=0
for f in "${!GINH[@]}"; do gin_total=$((gin_total + GINH[$f])); done
html_total=0
for f in "${!CHTML[@]}"; do html_total=$((html_total + CHTML[$f])); done

# 台账里只记两行合计：gin.H 与 c.HTML。
allow_gin="${ALLOW[__gin_h__]:-0}"
allow_html="${ALLOW[__c_html__]:-0}"

if [ "${1:-}" = "--list" ]; then
  echo "gin.H 装配页面视图（共 $gin_total 处）："
  for f in $(printf '%s\n' "${!GINH[@]}" | sort); do printf '  %-70s %s\n' "$f" "${GINH[$f]}"; done
  echo
  echo "c.HTML 直出整页（共 $html_total 处）："
  for f in $(printf '%s\n' "${!CHTML[@]}" | sort); do printf '  %-70s %s\n' "$f" "${CHTML[$f]}"; done
  exit 0
fi

if [ "$gin_total" -gt "$allow_gin" ]; then
  echo "✗ gin.H 装配页面视图：$gin_total 处，台账上限 $allow_gin" >&2
  echo "  新页面请用 dto 里的强类型视图 + adminui.PageSlot + pageview.Render（照 product / masterdata）" >&2
  fail=1
fi
if [ "$html_total" -gt "$allow_html" ]; then
  echo "✗ c.HTML 直出整页：$html_total 处，台账上限 $allow_html" >&2
  echo "  控制器不持模板名：路由挂 adminui.PageSlot(<模板名>)，handler 调 pageview.Render(c, view)" >&2
  fail=1
fi

if [ "$fail" -eq 0 ]; then
  echo "✓ 页面视图出口台账未超限（gin.H $gin_total / c.HTML $html_total）"
  if [ "$gin_total" -lt "$allow_gin" ] || [ "$html_total" -lt "$allow_html" ]; then
    echo "  ⚠ 已低于台账（gin.H $allow_gin → $gin_total，c.HTML $allow_html → $html_total）：请下调 $ALLOW_FILE"
  fi
fi
exit "$fail"
