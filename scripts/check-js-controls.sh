#!/usr/bin/env bash
# 前端控件的行为测试（node:test + vm，无浏览器、无依赖）。
#
# 为什么需要这道门禁：这些 .test.cjs 一直存在（json_write / drawer / bundle_members），
# 但**从来没有被任何自动流程跑过** —— 没人调用的测试永远不会变红，等于不存在。
# 审查指出的正是这个缺口：「静态架构门禁不能替代请求体转换的行为测试」。
#
# 覆盖的缺陷类型（都是界面上看不出来的）：
#   · json_write.test.cjs          —— 写表单三档分支 + 失败不清空 + CSRF 头
#   · htmx_form_body.test.cjs      —— 空值删除 / 同名多值不被误删 / 嵌套对象 / 行数组
#   · drawer.test.cjs              —— 抽屉焦点与关闭
#   · bundle_members.test.cjs      —— 捆绑成员行
#
# 只有 node 不可用时跳过（本机与 CI 都有 node；跳过时会明确打印，不静默）。
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT" || exit 1

if ! command -v node >/dev/null 2>&1; then
  echo "· node 不可用，跳过前端控件行为测试"
  exit 0
fi

shopt -s nullglob
# ui/ 是基座控件；workbench/ 是编辑器内核（此前 0 个测试，本批补上）。
# 两个目录都扫：单扫 ui/ 会让「工作台 JS 一个测试都没有」这件事看不见。
files=(internal/templates/static/js/ui/*.test.cjs internal/templates/static/js/workbench/*.test.cjs)
if [[ ${#files[@]} -eq 0 ]]; then
  echo "✗ 一个控件测试都没找到（*.test.cjs）—— 检查是否被误删" >&2
  exit 1
fi

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
