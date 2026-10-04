#!/usr/bin/env bash
# check-contract-deps.sh — 契约包的依赖方向（AGENTS.md 不变量 7、docs/01-overview.md §5）。
#
# 为什么需要一条脚本而不是靠评审记：不变量 7 把「契约包不得反向 import builder/core」
# 写成死线，但这条死线此前只活在文档里 —— 契约包多 import 一个 builder 子包，本模块
# 照样编译得过（编译期谁都发现不了），直到某天 builder/core 想持有这个契约时才炸成
# import cycle，而那时改动面已经摊到全仓。
#
# 判据（命中即违规）：对每个 internal/module/*/contract 包跑 `go list -deps`，输出里
# 出现下列任一包即违规：
#     go_wp/internal/builder/core
#     go_wp/internal/builder/style
#     go_wp/internal/builder/plugincomp
#
# 契约包只允许依赖 go_wp/internal/builder/source（纯数据形状，自身零 builder 内部依赖）。
#
# 为什么用 `-deps`（传递闭包）而不是 `go list -f '{{.Imports}}'`：直接依赖与「经别的包
# 绕一层的间接依赖」是同一类越界 —— plugin/contract 并不直接 import builder/core，它
# 是经 builder/plugincomp 把 core 拖进来的；只查直接 import 等于给「多绕一层」留后门。
#
# 豁免清单：scripts/contract-deps-allow.txt（每行 `包路径  # 理由`）。开源阶段把既有
# 违例登记成「可核对的技债」，而不是把判据改宽；条目一旦不再命中，脚本失败并要求删条目。
#
# 用法：bash scripts/check-contract-deps.sh
# 退出码：0 = 无白名单外违规；1 = 有违规（或豁免清单存在过期条目）；2 = 环境/清单/编译问题。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT" || exit 1

TARGET="internal/module"
ALLOW_FILE="scripts/contract-deps-allow.txt"
# 末尾的 $ 是必要的：没有它，将来出现 builder/corex 之类的包会被误判成 builder/core。
FORBIDDEN='^go_wp/internal/builder/(core|style|plugincomp)$'

if [ ! -d "$TARGET" ]; then
  echo "找不到目标目录：$TARGET" >&2
  exit 2
fi
if [ ! -f "$ALLOW_FILE" ]; then
  echo "找不到豁免清单：$ALLOW_FILE" >&2
  exit 2
fi
if ! command -v go >/dev/null 2>&1; then
  echo "找不到 go，无法解析契约包依赖图。" >&2
  exit 2
fi

# —— 读豁免清单。格式错、理由为空、重复登记都算脚本自身错误（退出码 2）——
# 理由为空是硬错误：登记一条「不知道为什么留着」的技债，等于把它变成永久设计。
declare -A ALLOWED=()
line_no=0
while IFS= read -r raw_line || [ -n "$raw_line" ]; do
  line_no=$((line_no + 1))
  line="${raw_line%$'\r'}"
  trimmed="${line#"${line%%[![:space:]]*}"}"   # 去前导空白
  if [ -z "$trimmed" ]; then continue; fi
  if [[ "$trimmed" == \#* ]]; then continue; fi

  key="${line%%#*}"; reason="${line#*#}"
  key="$(printf '%s' "$key" | sed 's/[[:space:]]*$//')"
  reason="$(printf '%s' "$reason" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')"
  if [ -z "$key" ] || [ -z "$reason" ]; then
    echo "✗ $ALLOW_FILE 第 $line_no 行格式不对（需要「<包路径>  # <理由>」，理由不得为空）：$line" >&2
    exit 2
  fi
  if [ -n "${ALLOWED[$key]:-}" ]; then
    echo "✗ $ALLOW_FILE 第 $line_no 行重复登记：$key" >&2
    exit 2
  fi
  ALLOWED["$key"]="$reason"
done < "$ALLOW_FILE"

echo "== 契约包依赖方向（禁止：builder/core | builder/style | builder/plugincomp）=="
echo

checked=0
failed=0
declare -a MATCHED=()
declare -a VIOLATIONS=()

for pkgdir in "$TARGET"/*/contract; do
  if [ ! -d "$pkgdir" ]; then continue; fi
  checked=$((checked + 1))

  # 逐包跑 go list：编译不过时依赖图不可信，直接报环境错误（2），
  # 不能降级成「没命中」——那会把「不知道」伪装成「通过」。
  if ! deps="$(go list -deps "./$pkgdir" 2>&1)"; then
    echo "✗ $pkgdir：go list -deps 失败 —— 契约包当前编译不过，依赖方向无法判定。" >&2
    printf '%s\n' "$deps" | sed 's/^/    /' >&2
    echo "  先让工作区编译通过再跑本门禁（退出码 2 表示环境/编译问题，不是依赖方向违规）。" >&2
    exit 2
  fi

  hits="$(printf '%s\n' "$deps" | rg -e "$FORBIDDEN" | sort -u || true)"
  if [ -z "$hits" ]; then continue; fi

  if [ -n "${ALLOWED[$pkgdir]:-}" ]; then
    MATCHED+=("$pkgdir")
    echo "⚠ $pkgdir — 已登记（待修）：${ALLOWED[$pkgdir]}"
    printf '%s\n' "$hits" | sed 's/^/    ↳ /'
  else
    VIOLATIONS+=("$pkgdir")
    failed=1
    echo "✗ $pkgdir — 违规"
    printf '%s\n' "$hits" | sed 's/^/    ↳ /'
  fi
done

# 豁免条目必须仍然命中：不再命中说明技债已还清（或契约被改名/删除），清单要跟着收口。
declare -a STALE=()
for key in $(printf '%s\n' "${!ALLOWED[@]}" | sort); do
  hit=0
  if (( ${#MATCHED[@]} > 0 )); then
    for m in "${MATCHED[@]}"; do
      if [ "$m" = "$key" ]; then hit=1; break; fi
    done
  fi
  if (( hit == 0 )); then STALE+=("$key"); fi
done

echo
if (( ${#VIOLATIONS[@]} > 0 )); then
  echo "✗ 契约包依赖方向违规（${#VIOLATIONS[@]} 个）：${VIOLATIONS[*]}" >&2
  echo "  契约包只能依赖 internal/builder/source（纯数据形状）；core / style / plugincomp" >&2
  echo "  会把 builder/core 一起拖进契约层，反向即成型环（AGENTS.md 不变量 7）。" >&2
  echo "  待修但暂不修的，写进 $ALLOW_FILE 并写明理由。" >&2
fi
if (( ${#STALE[@]} > 0 )); then
  echo "✗ 豁免清单里这些条目已经不再命中（类型已下沉 / 契约被改名或删除）：" >&2
  printf '%s\n' "${STALE[@]}" | sort | sed 's/^/    /' >&2
  echo "  请从 $ALLOW_FILE 删掉 —— 技债还清了就要收口，清单只增不减会掩盖回归。" >&2
  failed=1
fi
if (( failed != 0 )); then
  exit 1
fi

echo "✓ 契约包依赖方向无违规（检查 $checked 个契约包，豁免登记 ${#ALLOWED[@]} 条）"
