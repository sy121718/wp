#!/usr/bin/env bash
# check-route-registration-placement.sh — 路由注册只能写在 router 文件里（docs/02-X §8）。
#
# 为什么需要它：`.GET(` / `.POST(` 写在哪个文件里不影响任何东西 —— 编译通过、路由照常注册、
# 测试照常绿。于是「handler 文件里顺手注册一条路由」不会被任何人发现，直到下一次重构时
# 没人知道「这个路径是谁注册的」。2026-10-07 重新基线时实测还有 10 个非 router 文件在注册
# 路由（block_http.go 同时放装配与 10 个 handler、media/plugin/navigation/… 的 SetupXxxPages
# 住在 handler 文件里、product_page.go 里藏着两条翻译页路由）。
#
# 判据（按**文件名形状**，不按目录名）：
#   1. 扫描 internal/module/**/inbound/http/*.go（生产代码，跳过 _test.go）；
#   2. 文件名是 *_router.go 或 router*.go 的免检 —— 这是路由文件的命名约定
#      （`<模块>_router.go` = API 装配，`<模块>_page_router.go` = 后台页面装配）；
#   3. 其余文件里出现**非注释行**的 `.GET("` / `.POST("` 即失败，打印 文件:行；
#   4. 豁免清单 scripts/route-registration-allow.txt（格式：`<go 相对路径> TAB <理由>`），
#      条目**不再命中即失败** —— 只增不减的清单等于没有门禁（与
#      check-contract-deps.sh / check-no-internal-error-leak.sh 同规）。
#
# 纯文本启发式：只跳过 `//` 开头的行，不解析块注释 —— 宁可多报一行让人看一眼，
# 也不放过一条真实注册。
#
# 自带负向自检（照 check-no-internal-error-leak.sh 的做法）：临时造一个非 router 文件
# 写一条 `.GET("`，必须被本脚本拦下；拦不住说明判据退化成空转，此时**脚本自己判失败**。
#
# 退出码：0 通过 / 1 违规 / 2 环境或自检失败。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ALLOW_FILE="$ROOT/scripts/route-registration-allow.txt"
# 自检与单测可用环境变量指向临时目录，避免往真实仓库里塞样本文件。
SCAN_DIR="${ROUTE_PLACEMENT_SCAN_DIR:-$ROOT/internal/module}"

if [ ! -d "$SCAN_DIR" ]; then
  echo "扫描根目录不存在：$SCAN_DIR" >&2
  exit 2
fi

# scan_dir <root>：打印 <相对路径>:<行号>: <原文>，只认非注释行里的 .GET(" / .POST("。
scan_dir() {
  local root="$1"
  local dir file base
  while IFS= read -r dir; do
    for file in "$dir"/*.go; do
      [ -e "$file" ] || continue
      base="$(basename "$file")"
      case "$base" in
        *_test.go) continue ;;
        *_router.go|router*.go) continue ;;
      esac
      awk -v rel="${file#"$root"/}" '
        { trimmed = $0; sub(/^[ \t]+/, "", trimmed) }
        trimmed ~ /^\/\// { next }
        /\.(GET|POST)\("/ { printf "%s:%d: %s\n", rel, NR, $0 }
      ' "$file"
    done
  done < <(find "$root" -type d -path '*/inbound/http' | sort)
}

# 一个 inbound/http 目录都没枚举到 = 目录结构变了，门禁会静默缩水成空转。
if [ -z "$(find "$SCAN_DIR" -type d -path '*/inbound/http' -print -quit)" ]; then
  echo "没有枚举到任何 inbound/http 目录（$SCAN_DIR）：目录结构变了？" >&2
  exit 2
fi

# —— 负向自检：判据必须抓得住一个明显的坏样本 ——
selftest_dir="$(mktemp -d)"
trap 'rm -rf "$selftest_dir"' EXIT
mkdir -p "$selftest_dir/mod/inbound/http"
cat >"$selftest_dir/mod/inbound/http/bad_sample_handle.go" <<'GOEOF'
package badhttp

func SetupBad(rg any) {
	rg.GET("/bad", nil)
}
GOEOF
if [ -z "$(scan_dir "$selftest_dir")" ]; then
  echo "负向自检失败：造出来的 bad_sample_handle.go 没有被判据抓住（判据已退化成空转）" >&2
  exit 2
fi
# 正向自检：router 命名的文件必须免检，否则门禁会把正确写法也拦下。
mkdir -p "$selftest_dir/mod2/inbound/http"
cat >"$selftest_dir/mod2/inbound/http/mod2_router.go" <<'GOEOF'
package mod2http

func SetupMod2(rg any) {
	rg.GET("/ok", nil)
}
GOEOF
if [ -n "$(scan_dir "$selftest_dir/mod2")" ]; then
  echo "正向自检失败：*_router.go 被误判成违规（命名免检失效）" >&2
  exit 2
fi

# —— 读豁免清单 ——
declare -A ALLOWED=()
declare -A ALLOW_HIT=()
if [ -f "$ALLOW_FILE" ]; then
  while IFS=$'\t' read -r path reason; do
    case "${path:-}" in ''|'#'*) continue ;; esac
    if [ -z "${reason:-}" ]; then
      echo "豁免条目缺少理由（格式：<go 相对路径> TAB <理由>）：$path" >&2
      exit 2
    fi
    ALLOWED["$path"]=1
  done <"$ALLOW_FILE"
fi

mapfile -t RAW < <(scan_dir "$SCAN_DIR")
declare -a VIOLATIONS=()
for line in "${RAW[@]:-}"; do
  [ -n "$line" ] || continue
  file="${line%%:*}"
  if [ -n "${ALLOWED[$file]:-}" ]; then
    ALLOW_HIT["$file"]=1
    continue
  fi
  VIOLATIONS+=("$line")
done

status=0

for line in "${VIOLATIONS[@]:-}"; do
  [ -n "$line" ] || continue
  echo "✗ 路由注册出现在非 router 文件：$line" >&2
  status=1
done
if [ "${#VIOLATIONS[@]}" -gt 0 ]; then
  cat >&2 <<'TIP'

  修法：把注册语句搬进同目录的 <模块>_router.go（API）或 <模块>_page_router.go（后台页面），
  handler 留在原文件；装配函数（SetupXxxRoutes / SetupXxxPages）跟注册一起搬。
  确实需要留在原地的，登记进 scripts/route-registration-allow.txt 并写明理由。
  详见 docs/02-X-route-assembly-unification.md §4（目标形态）与 §8（本门禁）。
TIP
fi

# 曾登记、如今不再命中 = 清单退化成注释，同样判失败。
for path in "${!ALLOWED[@]}"; do
  if [ -z "${ALLOW_HIT[$path]:-}" ]; then
    echo "✗ 豁免清单条目不再命中（已修好或文件已改名，请删掉这一条）：$path" >&2
    status=1
  fi
done

if [ "$status" -eq 0 ]; then
  echo "✓ 路由注册落点正确（扫描 $(find "$SCAN_DIR" -type d -path '*/inbound/http' | wc -l | tr -d ' ') 个 inbound/http 目录）"
fi
exit "$status"
