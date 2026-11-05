#!/usr/bin/env python3
"""对账写表单和可覆盖提交端点的字段契约。"""
import hashlib
import json
import os
import re
import sys
from functools import lru_cache
from html.parser import HTMLParser
from pathlib import Path
from urllib.parse import urlsplit

contracts = json.load(open(sys.argv[1], encoding='utf-8'))
root = sys.argv[2] if len(sys.argv) > 2 else 'internal/templates'
prefix = sys.argv[3] if len(sys.argv) > 3 else ''
if not Path(root).is_dir():
    sys.exit(f'字段门禁未检查：模板目录不存在：{root}')

violations = 0
checked_forms = 0
checked_templates = 0


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


WRITE_METHODS = ('post', 'put', 'patch', 'delete')


def write_endpoint(attrs):
    for method in WRITE_METHODS:
        if attrs.get('hx-' + method):
            return method.upper(), attrs['hx-' + method]
    if attrs.get('method', '').lower() in WRITE_METHODS and attrs.get('action'):
        return attrs['method'].upper(), attrs['action']
    return None


class FormParser(HTMLParser):
    """保留 form 归属与 template 边界，外部控件不能混入相邻批量表单。"""
    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.forms = []
        self.controls = []
        self.form = None
        self.templates = []
        self.elements = []
        self.serial = 0

    def handle_starttag(self, tag, pairs):
        attrs = dict(pairs)
        element = {'tag': tag, 'attrs': attrs}
        if tag == 'legend' and self.elements and self.elements[-1]['tag'] == 'fieldset':
            fieldset = self.elements[-1]
            element['exempts'] = fieldset if not fieldset.get('legend_seen') else None
            fieldset['legend_seen'] = True
        if tag == 'template':
            self.serial += 1
            self.templates.append(self.serial)
        uncertain = set()
        for ancestor in self.elements + [element]:
            uncertain.update(set(ancestor['attrs']) & {'hx-vals', 'hx-inherit', 'hx-disinherit'})
            # hx-include 的字面 id 选择器可静态解析（见 included_controls）；含 Jet 表达式或
            # 非 id 选择器时照旧报未检查，不放宽判据。
            include = ancestor['attrs'].get('hx-include')
            if include is not None and not include_is_literal(include):
                uncertain.add('hx-include')
            if ancestor is not element and ancestor['tag'] != 'form' and 'hx-encoding' in ancestor['attrs']:
                uncertain.add('hx-encoding')
        if tag == 'form':
            self.form = {'attrs': attrs, 'scope': tuple(self.templates), 'parent': self.form,
                         'uncertain': uncertain}
            self.forms.append(self.form)
        if tag in ('input', 'select', 'textarea', 'button'):
            disabled = 'disabled' in attrs
            for ancestor in self.elements:
                if ancestor['tag'] == 'fieldset' and 'disabled' in ancestor['attrs']:
                    disabled |= not any(parent.get('exempts') is ancestor for parent in self.elements)
            self.controls.append({'attrs': attrs, 'tag': tag, 'parent': self.form,
                                  'scope': tuple(self.templates), 'disabled': disabled,
                                  'uncertain': uncertain})
        if tag not in ('area', 'base', 'br', 'col', 'embed', 'hr', 'img', 'input', 'link', 'meta', 'param', 'source', 'track', 'wbr'):
            self.elements.append(element)

    def handle_endtag(self, tag):
        if tag == 'form' and self.form:
            self.form = self.form['parent']
        if tag == 'template' and self.templates:
            self.templates.pop()
        for index in range(len(self.elements) - 1, -1, -1):
            if self.elements[index]['tag'] == tag:
                del self.elements[index:]
                break

    def owner(self, control):
        attrs = control['attrs']
        if 'form' not in attrs:
            return control['parent']
        return next((form for form in self.forms if form['attrs'].get('id') == attrs['form']
                     and form['scope'] == control['scope']), None)

    def fields_of(self, form):
        """表单自身提交时带上的具名字段。hx-include 引入的字段另行合并。"""
        out = set()
        for control in self.controls:
            if self.owner(control) is not form or control['scope'] != form['scope'] or control['disabled']:
                continue
            attrs = control['attrs']
            kind = attrs.get('type', 'submit' if control['tag'] == 'button' else 'text').lower()
            if kind in ('submit', 'button', 'reset', 'image') or write_endpoint(attrs):
                continue
            if attrs.get('name'):
                out.add(attrs['name'])
        return out

    def form_by_id(self, form_id):
        for form in self.forms:
            if form['attrs'].get('id') == form_id:
                return form
        return None

    def included_fields(self, form, attrs):
        """按 hx-include 的字面 #id 选择器合并被引入表单的字段。"""
        merged = set()
        for value in (attrs.get('hx-include'), form['attrs'].get('hx-include')):
            if not value or not include_is_literal(value):
                continue
            for form_id in include_ids(value):
                target = self.form_by_id(form_id)
                if target is not None:
                    merged |= self.fields_of(target)
        return merged

    def actions(self):
        for form in self.forms:
            controls = [control for control in self.controls if self.owner(control) is form
                        and control['scope'] == form['scope'] and not control['disabled']]
            fields = set()
            buttons = []
            for control in controls:
                attrs = control['attrs']
                kind = attrs.get('type', 'submit' if control['tag'] == 'button' else 'text').lower()
                if kind == 'submit' or write_endpoint(attrs):
                    buttons.append(attrs)
                elif kind not in ('button', 'reset', 'image') and attrs.get('name'):
                    fields.add(attrs['name'])
            # 独立 type=button 不替代原表单的提交入口。
            if not any(button.get('type', 'submit').lower() == 'submit' for button in buttons):
                buttons.append({})
            uncertain = form['uncertain'] | set().union(*(control['uncertain'] for control in controls))
            for button in buttons:
                endpoint = write_endpoint(button) or write_endpoint(form['attrs'])
                if not any('hx-' + method in button or 'hx-' + method in form['attrs'] for method in WRITE_METHODS):
                    native = {**form['attrs']}
                    for source, target in (('formmethod', 'method'), ('formaction', 'action'), ('formenctype', 'enctype')):
                        if source in button:
                            native[target] = button[source]
                    endpoint = write_endpoint(native)
                if endpoint:
                    offered = fields | ({button['name']} if button.get('name') else set())
                    attrs = {**form['attrs'], **button}
                    offered |= self.included_fields(form, attrs)
                    if button.get('formenctype'):
                        attrs['enctype'] = button['formenctype']
                    yield endpoint, attrs, offered, uncertain
        for control in self.controls:
            if self.owner(control) is None and not control['disabled'] and write_endpoint(control['attrs']):
                attrs = control['attrs']
                offered = {attrs['name']} if attrs.get('name') else set()
                included_uncertain = set()
                include = attrs.get('hx-include')
                if include:
                    if include_is_literal(include):
                        for form_id in include_ids(include):
                            target = self.form_by_id(form_id)
                            if target is not None:
                                offered |= self.fields_of(target)
                    else:
                        included_uncertain.add('hx-include')
                yield write_endpoint(attrs), attrs, offered, control['uncertain'] | included_uncertain


