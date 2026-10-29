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
# 于是「这道门禁到底覆盖了什么」谁都答不出来。现在只描述覆盖面（下面的 glob），
# 具体文件名由脚本运行时列出；要用例清单就跑：
#
#   bash scripts/check-js-controls.sh
#
# 覆盖面：internal/templates/static/js 下**任意深度**的 *.test.cjs。
#
# 递归是必须的：此前只扫 ui/*.test.cjs 与 workbench/*.test.cjs 一级，
# 于是 workbench/methods/controls/ 下 9 个控件（base/color/corners/media/misc/
# repeater/selects/spacing/text）零测试且**不在扫描范围内** —— 加进去也不会被跑。
#
# 只有 node 不可用时跳过（本机与 CI 都有 node；跳过时会明确打印，不静默）。
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT" || exit 1

if ! command -v node >/dev/null 2>&1; then
  echo "· node 不可用，跳过前端控件行为测试"
  exit 0
fi

shopt -s nullglob globstar
# 递归扫：只看一级会漏掉 workbench/methods/controls/ 下的控件测试，
# 而"漏在扫描范围外"与"根本没有测试"在结果上无法区分（都表现为全绿）。
files=(internal/templates/static/js/**/*.test.cjs)
if [[ ${#files[@]} -eq 0 ]]; then
  echo "✗ 一个控件测试都没找到（**/*.test.cjs）—— 检查是否被误删" >&2
  exit 1
fi
echo "· 发现 ${#files[@]} 个控件测试文件（递归 internal/templates/static/js）"

# **逐文件串行**跑，不是一次传全部。
#
# 为什么：这些测试都靠 vm + DOM 桩，其中 plugin_write.test.cjs 会**切 json_write.test.cjs
# 的源码**当夹具复用。一个进程里跑多个文件时，这种「把别人的源码读进来执行」的做法会
# 相互干扰（实测：一起跑时 json_write 里一个依赖 formBlockOf 的用例会 ReferenceError，
# 单独跑则全绿）。逐文件隔离之后每个文件的结果都是确定的，也不受加载顺序影响。
fail=0
total_pass=0
for f in "${files[@]}"; do
  out="$(node --test "$f" 2>&1)"
  if [[ $? -ne 0 ]]; then
    echo "$out" >&2
    echo "✗ $(basename "$f") 未通过" >&2
    fail=1
    continue
  fi
  p="$(printf '%s\n' "$out" | grep -E '^ℹ pass ' | head -1 | awk '{print $3}')"
  total_pass=$((total_pass + ${p:-0}))
done

if [[ $fail -ne 0 ]]; then
  echo "✗ 前端控件行为测试未通过（${#files[@]} 个文件，逐文件串行）" >&2
  exit 1
fi

echo "✓ 前端控件行为测试通过（${#files[@]} 个文件 / ${total_pass} 个用例，逐文件串行）"
