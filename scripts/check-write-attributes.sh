#!/usr/bin/env bash
# HTML 写请求属性契约：HTMX 原生编码与交换，write-base 只做共享接线。
# judge 入口与全模板扫描共用解析器；旧模块未迁导致失败时，不增加兼容白名单。
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PARSER="$(mktemp)"
trap 'rm -f "$PARSER"' EXIT

# Python 代码落临时文件，保留 stdin 给 judge 夹具使用。
read -r -d '' SOURCE <<'PY' || true
import re
import sys
from pathlib import Path

ROOT = Path(sys.argv[1])
MODE = sys.argv[2] if len(sys.argv) > 2 else ''
BASE = ROOT / 'pkg/view/ui/assets/js/ui/write-base.js'
FLAGS = {'data-write-reload', 'data-write-status', 'data-write-host'}
# result 是 Jet 的 hx-target 钩子，不是基座执行属性。
HOOKS = {'data-write-result'}
VALUES = {'data-write-nav', 'data-write-close', 'data-write-builder', 'data-write-session'}
REMOVED = {
    'data-write-' + name for name in (
        'url', 'api', 'mode', 'confirm', 'extra', 'outcome', 'number', 'float', 'bool',
        'array', 'skip-empty', 'flash', 'flash-target', 'html', 'json', 'json-single',
        'json-wrap', 'lines', 'keep-empty', 'ids-scope', 'rows', 'object',
        'items', 'latest', 'handler')
} | {'data-close-on-ok', 'data-reload-on-ok'}
WRITE_METHODS = ('hx-post', 'hx-put', 'hx-patch', 'hx-delete')
TAG_START = re.compile(r'<([a-z][a-z0-9:-]*)(?=[\s/>])', re.I)
WS = re.compile(r'\s+')
ATTR = re.compile(r'''([^\s"'<>/=]+)(?:\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'=<>]+)))?''')
TAB = '\t'


def tags(text):
    # 注释里的历史例子不参与判定；只在引号外的 > 结束标签。
    text = re.sub(r'\{\*.*?\*\}|<!--.*?-->', '', text, flags=re.S)
    pos = 0
    while True:
        match = TAG_START.search(text, pos)
        if not match:
            return
        i, quote = match.end(), None
        while i < len(text):
            char = text[i]
            if quote:
                if char == quote:
                    quote = None
            elif char in ('"', "'"):
                quote = char
            elif char == '>':
                yield match.group(1).lower(), WS.sub(' ', text[match.start():i + 1])
                break
            elif text.startswith('{{', i):
                end = text.find('}}', i)
                if end < 0:
                    raise ValueError('标签内 Jet 表达式未闭合')
                i = end + 2
                continue
            i += 1
        else:
            raise ValueError('标签未闭合: ' + text[match.start():match.end()])
        pos = i + 1


def attrs_of(kind, tag):
    body = tag[1 + len(kind):].removesuffix('>').removesuffix('/')
    return [(m.group(1).lower(), next((v for v in m.groups()[1:] if v is not None), None))
            for m in ATTR.finditer(body)]


