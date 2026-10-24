#!/usr/bin/env bash
# 写表单的组合合法性（形状约束，不是属性名）。
#
# 为什么需要这道门禁：合法的属性组合只有几种，而**错的组合不会报错**——基座会照常提交，
# 只是行为不是写那个属性的人以为的那样。三条真实踩过的例子：
#
#   · bulk 表单没有 [name=ids] → 点「批量删除」提示「没勾选任何项」，永远发不出请求。
#   · data-write-outcome 写在非 bulk 表单上 → 基座忽略它（只对 bulk 有意义），
#     写的人以为能拿到逐条回执。
#   · data-write-nav 带 {id} 却用在 bulk 上 → 批量没有单一 id，{id} 被替成空串，
#     跳到一个不存在的地址。
#
# 判据：
#   1. data-write="bulk" 的表单/按钮所在表单里必须有 [name=ids] 或 [name="ids[]"]
#      （或声明了 data-write-ids-scope 指向别处）。
#   2. data-write-outcome 只能出现在 bulk 上。
#   3. data-write-nav 里的 {id} 只能配 create / update（有单一 id 的动作）。
#   4. data-write-json 点名的每个 name 都必须在同一表单里有对应的
#      <template name="…" data-row>（否则基座组装出空对象，字段静默丢失）。
#
# 退出码 0 = 通过；1 = 有违规。
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT" || exit 1

TPL_DIR="internal/templates/admin"
violations=0
fail() { printf '✗ %s\n' "$1" >&2; violations=$((violations + 1)); }

# 行勾选框常放在被 include 的片段里（block_row.html / product_row.html …），
# 单文件扫不到。先把「哪些模板里出现过 name="ids"」收集起来，判据 1 用得到。
IDS_FRAGMENTS="$(grep -rlE 'name="ids(\[\])?"' "$TPL_DIR" 2>/dev/null | tr '\n' ' ')"

while IFS= read -r file; do
  WB_IDS_FRAGMENTS="$IDS_FRAGMENTS" python3 - "$file" <<'PY'
import os, re, sys

path = sys.argv[1]
src = open(path, encoding='utf-8').read()
if src.startswith('\ufeff'):
    src = src[1:]

# 剥 Jet 注释（注释里会解释这些属性，不是用法）。
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

problems = []

# 逐个 form：取出开始标签与配对的闭合范围（近似：到下一个 </form>）。
for m in re.finditer(r'<form\b([^>]*)>', text, re.S):
    attrs = re.sub(r'\s+', ' ', m.group(1))
    # 表单自身没有 data-write-url 时，看它内部有没有带 data-write-url 的按钮。
    body_end = text.find('</form>', m.end())
    block = text[m.start(): body_end if body_end >= 0 else len(text)]

    mode_here = re.search(r'\bdata-write="([^"]*)"', attrs)
    url_here = re.search(r'\bdata-write-url="([^"]*)"', attrs)
    has_write = bool(url_here)

    # 判据 1：bulk 必须有 ids 来源。
    #   表单级 bulk，或内部某个按钮级 bulk —— 两种都要有 ids 或 scope。
    is_bulk = bool(mode_here and mode_here.group(1) == 'bulk') or \
              bool(re.search(r'data-write="bulk"', block))
    if is_bulk:
        # ids 可能来自被 include 的行片段（block_row.html 等）——那类写法是
        # `{{include "…"}}` 或 `{{yield …}}` 在表单块内出现。
        has_ids = bool(re.search(r'name="ids(\[\])?"', block)) or \
                  bool(re.search(r'name=ids(\[\])?[ >]', block)) or \
                  (bool(re.search(r'\{\{\s*(include|yield)\b', block)) and
                   # 且仓库里确实存在带 name="ids" 的片段 —— 否则就是真的没有 ids。
                   bool(os.environ.get('WB_IDS_FRAGMENTS', '').strip()))
        has_scope = 'data-write-ids-scope=' in block
        if not has_ids and not has_scope:
            problems.append(
                'data-write="bulk" 的写表单里没有 [name=ids] 也没有 data-write-ids-scope —— '
                '基座收不到要操作的 id，点下去只会提示「没勾选任何项」'
            )

    # 判据 2：outcome 只配 bulk。
    if 'data-write-outcome=' in block and not is_bulk:
        problems.append('data-write-outcome 只对 bulk 有意义，这个表单不是 bulk —— 它会被忽略')

    # 判据 3：nav 的 {id} 只能配 create / update。
    for nav in re.finditer(r'data-write-nav="([^"]*)"', block):
        if '{id}' in nav.group(1):
            modes = {x.group(1) for x in re.finditer(r'data-write="([^"]*)"', block)}
            if modes and not (modes & {'create', 'update'}):
                problems.append(
                    'data-write-nav 带 {id} 但表单不是 create/update —— '
                    '批量/删除动作没有单一 id，{id} 会被替成空串，跳到不存在的地址'
                )

    # 判据 4：data-write-json 点名的 name 必须有对应 <template name="…" data-row>。
    for jf in re.finditer(r'data-write-json="([^"]*)"', attrs):
        for name in [x.strip() for x in jf.group(1).split(',') if x.strip()]:
            pat = r'<template[^>]*\bname="' + re.escape(name) + r'"[^>]*\bdata-row\b'
            if not re.search(pat, block):
                problems.append(
                    f'data-write-json 点名了 {name}，但表单里没有 <template name="{name}" data-row> —— '
                    '基座组装出空对象，这个字段会静默丢失'
                )

for p in problems:
    print(f'✗ {path}：{p}')
sys.exit(1 if problems else 0)
PY
  [[ $? -ne 0 ]] && violations=$((violations + 1))
done < <(find "$TPL_DIR" -type f \( -name '*.html' -o -name '*.jet' \) | sort)

if [[ $violations -gt 0 ]]; then
  echo
  echo "写表单契约见 internal/templates/CLAUDE.md「写操作」。" >&2
  exit 1
fi
echo "✓ 写表单组合合法（bulk 有 ids / outcome 只配 bulk / nav 的 {id} 只配 create-update / json 有行模板）"
