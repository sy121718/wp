#!/usr/bin/env bash
# check-page-view-dto.sh — 页面 GET 的视图出口形状（只减不增台账，文件级）。
#
# 目标形态（product / masterdata 已走完）：
#
#   路由：adminui.PageSlot(<模板名>)      —— 模板名与渲染实现在装配点声明
#   handler：pageview.Render(c, view)     —— 控制器不持模板名、不 import 渲染层
#   视图：dto 里的强类型结构体             —— 字段名就是模板契约
#
# ## 为什么是**文件级**台账而不是全局总量
#
# 第一版只守两个全局数字（gin.H 总数、c.HTML 总数）。它能挡住"整体变多"，但挡不住
# 最要紧的一类漂移：**A 文件少写 5 处、B 文件多写 5 处** —— 总量不变，而新代码已经
# 搬到不该去的地方。总量是收敛节奏的度量，文件级是"不要往新文件里搬"的守卫，
# 两者不是一回事，缺了后者这个门禁就只是个计数器。
#
# 台账格式（scripts/page-view-dto-allow.txt）：
#
#   <文件相对路径> gin=<n> html=<n>     —— 该文件当前容许的 gin.H / c.HTML 数量
#   --total gin=<n> html=<n>            —— 全仓总量上限（收敛节奏的度量）
#
# 判据（三条，全过才绿）：
#   1. 每个文件的 gin.H / c.HTML **不得超过**台账里它自己的数（往已知文件里加也拦）；
#   2. 不在台账里的文件出现 gin.H / c.HTML → 红灯（新文件不许引入）；
#   3. 全仓总量不得超过 --total（整体只能往下走）。
#
# 收敛方式：一个模块一个模块迁。每迁完一处，把对应行的数字改下来；清零就删掉那一行。
#
# 判据只认「页面 GET 的出口」：gin.H 用在片段出口与 JSON 响应里不算违规
# （那里的键不是模板契约），所以扫描范围限定在 inbound/http 的控制器文件。
set -uo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

ALLOW_FILE="scripts/page-view-dto-allow.txt"

if [ ! -f "$ALLOW_FILE" ]; then
  echo "✗ 缺少台账 $ALLOW_FILE（本门禁的全部判据都在那张表里）" >&2
  exit 1
fi

# ── 读台账 ────────────────────────────────────────────────────────────────────
declare -A FILE_GIN FILE_HTML
TOTAL_GIN=0
TOTAL_HTML=0
while IFS= read -r line; do
  case "$line" in ''|'#'*) continue ;; esac
  # --total gin=N html=M
  if [[ "$line" == --total* ]]; then
    for kv in $line; do
      case "$kv" in
        gin=*)  TOTAL_GIN="${kv#gin=}" ;;
        html=*) TOTAL_HTML="${kv#html=}" ;;
      esac
    done
    continue
  fi
  # <path> gin=N html=M
  path=$(echo "$line" | awk '{print $1}')
  [ -n "$path" ] || continue
  for kv in $line; do
    case "$kv" in
      gin=*)  FILE_GIN["$path"]="${kv#gin=}" ;;
      html=*) FILE_HTML["$path"]="${kv#html=}" ;;
    esac
  done
done < "$ALLOW_FILE"

# ── 扫描现状（只看非测试的控制器文件）────────────────────────────────────────
#
# **只数「页面视图」的 gin.H，不数 JSON 响应里的**。
#
# 这一点最初写错过：判据只按 `gin.H{` 行数算，于是 ai_handle.go 的 21 处
# JSON-RPC 载荷（writeRPCResult / response.Success / c.JSON）全被算进「页面视图存量」，
# 而那批根本不渲染页面 —— 台账里挂着一批永远「迁不掉」的数，看着像永远做不完。
#
# 判据：**按语句**判定，不按行。
#
# 第一版是单行判定（同一行里有没有 JSON 出口的函数名），并注明了「跨行时会被算进来
# （宁可多算，不要漏算页面视图）」。那个保守取法的代价后来变成了实质问题：台账里
# 挂着一批跨行写的 JSON-RPC 载荷（`writeRPCResult(c, id, gin.H{ … })` 跨七八行），
# 它们永远迁不掉 —— 数字看着像永远做不完，而实际页面视图早就清零了。
#
# 所以改成按语句判定：从一行开始累加括号深度，回到零就是一条语句结束。
# 整条语句里出现过 JSON 出口函数，其中的 gin.H 一律不计入页面视图。
# 这不是「放宽标准」，而是把判据对准它本来要衡量的东西：**这条 gin.H 是不是要交给模板**。
#
# 另外两类也不计入（同样不是页面出口）：空字面量 gin.H{}，以及 []gin.H / map[string]gin.H 容器。
declare -A CUR_GIN CUR_HTML
gin_sum=0
html_sum=0
while IFS= read -r f; do
  case "$f" in *_test.go) continue ;; esac
  read -r g h < <(python3 scripts/page-view-scan.py "$f")
  g=${g:-0}; h=${h:-0}
  [ "$g" -gt 0 ] && { CUR_GIN["$f"]="$g"; gin_sum=$((gin_sum + g)); }
  [ "$h" -gt 0 ] && { CUR_HTML["$f"]="$h"; html_sum=$((html_sum + h)); }