def judge(kind, tag):
    attrs = attrs_of(kind, tag)
    own = {}
    errors = []
    for name, value in attrs:
        own.setdefault(name, value)
        if name in REMOVED:
            errors.append(('E1', name, '已删除；使用原生控件字段、HTMX 与 Jet HTML'))
        elif name.startswith('data-write-') and name not in FLAGS | HOOKS | VALUES:
            errors.append(('E4', name, '未登记的写属性'))
        elif name in FLAGS | HOOKS and value is not None:
            errors.append(('E5a', name, '行为开关与结果钩子必须写成裸属性'))
        elif name in VALUES and not (value or '').strip():
            errors.append(('E4', name, '必须声明非空值'))
        if name == 'data-write-session' and value != 'visitor':
            errors.append(('E4', name, '访客会话必须显式声明 visitor；后台不声明此属性'))
        if name == 'data-json-type' or (name == 'hx-ext' and 'form-json' in re.split(r'[,\s]+', value or '')):
            errors.append(('E7', name, '旧 form-json 字段整形已退役；复杂对象使用明确 JSON 输入'))
    # 单标签判据不推断父表单；按钮仅覆盖 mode/端点时，无需复制 form 的完整声明。
    if kind not in ('form', 'button') or 'data-write' not in own:
        return errors
    methods = [name for name in WRITE_METHODS if name in own]
    if kind == 'form':
        if len(methods) != 1:
            errors.append(('E2', 'hx-post', '写表单必须声明且只声明一个 HTMX 写方法'))
        for name in ('hx-target', 'hx-swap'):
            if not (own.get(name) or '').strip():
                errors.append(('E2', name, '写表单必须显式声明成功目标与交换方式'))
    elif methods and (own.get('type') or '').lower() != 'button':
        errors.append(('E2', 'type', '有独立 HTMX 端点的按钮必须 type="button"，避免原生 submit 双执行'))
    if len(methods) > 1 and kind == 'button':
        errors.append(('E2', 'hx-post', '按钮只能声明一个 HTMX 写方法'))
    for method in methods:
        url = own.get(method) or ''
        if not url.startswith('/') or url.startswith('//') or '\\' in url or re.search(r'[\x00-\x1f]', url):
            errors.append(('E3b', method, '必须使用本站绝对路径；不按 /api/ 前缀判断协议'))
    return errors


def reverse_errors():
    errors = []
    if not BASE.is_file():
        errors.append('写基座不存在: ' + str(BASE))
    else:
        source = BASE.read_text(encoding='utf-8')
        for name in sorted(FLAGS | VALUES | {'data-write'}):
            literal = re.escape(name) + '|' + re.escape('[' + name + ']')
            if not re.search(r'''['"](?:''' + literal + r''')['"]''', source):
                errors.append('登记属性在 write-base 中没有消费点: ' + name)
    return errors


def main():
    bad = 0
    if MODE == 'judge':
        for raw in sys.stdin:
            if not raw.strip():
                continue
            kind, sep, tag = raw.rstrip('\n').partition(TAB)
            errors = judge(kind, tag) if sep else [('E0', '', '缺少 kind TAB 分隔')]
            for code, name, message in errors:
                print(TAB.join((code, tag, name, message)))
            bad += len(errors)
        print('BAD' + TAB + str(bad))
        return 1 if bad else 0
    # 三个模板属主都要扫：应用模板、公共 UI 模板、工具模板资产。
    # 只扫 internal/templates 会让 pkg 侧的写属性/控件绕过判据。
    files = []
    for root_name in ('internal/templates', 'pkg/view/ui/templates', 'pkg/builder'):
        base = ROOT / root_name
        if not base.is_dir():
            print(f'扫描根不存在：{root_name}', file=sys.stderr)
            bad += 1
            continue
        files.extend(p for p in base.rglob('*')
                     if p.suffix in ('.html', '.jet') and (not MODE or MODE in str(p)))
    files.sort()
    for path in files:
        try:
            for kind, tag in tags(path.read_text(encoding='utf-8-sig')):
                for code, name, message in judge(kind, tag):
                    print(f'{path.relative_to(ROOT)}: {code} {name}: {message}', file=sys.stderr)
                    bad += 1
        except (OSError, ValueError) as error:
            print(f'{path.relative_to(ROOT)}: 标签提取失败: {error}', file=sys.stderr)
            bad += 1
    # 反向登记即使没有模板违规也必须影响退出码，不能打印错误后仍返回成功。
    for error in reverse_errors():
        print(error, file=sys.stderr)
        bad += 1
    if bad:
        print(f'写请求属性门禁失败：{bad} 项；见 internal/templates/CLAUDE.md「写操作」。', file=sys.stderr)
        return 1
    print(f'写请求属性合规：扫描 {len(files)} 模板，HTMX 单执行入口及属性消费点核对通过。')
    return 0


sys.exit(main())
PY
printf '%s\n' "$SOURCE" > "$PARSER"
python3 "$PARSER" "$ROOT" "$@"