INCLUDE_ID = re.compile(r'^\s*#([A-Za-z][A-Za-z0-9_-]*)\s*$')


def include_is_literal(value):
    # hx-include 允许逗号分隔多个选择器；这里只认「纯 #id」的静态写法。
    parts = [part.strip() for part in str(value).split(',') if part.strip()]
    return bool(parts) and all(INCLUDE_ID.match(part) for part in parts)


def include_ids(value):
    return [INCLUDE_ID.match(part.strip()).group(1)
            for part in str(value).split(',') if INCLUDE_ID.match(part.strip())]


def mask_jet(match):
    # 不让 Jet 内部的引号和 > 干扰 HTML 属性解析；相同表达式仍可匹配 form/id。
    return '__JET_' + hashlib.sha256(match.group(1).encode('utf-8')).hexdigest()[:16] + '__'


def report(message):
    global violations
    print(message)
    violations += 1


for dirpath, _, files in os.walk(root):
    for name in sorted(files):
        if not name.endswith(('.html', '.jet')):
            continue
        path = os.path.join(dirpath, name)
        rel = os.path.relpath(path, root)
        if not rel.startswith(prefix):
            continue
        checked_templates += 1
        text = expand_template(Path(path).resolve())
        parser = FormParser()
        parser.feed(JET_TAG.sub(mask_jet, text))
        for (method, url), attrs, offered, uncertain in parser.actions():
            if uncertain:
                report(f'{rel}：未检查 {method} {url} 的参数选择/继承声明：' + ', '.join(sorted(uncertain)))
                continue
            parsed = urlsplit(url)
            if '__JET_' in parsed.path or not parsed.path.startswith('/') or parsed.netloc or parsed.scheme:
                report(f'{rel}：未检查动态或非本站端点 {method} {url}；不使用宽泛前缀匹配')
                continue
            endpoint = method + ' ' + parsed.path
            spec = contracts.get(endpoint)
            if spec is None:
                report(f'{rel}：端点 {endpoint} 不在写端点契约里（先重跑 go run ./cmd/write-contracts）')
                continue
            protocol = spec.get('protocol')
            if protocol not in ('form', 'multipart', 'json') or spec.get('reason'):
                report(f'{rel}：未检查 {endpoint}：' + spec.get('reason', '契约缺少确定输入协议'))
                continue
            encoding = attrs.get('hx-encoding') or attrs.get('enctype') or ''
            actual = 'json' if attrs.get('data-write-builder') else ('multipart' if encoding == 'multipart/form-data' else 'form')
            if protocol != actual:
                report(f'{rel}：{endpoint} 输入协议不匹配：模板 {actual}，真实 binder {protocol}')
                continue
            if protocol == 'json':
                report(f'{rel}：未检查 {endpoint} 的 JSON 对象提供器字段；需要真实对象链路验证，不能按普通控件冒充已对账')
                continue
            offered.discard('csrf_token')
            if any('__JET_' in field for field in offered):
                report(f'{rel}：未检查 {endpoint} 的动态字段名')
                continue
            allowed = {field['name'] for field in spec.get('fields', [])}
            for field in sorted(offered - allowed):
                report(f'{rel}：字段 name="{field}" 不在 {endpoint} 的 DTO 字段表里；原生 binder 不折叠点号或数组下标')
            for field in spec.get('fields', []):
                if field.get('required') and field['name'] not in offered:
                    report(f'{rel}：{spec["dto"]} 的必填字段 "{field["name"]}" 没有出现在表单里（端点 {endpoint}）')
            checked_forms += 1

if checked_templates == 0:
    report(f'字段门禁未检查：范围 {prefix or root} 没有模板')
if violations:
    print(f'字段门禁未通过：{violations} 项错误或未检查项；已对账 {checked_forms} 个提交入口。', file=sys.stderr)
    sys.exit(1)
print(f'字段静态对账通过：{checked_forms} 个提交入口，契约 {len(contracts)} 条；不替代真实绑定与浏览器验证。')
