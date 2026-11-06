#!/usr/bin/env bash
# 前端控件的行为测试（node:test + vm，无浏览器、无依赖）。
#
# 为什么需要这道门禁：这些 .test.cjs 一直存在，但**从来没有被任何自动流程跑过** ——
# 没人调用的测试永远不会变红，等于不存在。
# 审查指出的正是这个缺口：「静态架构门禁不能替代请求体转换的行为测试」。
#
# ## 覆盖清单以**文件系统为准**，不在这里抄一份
#
# 这段注释曾经点名 json_write / htmx_form_body / bundle_members / product_bundle_write
# 四个文件 —— 四个全都不存在，而实际存在的七个一个都没写。注释与实际各说一套，
# 于是「这道门禁到底覆盖了什么」谁都答不出来。现在只描述覆盖面（下面的根与 glob），
# 具体文件名由脚本运行时列出；要用例清单就跑：
#
#   bash scripts/check-js-controls.sh
#
# 覆盖面（两侧都扫，任意深度）：
#   - pkg/view/ui/assets/js      公共控件源与它们的行为测试（属主）
#   - internal/templates/static/js  应用专用控件 / 工作台测试
#
# 测试可以读另一侧的源，但必须显式指向真实属主（例如 pkg/view/ui/assets/js/ui），
# 不允许把 JS 复制回旧位置，也不做多路径 fallback。
#
# 逐文件串行跑：这些测试都靠 vm + DOM 桩，其中一部分会把**别的控件脚本的源码**读进来
# 当夹具复用（vm 里没有模块边界，共享的全局会串）。一个进程里跑多个文件时，这种
# 「把别人的源码读进来执行」的做法会相互干扰 —— 表现为只在合并运行时出现的
# ReferenceError，单独跑则全绿。逐文件隔离之后每个文件的结果都是确定的，也不受加载
# 顺序影响；代价是启动次数多，对这点体量无所谓。
#
# 退役的 JSON→DOM 执行器（json_swap.js / form-json-v4.js）与它们的源文件、默认加载、
# 结果解释器接口都不得恢复；本脚本末尾保留一条终局断言。
#
# 只有 node 不可用时跳过（本机与 CI 都有 node；跳过时会明确打印，不静默）。
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT" || exit 1

if ! command -v node >/dev/null 2>&1; then
  echo "· node 不可用，跳过前端控件行为测试"
  exit 0
fi

ROOTS=("pkg/view/ui/assets/js" "internal/templates/static/js")
files=()
for root in "${ROOTS[@]}"; do
  if [[ ! -d "$root" ]]; then
    echo "✗ 控件测试扫描根不存在：$root" >&2
    exit 1
  fi
  if [[ ! -r "$root" ]]; then
    echo "✗ 控件测试扫描根不可读：$root" >&2
    exit 1
  fi
  errfile="$(mktemp)"
  found=0
  while IFS= read -r -d '' f; do
    files+=("$f")
    found=$((found + 1))
  done < <(find "$root" -type f -name '*.test.cjs' -print0 2>"$errfile")
  if [[ -s "$errfile" ]]; then
    cat "$errfile" >&2
    rm -f "$errfile"
    echo "✗ 遍历控件测试失败：$root" >&2
    exit 1
  fi
  rm -f "$errfile"
  echo "· $root：$found 个控件测试文件"
done

if [[ ${#files[@]} -eq 0 ]]; then
  echo "✗ 两侧一个控件测试都没找到（*.test.cjs）—— 检查是否被误删" >&2
  exit 1
fi

fail=0
total_pass=0
total_tests=0
for f in "${files[@]}"; do
  out="$(node --test "$f" 2>&1)"
  if [[ $? -ne 0 ]]; then
    echo "$out" >&2
    echo "✗ $f 未通过" >&2
    fail=1
    continue
  fi
  p="$(printf '%s\n' "$out" | grep -E '^ℹ pass ' | head -1 | awk '{print $3}')"
  t="$(printf '%s\n' "$out" | grep -E '^ℹ tests ' | head -1 | awk '{print $3}')"
  total_pass=$((total_pass + ${p:-0}))
  total_tests=$((total_tests + ${t:-0}))
  if [[ ${t:-0} -eq 0 ]]; then
    echo "✗ $f 实际执行 0 个用例 —— 测试文件被掏空或未注册" >&2
    fail=1
  fi
done

if [[ $fail -ne 0 ]]; then
  echo "✗ 前端控件行为测试未通过（${#files[@]} 个文件，逐文件串行）" >&2
  exit 1
fi

# 终局断言：退役执行器不得以任何形态恢复。
retired=(
  "pkg/view/ui/assets/js/ui/json_swap.js"
  "pkg/view/ui/assets/js/ui/form-json-v4.js"
  "internal/templates/static/js/ui/json_swap.js"
  "internal/templates/static/js/ui/form-json-v4.js"
  "internal/templates/static/vendor/form-json"
)
for path in "${retired[@]}"; do
  if [[ -e "$path" ]]; then
    echo "✗ 退役执行器被恢复：$path" >&2
    exit 1
  fi
done
entry="pkg/view/ui/templates/partials/ui_scripts.html"
if [[ ! -f "$entry" ]]; then
  echo "✗ 控件默认加载入口缺失：$entry" >&2
  exit 1
fi
if grep -Eq 'json_swap|form-json|formjson|registerResultHandler' "$entry"; then
  echo "✗ 默认加载又引用了退役 JSON 执行器：$entry" >&2
  exit 1
fi
while IFS= read -r src; do
  name="${src#/static/js/}"
  if [[ -f "pkg/view/ui/assets/js/$name" ]]; then
    file="pkg/view/ui/assets/js/$name"
  elif [[ -f "internal/templates/static/js/$name" ]]; then
    file="internal/templates/static/js/$name"
  else
    continue
  fi
  if grep -Eq 'registerResultHandler|WPJsonWrite' "$file"; then
    echo "✗ 默认加载的控件恢复了 JSON→DOM 结果解释器：$file" >&2
    exit 1
  fi
done < <(grep -o '/static/js/[a-z0-9_./-]*\.js' "$entry" | sort -u)

echo "✓ 前端控件行为测试通过（${#files[@]} 个文件 / ${total_tests} 个用例，其中通过 ${total_pass}；逐文件串行）"
