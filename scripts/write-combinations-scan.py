#!/usr/bin/env python3
import os, re, sys

from pathlib import Path

def read_template(path, stack=()):
    path = Path(path).resolve()
    if path in stack:
        return ''
    src = path.read_text(encoding='utf-8-sig')
    src = re.sub(re.escape('{*') + '.*?' + re.escape('*}'), '', src, flags=re.S)
    def include(match):
        child = path.parent / match.group(1)
        if not child.exists():
            return match.group(0)
        return read_template(child, (*stack, path))
    return re.sub(r'{{[ \t]*include[ \t]+"([^"]+)".*?}}', include, src, flags=re.S)

path = sys.argv[1]
text = read_template(path)

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
            name_attr = r'\sname="' + re.escape(name) + r'"'
            if not any(re.search(name_attr, tag) and re.search(r'\sdata-row(?=\s|=|>)', tag)
                       for tag in re.findall(r'<template\b[^>]*>', block, re.S)):
                problems.append(
                    f'data-write-json 点名了 {name}，但表单里没有 <template name="{name}" data-row> —— '
                    '基座组装出空对象，这个字段会静默丢失'
                )

for p in problems:
    print(f'✗ {path}：{p}')
sys.exit(1 if problems else 0)
