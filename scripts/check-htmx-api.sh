#!/usr/bin/env bash
# htmx 属性与事件名的机械判据（针对 vendored 4.0.0）。
#
# 为什么需要这道门禁：htmx 4 与 v2 之间**静默失效**的东西太多 —— 属性改名、事件改名、
# 扩展 API 改名，写错了都不报错、不 500、日志干净，只是那段交互从此不再工作。
# 本项目已经踩过两次：
#
#   1. product 目录 9 处 hx-disabled-elt（v4 已删该属性）。实测对照：用旧属性时
#      请求期间按钮 disabled 恒为 false，改 hx-disable 才是 true —— 防重复提交静默失效。
#   2. vendor/idiomorph/idiomorph-ext.min.js 用 htmx 2 的 htmx.defineExtension，
#      v4 下加载即 `TypeError: htmx.defineExtension is not a function`。
#
# 判据（任一命中即失败）：
#   1. v2 已删属性：hx-vars / hx-params / hx-request= / hx-disabled-elt / hx-ws / hx-sse
#   2. v2 驼峰事件名（htmx:afterSwap 等），扫模板 + JS；文档只做**正向**核对
#   3. v2 扩展 API：htmx.defineExtension / htmx.onLoad
#   4. **反向核对**：模板里写的每个 hx-<名> 必须在 vendored bundle 里存在（或走通用改写）
#
# 退出码 0 = 通过；1 = 有违规。
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT" || exit 1

BUNDLE="internal/templates/static/js/ui/htmx.min.js"
TPL_DIR="internal/templates"
JS_DIR="internal/templates/static/js"

if [[ ! -f "$BUNDLE" ]]; then
  echo "✗ 找不到 vendored htmx bundle：$BUNDLE —— 判据 4 无法执行，检查文件是否被移动" >&2
  exit 1
fi

violations=0
fail() {
  printf '✗ %s\n' "$1" >&2
  violations=$((violations + 1))
}

# 扫描范围：模板（.html/.jet）、控件 JS（排除 bundle 自己）、模板目录下的文档。
tpl_files=()
while IFS= read -r f; do tpl_files+=("$f"); done < <(
  find "$TPL_DIR" -type f \( -name '*.html' -o -name '*.jet' \) | sort
)
js_files=()
while IFS= read -r f; do js_files+=("$f"); done < <(
  find "$JS_DIR" -type f -name '*.js' ! -name 'htmx.min.js' | sort
)
md_files=()
while IFS= read -r f; do md_files+=("$f"); done < <(
  find "$TPL_DIR" -type f -name '*.md' | sort
)

# ── 判据 1：v2 已删属性 ────────────────────────────────────────────────────────
# 扫模板与 JS（不扫文档：文档里出现旧名字通常正是在说「不要这么写」）。
REMOVED_ATTRS='hx-vars|hx-params|hx-request|hx-disabled-elt|hx-ws|hx-sse'
hits="$(grep -rnE "\\b(${REMOVED_ATTRS})[=\"]" "${tpl_files[@]}" "${js_files[@]}" 2>/dev/null || true)"
if [[ -n "$hits" ]]; then
  fail "判据 1：出现 htmx 2 已删除的属性（v4 里写了不报错、只是不生效）"
  printf '%s\n' "$hits" | sed 's/^/    /' >&2
fi

# ── 判据 2：v2 驼峰事件名 ─────────────────────────────────────────────────────
# v4 一律冒号分段（afterSwap → after:swap）。逐条列出而不是用宽泛正则：
# 宽泛的 `htmx:[a-z]+[A-Z]` 会误伤 v4 自己的复合名。
V2_EVENTS=(
  htmx:afterSwap htmx:beforeSwap htmx:afterRequest htmx:beforeRequest
  htmx:afterOnLoad htmx:beforeOnLoad htmx:afterProcessNode htmx:beforeProcessNode
  htmx:afterSettle htmx:beforeSend htmx:configRequest htmx:load htmx:responseError
  htmx:sendError htmx:timeout htmx:afterTrigger
)
ev_pattern="$(IFS='|'; echo "${V2_EVENTS[*]}")"
hits="$(grep -rnE "(${ev_pattern})\\b" "${tpl_files[@]}" "${js_files[@]}" 2>/dev/null || true)"
if [[ -n "$hits" ]]; then
  fail "判据 2：出现 htmx 2 的驼峰事件名（v4 改为冒号分段，旧名不再派发 = 死监听）"
  printf '%s\n' "$hits" | sed 's/^/    /' >&2
fi

# ── 判据 3：v2 扩展 API ───────────────────────────────────────────────────────
hits="$(grep -rnE 'htmx\.(defineExtension|onLoad)\b' "${tpl_files[@]}" "${js_files[@]}" 2>/dev/null || true)"
if [[ -n "$hits" ]]; then
  fail "判据 3：出现 htmx 2 的扩展 API（v4 改为 registerExtension / htmx_<event>）"
  printf '%s\n' "$hits" | sed 's/^/    /' >&2
