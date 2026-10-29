#!/usr/bin/env python3
"""对账写表单和可覆盖提交端点的字段契约。"""
import json, os, re, sys
from functools import lru_cache
from pathlib import Path

contracts = json.load(open(sys.argv[1], encoding='utf-8'))
root = sys.argv[2] if len(sys.argv) > 2 else 'internal/templates/admin'

# 不在契约里的端点。每条都要写理由 —— 这里都是「形状分派型」写端点：
# handler 用裸 JSON map（map[string]json.RawMessage）按**顶层键**决定走哪条分支，
# 而不是 `binding.JSON[T]` 绑一个固定 DTO，所以契约生成器取不到字段表。
# 它们的字段契约由各自的 handler 测试与 feature chain 守，不由本门禁守。
EXEMPT_ENDPOINTS = {
    # 裸 JSON map + 形状分派（单档 / 批档并集）：字段表在 handler 里按分支判。
    '/api/order/status': '裸 JSON map：单档/批档按顶层键分派（order_write.go）',
    '/api/order/cancel': '同上',
    '/api/order/note': '同上',
    '/api/order/refund': '同上',
    '/api/order/coupon/delete': '同上',
    '/api/navigation/create': '裸 JSON map：action=addSource 形状分派',
    '/api/navigation/update': '同上',
    '/api/navigation/delete': '同上',
    '/api/mail/contact/save': '裸 JSON map：含 tags 数组与 consent 分支',
    '/api/mail/contact/delete': '同上',
    '/api/customer/status': '裸 JSON map：批量/单档形状分派',
    '/api/membership/assign/set': '裸 JSON map：手动分配与解锁两档',
    '/api/membership/assign/unlock': '同上',
    '/api/i18n/save': '裸 JSON map：单条/批量词条写入',
    '/api/contenttemplate/delete': '仅路径/单 id，无请求体字段',
    # 泛型写辅助函数：binding 包在 helper[T] 里，类型实参在调用处的函数字面量中，
    # 生成器已能追（dtoFromGenericHelperCall），但下面这些的 helper 签名更绕，暂未覆盖。
    '/api/product/attribute/set-values': '泛型辅助：逐值行由前端的 data-write-rows 组装',
    '/api/product/bundle/set': '泛型辅助：成员表由 data-write-rows + builder 组装',
    '/api/product/variant/save': '泛型辅助：变体行由 builder 组装',
    '/api/product/detail-template/update-url': '泛型辅助',
    '/api/product/detail-template/reapply-preset': '泛型辅助',
    '/api/product/detail-template/rollback-document': '泛型辅助',
}

violations = 0
checked_forms = 0


def strip_comments(src):
    out = []
    while True:
        i = src.find('{*')
        if i < 0:
            out.append(src)
            return ''.join(out)
        out.append(src[:i])
        j = src.find('*}', i)
        if j < 0:
            return ''.join(out)
        src = src[j + 2:]


JET_TAG = re.compile(r'\{\{(.*?)\}\}', re.S)
STATIC_IMPORT = re.compile(r'^\s*import\s+"([^"]+)"')
STATIC_INCLUDE = re.compile(r'^\s*include\s+"([^"]+)"')
YIELD_BLOCK = re.compile(r'^\s*yield\s+([A-Za-z_]\w*)\s*\(')
BLOCK_START = re.compile(r'^\s*block\s+([A-Za-z_]\w*)\s*\(')
NESTED_START = re.compile(r'^\s*(?:block|if|range|try)\b')


@lru_cache(maxsize=None)
def template_source(path):
    return strip_comments(path.read_text(encoding='utf-8'))


def template_path(path, name):
    if name.startswith('/'):
        base = Path(root).resolve()
        if base.name == 'admin':
            base = base.parent
        return (base / name.lstrip('/')).resolve()
    return (path.parent / name).resolve()


def template_blocks(path, visited=()):
    """只收集 block 定义；导入文件的其它内容不属于调用方表单。"""
    if path in visited or not path.is_file():
        return {}
    source = template_source(path)
    tags = list(JET_TAG.finditer(source))
    blocks = {}
    for tag in tags:
        imported = STATIC_IMPORT.match(tag.group(1))
        if imported:
            blocks.update(template_blocks(template_path(path, imported.group(1)), visited + (path,)))
    for index, tag in enumerate(tags):
        start = BLOCK_START.match(tag.group(1))
        if not start:
            continue
        depth = 1
        for end in tags[index + 1:]:
            token = end.group(1).strip()
            if token == 'end':
                depth -= 1
                if depth == 0:
                    blocks[start.group(1)] = (path, source[tag.end():end.start()])
                    break
            elif NESTED_START.match(token):
                depth += 1
    return blocks


def expand_template(path, source=None, blocks=None, active=()):
    """沿真实 include/yield 调用展开控件，防止未调用的片段补齐必填项。"""
    if source is None:
        source = template_source(path)
    available = dict(blocks or {})
    available.update(template_blocks(path))

    def expand_tag(tag):
        token = tag.group(1)
        included = STATIC_INCLUDE.match(token)
        if included:
            target = template_path(path, included.group(1))
            identity = ('include', target)
            if target.is_file() and identity not in active:
                return expand_template(target, blocks=available, active=active + (identity,))
        yielded = YIELD_BLOCK.match(token)
        if yielded and yielded.group(1) in available:
            owner, body = available[yielded.group(1)]
            identity = ('yield', owner, yielded.group(1))
            if identity not in active:
                return expand_template(owner, source=body, blocks=available, active=active + (identity,))
        return tag.group(0)

    return JET_TAG.sub(expand_tag, source)


