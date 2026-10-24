#!/usr/bin/env bash
# 写表单的属性白名单（write-base.js 的契约）。
#
# 为什么需要这道门禁：写表单的契约是**一组合法写法**，而属性名写错的表现是「静默失效」——
# 基座不认识这个属性，于是那件事不会发生（不报错、不 500、日志干净）。本项目踩过的例子：
#
#   · `data-write-number` 是旧基座为 fetch+JSON 路径补的类型声明，随那条路径一起删了。
#     新基座上再写它不会报错，只是**什么都不做** —— 而写它的人以为自己声明了类型。
#   · `hx-swap="none"` 曾被逐个表单声明；现在由基座内置。留在模板里不报错，
#     但它暗示「这个表单靠 htmx 提交」，与事实相反。
#
# 判据（任一命中即失败）：
#   1. 写表单（有 data-write-url）上只允许白名单属性；出现已删属性即违规。
#   2. 写表单上禁止 hx-post / hx-swap / hx-target / hx-include —— 基座接 submit，
#      htmx 也接 submit，同时挂会一次点击发两个请求（写动作＝建两条记录）。
#   3. data-write 的值必须在枚举里；data-write-url 必须以 /api/ 开头。
#
# 退出码 0 = 通过；1 = 有违规。
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT" || exit 1

TPL_DIR="internal/templates/admin"

violations=0
fail() { printf '✗ %s\n' "$1" >&2; violations=$((violations + 1)); }

# 已删除的属性（历史名 → 现在的写法）。出现在写表单上就是没迁完或写错了。
# shellcheck disable=SC2016
declare -A REMOVED=(
  [data-write-api]='data-write-url'
  [data-write-mode]='data-write'
  [data-write-number]='（已删：formdata 的 "3" 服务端直接绑进 int）'
  [data-write-float]='（已删：同上）'
  [data-write-bool]='（已删：同上）'
  [data-write-array]='（已删：a&a=b 直接绑成 []string）'
  [data-write-skip-empty]='（已删：基座永远删空值）'
  [data-write-host]='（已删：基座用 closest("[data-write]")）'
  [data-write-flash]='data-write-reload'
  [data-write-flash-target]='（已删：结论经 sessionStorage 跨刷新）'
  [data-close-on-ok]='data-write-close'
  [data-reload-on-ok]='data-write-reload'
  [data-write-html]='data-write-json-wrap'
)

# 仍在用的整形属性（基座认识；不属于类型声明）。
# data-write-ids-scope 保留：ids 可能在**另一个容器**里（抽屉内容被克隆到 document 级，
# 勾选框留在列表的批量表单里），必须显式指回去。
declare -A ALLOWED_SHAPING=(
  [data-write-json]='行模板 → JSON（数组或单对象）'
  [data-write-json-single]='声明哪些 name 发单对象'
  [data-write-json-wrap]='裸 HTML → {html: …}'
  [data-write-lines]='多行文本 → 字符串数组'
  [data-write-keep-empty]='空串有语义的字段'
  [data-write-ids-scope]='ids 在另一个容器里'
  [data-write-builder]='存量兼容：页面级组装器'
  [data-write-rows]='存量兼容：行容器 → 数组'
  [data-write-object]='存量兼容：容器 → 单对象'
)

# 写表单上不该出现的 htmx 属性。
declare -A NO_HTMX=(
  [hx-post]='基座发 fetch；同挂会双提交'
  [hx-put]='同上'
  [hx-patch]='同上'
  [hx-delete]='同上'
  [hx-swap]='基座内置 hx-swap="none"'
  [hx-target]='写动作没有要换进 DOM 的内容'
  [hx-include]='基座从 closest("[data-write]") 收字段'
)

# 遍历模板：按 <form ...> 与 <button ...> 逐个判（属性可能写在任一处）。
while IFS= read -r file; do
  # 剥掉 Jet 注释，避免把「注释里解释历史」当成违规。
  python3 - "$file" <<'PY' > /tmp/wb_tags.txt
import re, sys
src = open(sys.argv[1], encoding='utf-8').read()
if src.startswith('\ufeff'):
    src = src[1:]
# 剥 Jet 注释 {* … *}
out = []
while True:
    i = src.find('{*')
    if i < 0:
        out.append(src); break
    out.append(src[:i])
    j = src.find('*}', i)
    if j < 0: break
    src = src[j+2:]
text = ''.join(out)
# 输出所有 <form …> 与 <button …> 开始标签（跨行）。
for m in re.finditer(r'<(form|button)\b[^>]*>', text, re.S):
    tag = re.sub(r'\s+', ' ', m.group(0))
    print(f'{m.group(1)}\t{tag}')
PY
  while IFS=$'\t' read -r kind tag; do
    [[ -z "${kind:-}" ]] && continue
    # 只判写表单：有 data-write-url 的表单，或带 data-write-url 的按钮。
    if ! grep -q 'data-write-url=' <<< "$tag"; then
      continue
    fi

    # 判据 1：已删属性
    for attr in "${!REMOVED[@]}"; do
      if grep -qE "[[:space:]]${attr}([=[:space:]/>])" <<< "$tag"; then
        fail "$file：写表单上出现已删除的 ${attr}（现在应为 ${REMOVED[$attr]}）"
      fi
    done

    # 判据 2：不该挂的 htmx 属性
    for attr in "${!NO_HTMX[@]}"; do
      if grep -qE "[[:space:]]${attr}([=[:space:]/>])" <<< "$tag"; then
        fail "$file：写表单上出现 ${attr} —— ${NO_HTMX[$attr]}"
      fi
    done

    if [[ "$kind" == "form" ]]; then
      # 判据 3：枚举与端点前缀
      mode="$(sed -nE 's/.*[[:space:]]data-write="([^"]*)".*/\1/p' <<< "$tag")"
      case "$mode" in
        create|update|delete|bulk|custom|single) ;;
        "") fail "$file：写表单有 data-write-url 但缺 data-write（必填）" ;;
        *) fail "$file：data-write=\"${mode}\" 不在枚举里（create/update/delete/bulk/custom）" ;;
      esac
      url="$(sed -nE 's/.*[[:space:]]data-write-url="([^"]*)".*/\1/p' <<< "$tag")"
      if [[ "$url" != /api/* ]]; then
        fail "$file：data-write-url=\"${url}\" 必须以 /api/ 开头（写基座只对接 JSON 端点）"
      fi
    fi
  done < /tmp/wb_tags.txt
done < <(find "$TPL_DIR" -type f \( -name '*.html' -o -name '*.jet' \) | sort)

rm -f /tmp/wb_tags.txt

if [[ $violations -gt 0 ]]; then
  echo
  echo "写表单契约见 internal/templates/CLAUDE.md「写操作」与 ui/write-base.js 文件头。" >&2
  exit 1
fi

printf '✓ 写表单属性合规（白名单 %d 个已删属性 / %d 个禁用 htmx 属性）\n' \
  "${#REMOVED[@]}" "${#NO_HTMX[@]}"