done < <(find internal/module -path '*/inbound/http/*.go' -type f | sort)

if [ "${1:-}" = "--list" ]; then
  echo "gin.H 装配页面视图（共 $gin_sum 处，台账上限 $TOTAL_GIN）："
  for f in $(printf '%s\n' "${!CUR_GIN[@]}" | sort); do printf '  %-72s %s\n' "$f" "${CUR_GIN[$f]}"; done
  echo
  echo "c.HTML 直出整页（共 $html_sum 处，台账上限 $TOTAL_HTML）："
  for f in $(printf '%s\n' "${!CUR_HTML[@]}" | sort); do printf '  %-72s %s\n' "$f" "${CUR_HTML[$f]}"; done
  exit 0
fi

fail=0

# ── 判据 1 + 2：逐文件 ────────────────────────────────────────────────────────
for f in $(printf '%s\n' "${!CUR_GIN[@]}" "${!CUR_HTML[@]}" | sort -u); do
  g="${CUR_GIN[$f]:-0}"
  h="${CUR_HTML[$f]:-0}"
  allow_g="${FILE_GIN[$f]:-}"
  allow_h="${FILE_HTML[$f]:-}"

  # 判据 2：不在台账里的文件不许出现。
  if [ -z "$allow_g" ] && [ -z "$allow_h" ]; then
    echo "✗ 新文件引入了页面视图存量：$f（gin.H=$g c.HTML=$h）" >&2
    echo "  新页面请用 dto 强类型视图 + adminui.PageSlot + pageview.Render（照 product / masterdata）" >&2
    fail=1
    continue
  fi
  # 判据 1：不得超过它自己的额度。
  if [ "$g" -gt "${allow_g:-0}" ]; then
    echo "✗ $f：gin.H 从 ${allow_g:-0} 涨到 $g" >&2
    fail=1
  fi
  if [ "$h" -gt "${allow_h:-0}" ]; then
    echo "✗ $f：c.HTML 从 ${allow_h:-0} 涨到 $h" >&2
    fail=1
  fi
done

# ── 判据 3：全仓总量 ──────────────────────────────────────────────────────────
if [ "$gin_sum" -gt "$TOTAL_GIN" ]; then
  echo "✗ 全仓 gin.H 总量 $gin_sum，台账上限 $TOTAL_GIN" >&2
  fail=1
fi
if [ "$html_sum" -gt "$TOTAL_HTML" ]; then
  echo "✗ 全仓 c.HTML 总量 $html_sum，台账上限 $TOTAL_HTML" >&2
  fail=1
fi

# ── 判据 4：双向精确匹配（与其它预算门禁同形）────────────────────────────────
#
# 高于台账判失败（涨了），低于台账也判失败（减了没记账）。
# 只判"不超限"会让台账变成一句注释：迁移做完之后台账还挂在 257，下一个人读到的
# 是"这里还有 257 处待迁"，而实际是 15 处 —— 过期台账比没有台账更坏。
if [ "$fail" -eq 0 ]; then
  if [ "$gin_sum" -lt "$TOTAL_GIN" ]; then
    echo "✗ 全仓 gin.H 已清到 $gin_sum 处，台账还是 $TOTAL_GIN；请把 $ALLOW_FILE 的 --total 改成 $gin_sum" >&2
    fail=1
  fi
  if [ "$html_sum" -lt "$TOTAL_HTML" ]; then
    echo "✗ 全仓 c.HTML 已清到 $html_sum 处，台账还是 $TOTAL_HTML；请把 $ALLOW_FILE 的 --total 改成 $html_sum" >&2
    fail=1
  fi
  # 逐文件同理：某个文件已经减下来了，它那一行的数字必须跟着改。
  for f in "${!FILE_GIN[@]}"; do
    cur="${CUR_GIN[$f]:-0}"
    if [ "$cur" -lt "${FILE_GIN[$f]}" ]; then
      echo "✗ $f：台账写 gin=${FILE_GIN[$f]}，实际已降到 $cur —— 请同步下调该行" >&2
      fail=1
    fi
  done
fi

if [ "$fail" -eq 0 ]; then
  echo "✓ 页面视图出口台账精确匹配（gin.H $gin_sum · c.HTML $html_sum）"
fi
exit "$fail"