def form_fields(block):
    """提取具名控件，ids[] 与动态行容器使用 DTO 顶层字段名。"""
    names = []
    for m in re.finditer(r'<input\b[^>]*>|<select\b[^>]*>|<textarea\b[^>]*>', block, re.S):
        tag = m.group(0)
        input_type = attribute(tag, 'type').lower()
        if input_type in ('submit', 'button', 'reset', 'image'):
            continue
        nm = re.search(r'\bname="([^"]+)"', tag)
        if not nm:
            continue
        name = nm.group(1)
        if '{{' in name:      # 动态名（如 {{f.Name}}）交给运行时
            continue
        if name == 'csrf_token':
            continue
        names.append("ids" if name == "ids[]" else name)
    # 容器名按 write-base.js 的声明语法解析，不把选择器和驱动字段当 DTO 字段。
    for kind, value in re.findall(r'\b(data-write-rows|data-write-object|data-transfer-lines-name)="([^"]+)"', block):
        if '{{' in value:
            continue
        parts = [part.strip() for part in value.split(';')]
        if kind == 'data-transfer-lines-name':
            names.append(value)
        elif kind == 'data-write-rows' and len(parts) >= 2:
            names.append(parts[2] if len(parts) > 2 and parts[2] else 'rows')
        elif kind == 'data-write-object' and len(parts) >= 3:
            names.append(parts[1])
    return names


def attribute(source, name, default=''):
    found = re.search(r'\b' + re.escape(name) + r'="([^"]*)"', source)
    return found.group(1) if found else default


def form_actions(attrs, block, fields):
    """每个提交按钮单独计算实际字段；按钮值只有 data-write-extra 点名才会被基座收集。"""
    buttons = []
    available = set(fields)
    for tag in re.findall(r'<(?:button|input)\b[^>]*>', block, re.S):
        name = attribute(tag, 'name')
        if name and '{{' not in name:
            available.add('ids' if name == 'ids[]' else name)
        default_type = 'submit' if tag.startswith('<button') else 'text'
        if attribute(tag, 'type', default_type).lower() == 'submit':
            buttons.append(tag)
    actions = []
    for button in buttons or ['']:
        endpoint = attribute(button, 'data-write-url', attribute(attrs, 'data-write-url'))
        if not endpoint or '{{' in endpoint:
            continue
        mode = attribute(button, 'data-write', attribute(attrs, 'data-write'))
        extras = set(filter(None, (part.strip() for part in attribute(button, 'data-write-extra', attribute(attrs, 'data-write-extra')).split(','))))
        offered = set(fields)
        if mode == 'bulk':
            offered.intersection_update(extras | {'ids'})
        offered.update(extras & available)
        actions.append((endpoint, offered))
    return actions


for dirpath, _, files in os.walk(root):
    for name in sorted(files):
        if not (name.endswith('.html') or name.endswith('.jet')):
            continue
        path = os.path.join(dirpath, name)
        rel = os.path.relpath(path, root)
        text = expand_template(Path(path).resolve())

        for m in re.finditer(r'<form\b([^>]*)>', text, re.S):
            attrs = re.sub(r'\s+', ' ', m.group(1))
            url_m = re.search(r'\bdata-write-url="([^"]*)"', attrs)
            if not url_m:
                continue
            url = url_m.group(1)
            if '{{' in url:
                continue   # 动态端点：契约按静态路径索引，运行时才对得上
            end = text.find('</form>', m.end())
            block = text[m.start(): end if end >= 0 else len(text)]
            present = form_fields(block)
            actions = form_actions(attrs, block, present)
            specs = []
            for endpoint, offered in actions:
                if endpoint in contracts:
                    specs.append((endpoint, contracts[endpoint], offered))
                elif endpoint not in EXEMPT_ENDPOINTS:
                    print(f'✗ {rel}：端点 {endpoint} 不在写端点契约里（先重跑 go run ./cmd/write-contracts）')
                    violations += 1
            if not specs:
                continue
            allowed = {f['name'] for _, spec, _ in specs for f in spec.get('fields', [])}
            checked_forms += 1

            # data-write-builder：页面级组装器会把**页面字段名**映射成 DTO 字段
            #（membership 的 thresholdYuan → thresholdAmount、freeShipping+discountPercent
            # → entitlements 数组）。这种映射只有页面脚本知道，本门禁看不到 —— 跳过对账。
            #
            # 谁来守这些映射：各自的 .test.cjs —— onboarding 侧是
            # ui/membership_write.test.cjs，商品侧是 ui/product_variant_write.test.cjs。
            # 注释此前把后者写成 product_bundle_write.test.cjs（bundle 概念已下线，
            # 那个文件从来不存在），于是"由测试守"这句话对商品变体行一直是假的。
            if 'data-write-builder=' in attrs:
                continue

            submitted = set().union(*(offered for _, _, offered in specs))
            for fname in submitted:
                # 嵌套字段由 data-write-json 组装，可能不在顶层字段表里 —— 放行。
                if 'data-write-json=' in attrs and fname not in allowed:
                    continue
                if fname not in allowed and fname != "ids":
                    print(f'✗ {rel}：字段 name="{fname}" 不在表单任一目标 DTO 的字段表里'
                          f'（端点 {url}）—— 服务端会静默丢掉它')
                    violations += 1

            for endpoint, spec, offered in specs:
                for field in spec.get('fields', []):
                    if field.get('required') and field['name'] not in offered:
                        print(f'✗ {rel}：{spec["dto"]} 的必填字段 "{field["name"]}" 没有出现在表单里'
                              f'（端点 {endpoint}）—— 提交必然失败')
                        violations += 1

if violations:
    print()
    print('写表单契约见 internal/templates/CLAUDE.md「写操作」。', file=sys.stderr)
    sys.exit(1)
print(f'✓ 写表单字段名全部对得上（{checked_forms} 个表单 vs {len(contracts)} 条端点契约）')