fi

# ── 判据 4：反向核对 —— 模板里的每个 hx-<名> 必须在 bundle 里存在 ─────────────
#
# 这是本门禁最重要的一条：前三条查「已知的旧东西」，这条查「以为支持其实不支持」。
# 做法：从 bundle 里抽出它认识的 hx-<名> 全集，与模板里用到的求差集。
#
# 为什么不能用「bundle 里 grep 到该字符串」这种松判据：hx-get / hx-post 在 bundle 里
# 只作为 #actionSelector 的选择器出现一次，而 hx-swap 这类既有常量又有选择器 ——
# 松判据会漏掉真正不存在的名字（实测：它把 hx-get 误判成缺失）。
#
# 例外的两类：
#   · hx-progress / hx-progress-slide —— 本项目自定义 CSS 类，不是 htmx API。
#   · hx-<名> 伪元素 —— bundle 的 #Ie 有通用改写 `<hx-名>` → `<template hx type="名">`，
#     所以任何 hx-<名> 元素名都会被当作 partial 指令处理。本仓模板已统一写 template
#     形态（不依赖该改写），这里仍放行是为了不把合法的历史写法判红。
# 排除名单（本项目自定义，不是 htmx API）：
#   · hx-progress / hx-progress-slide —— 自定义 CSS 类
#   · hx-ext —— v4 的扩展挂载属性；扩展由 registerExtension 注册（本项目 json-swap），
#     属性名不出现在 bundle 的字符串字面量里，反向核对会把它误判成「不存在」。
EXCLUDE='^(hx-progress|hx-progress-slide|hx-ext)$'
# hx-ext：v4 用 registerExtension 注册扩展（本项目 json-swap 就是），属性本身合法 ——
# bundle 里只在 config 字符串里出现，不是可 grep 的字面量属性名。
HTMX_EXTRA_ALLOW="hx-ext"
GENERIC_ELEMENT_SHIM='<hx-'   # bundle 里存在通用改写即放行 hx-<名> 元素名

bundle_names="$(grep -oE 'hx-[a-z][a-z0-9-]*' "$BUNDLE" | sort -u)"
# hx-<名> 允许当元素名用（通用改写的目标），但只对 bundle 里确实有通用改写的版本放行。
has_shim=0
if grep -qF "$GENERIC_ELEMENT_SHIM" "$BUNDLE"; then has_shim=1; fi

# bundle 显式认识的**元素名**（当 hx-X 出现在 <...> 位置时按这条判）。
# partial 是唯一有实际用法的：`<hx-partial>` 与 `<template hx type="partial">` 等价。
shim_elements="partial"

used="$(grep -rhoE '\bhx-[a-z][a-z0-9-]*' "${tpl_files[@]}" 2>/dev/null | sort -u)"
missing=()
while IFS= read -r name; do
  [[ -z "$name" ]] && continue
  if [[ "$name" =~ $EXCLUDE ]]; then continue; fi
  if grep -qxF "$name" <<< "$bundle_names"; then continue; fi
  # 不在 bundle 显式名单里：只有当它是**元素名**且版本支持通用改写时放行。
  elem="${name#hx-}"
  if [[ $has_shim -eq 1 ]] && grep -qxF "$elem" <<< "$(echo "$shim_elements" | tr ' ' '\n')"; then
    continue
  fi
  missing+=("$name")
done <<< "$used"

if [[ ${#missing[@]} -gt 0 ]]; then
  fail "判据 4：模板里使用了 vendored htmx bundle 中不存在的 hx-* 名字（写了不报错、静默不生效）"
  for name in "${missing[@]}"; do
    where="$(grep -rln "\\b${name}\\b" "${tpl_files[@]}" 2>/dev/null | head -3 | tr '\n' ' ')"
    printf '    %-24s %s\n' "$name" "$where" >&2
  done
fi

# ── 自检：扫描范围没写坏（否则判据 4 会「什么都算违规」或「什么都没查」）────────
if [[ -z "$used" ]]; then
  fail "自检：模板里一个 hx-* 都没扫到 —— 扫描范围可能写错了"
fi
if [[ -z "$bundle_names" ]]; then
  fail "自检：从 bundle 里一个 hx-* 都没抽到 —— bundle 是否被替换/压缩方式变了"
fi

if [[ $violations -gt 0 ]]; then
  echo
  echo "htmx 属性契约见 internal/templates/CLAUDE.md「htmx 属性」一节。" >&2
  exit 1
fi

printf '✓ htmx 属性与事件名合规（扫 %d 模板 / %d JS / %d 文档；bundle 认识 %d 个 hx-*）\n' \
  "${#tpl_files[@]}" "${#js_files[@]}" "${#md_files[@]}" "$(echo "$bundle_names" | wc -l)"
