#!/usr/bin/env python3
"""核对实际基座能力的声明组合，不从 data-write 的值推导业务或 JSON 形状。"""
import re
import sys
from html.parser import HTMLParser
from pathlib import Path


def read_template(path, stack=()):
    path = Path(path).resolve()
    if path in stack:
        return ''
    src = path.read_text(encoding='utf-8')
    src = re.sub(r'\{\*.*?\*\}', '', src, flags=re.S)
    def include(match):
        child = path.parent / match.group(1)
        if not child.is_file():
            return match.group(0)
        return read_template(child, (*stack, path))
    return re.sub(r'{{[ \t]*include[ \t]+"([^"]+)".*?}}', include, src, flags=re.S)


class CombinationParser(HTMLParser):
    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.forms = []
        self.form = None
        self.actions = []

    def handle_starttag(self, tag, pairs):
        attrs = dict(pairs)
        if tag == 'form':
            self.form = attrs
            self.forms.append(attrs)
        if tag == 'form' or any('hx-' + method in attrs for method in ('post', 'put', 'patch', 'delete')):
            self.actions.append((attrs, self.form))

    def handle_endtag(self, tag):
        if tag == 'form':
            self.form = None


path = sys.argv[1]
source = read_template(path)
source = re.sub(r'\{\{.*?\}\}', '__JET_DYNAMIC__', source, flags=re.S)
parser = CombinationParser()
parser.feed(source)
problems = []
for attrs, parent in parser.actions:
    if 'form' in attrs:
        parent = next((form for form in parser.forms if form.get('id') == attrs['form']), None)
    merged = {**(parent or {}), **attrs}
    for name in ('data-write-nav', 'data-write-close'):
        value = merged.get(name) or ''
        # 当前导航读取成功 Jet 片段里的链接，关闭读取 DOM 节点；两者都不是 URL 插值器。
        if value.startswith('/') or '://' in value or '{' in value:
            problems.append(f'{name} 必须声明节点选择器，不接收 URL 或 JSON id 插值')
    encoding = merged.get('hx-encoding') or merged.get('enctype')
    if merged.get('data-write-builder') and encoding == 'multipart/form-data':
        problems.append('JSON 对象提供器不能与 multipart 编码同时声明；一个端点一种输入协议')

for problem in problems:
    print(f'{path}：{problem}')
sys.exit(1 if problems else 0)
