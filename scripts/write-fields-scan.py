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
from urllib.parse import parse_qs, urlsplit

if len(sys.argv) < 2:
    sys.exit('字段门禁未检查：缺少写端点契约文件参数（先跑 go run ./cmd/write-contracts）')
contracts = json.load(open(sys.argv[1], encoding='utf-8'))
if not contracts:
    # 空契约会让每个提交入口都「不在契约里」或全部静默通过；两种都不能当已对账。
    sys.exit('字段门禁未检查：写端点契约为空')

# JSON 对象提供器台账：由 scripts/write-object-keys.js 真实执行 provider 后产出，
# 键是「METHOD PATH」，值含该端点实际核对通过的 DTO。只有台账里确实跑过、
# 且 DTO 与当前契约一致的端点，才从「对象提供器未检查」里摘出。
OBJECT_PROVIDERS = {}
# 台账必须登记产出它的驱动脚本；字段门禁对照**当前**驱动文件判证据是否过期。
DRIVER_SOURCE = 'scripts/write-object-keys.js'
_provider_ledger = Path(sys.argv[1]).with_name('write-object-providers.json')
if _provider_ledger.is_file():
    OBJECT_PROVIDERS = json.loads(_provider_ledger.read_text(encoding='utf-8'))
    # 台账只允许登记真实契约里的端点；陈旧台账（端点已退役）必须显式报错，
    # 否则它会一直替一个不存在的端点「证明已核对」。
    stale = sorted(set(OBJECT_PROVIDERS) - set(contracts))
    if stale:
        sys.exit('字段门禁未检查：对象提供器台账含已退役端点（重跑 scripts/tests/run_write_object_providers.sh）：' + ', '.join(stale))


# STATIC_FIELD_SOURCES：模板用循环展开 name 的端点 → 该名字集合的 Go 侧静态来源。
#
# 只为「名字来自编译期常量表、且 Go 侧有测试钉住表 ⊆ DTO」的端点登记；
# 登记必须与该端点的当前 DTO 字段表完全一致，否则判过期。
STATIC_FIELD_SOURCES = {
    # internal/module/project/inbound/http/project_page.go 的 themeFieldGroups，
    # 由 TestThemeSettingsFieldGroupNamesInDTO 钉住每个 Name 都在 DTO 的 form tag 里。
    'POST /admin/themes/settings/save': {
        'go_test': 'internal/module/project/inbound/http:TestThemeSettingsFieldGroupNamesInDTO',
        'required': True,
        'fields': [
            'images.lazyLoad', 'images.skeleton',
            'colors.primary', 'colors.secondary', 'colors.accent', 'colors.success',
            'colors.warning', 'colors.danger', 'colors.text', 'colors.heading',
            'colors.background', 'colors.surface', 'colors.border',
            'typography.heading.color', 'typography.heading.weight', 'typography.heading.size',
            'typography.heading.spacing', 'typography.heading.font',
            'typography.body.color', 'typography.body.size', 'typography.body.line', 'typography.body.font',
            'typography.link.color', 'typography.link.hover', 'typography.link.underline',
            'button.background', 'button.color', 'button.radius', 'button.weight',
            'button.py', 'button.px', 'button.hoverBg', 'button.hoverColor',
            'button.borderWidth', 'button.borderStyle', 'button.borderColor', 'button.shadow',
            'surface.radius', 'surface.borderWidth', 'surface.borderColor', 'surface.shadow',
            'motion.duration', 'motion.easing', 'motion.entrance',
        ],
    },
}


def provider_source_hash(rel):
    """按台账里登记的源文件路径算当前指纹，用于判「provider 改过、台账过期」。

    台账与契约文件同目录，源路径可能相对仓库根（生产）或相对契约目录（测试夹具）：
    两处都试，取第一个存在的。都读不到时返回空，调用方按「证据过期」处理。
    """
    candidates = [Path(rel), Path(sys.argv[1]).resolve().parent / rel]
    for candidate in candidates:
        try:
            return hashlib.sha256(candidate.read_bytes()).hexdigest()[:16]
        except OSError:
            continue
    return ''


@lru_cache(maxsize=1)
def driver_source_hash():
    """当前对象提供器驱动的源码指纹，用于判「驱动改过、台账过期」。

    与 provider_source_hash 同样两处都试：仓库根（生产）或契约目录旁边（测试夹具）。
    读不到时返回空，调用方按「证据过期」处理 —— 宁可重跑，不可放过。
    """
    # 先试契约目录旁边（测试夹具），再试仓库根（生产）：生产的契约目录下没有
    # scripts/，夹具则把「当前驱动」放在契约目录旁，必须优先命中夹具那份。
    candidates = [Path(sys.argv[1]).resolve().parent / DRIVER_SOURCE, Path(DRIVER_SOURCE)]
    for candidate in candidates:
        try:
            return hashlib.sha256(candidate.read_bytes()).hexdigest()[:16]
        except OSError:
            continue
    return ''


def provider_ledger_valid(endpoint, spec):
    """台账是否仍能证明该端点已核对：DTO、字段表、provider 源码指纹都必须一致。

    任何一项变了（DTO 改名、字段增删、provider 逻辑改动）都说明旧证据已过期，
    必须重跑对账，不能拿旧台账继续摘掉未知项。
    """
    entry = OBJECT_PROVIDERS.get(endpoint)
    if not entry or entry.get('dto') != spec.get('dto'):
        return False
    # 名字集合必须一致。
    fields = sorted(field.get('name') for field in spec.get('fields', []))
    entry_fields = sorted(f.get('name') if isinstance(f, dict) else f for f in entry.get('fields', []))
    if entry_fields != fields:
        return False
    # required 不在名字集合里，必须由指纹单独覆盖：只比名字会放过
    # 「projectId 变必填、provider 却不产出它」这类破坏性变更。
    if entry.get('fieldsHash') != fields_fingerprint(spec):
        return False
    # 场景形状指纹：只能证明台账内部自洽，证明不了它出自当前驱动（见下一条）。
    if entry.get('caseShape') != case_shape_digest(entry):
        return False
    # 驱动定义指纹：对照**当前**驱动脚本文件，而不是台账里记的 cases。
    # 只比 caseShape 等于让台账自证 —— 驱动改了场景名/预期值/嵌套断言而台账没重跑时，
    # 台账里的 cases 与它自己的指纹照样一致，旧证据会被一直沿用。
    if entry.get('driver') != DRIVER_SOURCE:
        return False
    if not entry.get('driverHash') or entry.get('driverHash') != driver_source_hash():
        return False
    source = entry.get('source')
    if not source or entry.get('sourceHash') != provider_source_hash(source):
        return False
    return True


def _canonical(payload):
    """与 JS 侧一致的规范 JSON：键序稳定、无多余空白。

    台账由 Node 生成，Python 必须用同一种序列化，否则同一个事实会算出两个指纹。
    """
    return hashlib.sha256(
        json.dumps(payload, ensure_ascii=False, separators=(',', ':')).encode('utf-8')
    ).hexdigest()[:16]


def fields_fingerprint(spec):
    """字段契约指纹：名字 + required，任一变化都让旧台账失效。"""
    payload = sorted([[f.get('name'), bool(f.get('required'))] for f in spec.get('fields', [])],
                     key=lambda pair: pair[0])
    return _canonical(payload)


def case_shape_digest(entry):
    """场景形状指纹：场景名与它产出的键集合。

    驱动脚本改动场景（改名、增删场景、改字段）后，旧台账不再引用同一份证据。
    """
    cases = entry.get('cases') or []
    payload = sorted([[c.get('name', ''), sorted(c.get('keys', []))] for c in cases],
                     key=lambda pair: json.dumps(pair, ensure_ascii=False))
    return _canonical(payload)

# 扫描根：应用模板 + 公共 UI 模板 + 工具模板资产，三处属主都要覆盖。
# 只传一个根时保留旧行为，便于按模块定向执行。
TEMPLATE_ROOTS = (
    'internal/templates',
    'pkg/view/ui/templates',
    'pkg/builder',
)
# 参数形态：<contracts> [--module <相对路径前缀>] [扫描根...]
#   · --module 是**定向过滤**：三个正式扫描根不变，只把匹配该前缀的模板纳入对账；
#   · 其余位置参数是扫描根；都不给时用三个正式根。
extra = sys.argv[2:]
module_prefix = ''
if '--module' in extra:
    at = extra.index('--module')
    if at + 1 >= len(extra) or not extra[at + 1]:
        sys.exit('字段门禁未检查：--module 缺少取值')
    module_prefix = extra[at + 1]
    extra = extra[:at] + extra[at + 2:]
roots = extra if extra else list(TEMPLATE_ROOTS)
roots = [r for r in roots if r]
missing = [r for r in roots if not Path(r).is_dir()]
if missing:
    sys.exit('字段门禁未检查：扫描根不存在：' + ', '.join(missing))
if not roots:
    sys.exit('字段门禁未检查：没有扫描根')

root = roots[0]
prefix = module_prefix

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
                body = substitute_block_args(body, owner, yielded.group(1), token)
                return expand_template(owner, source=body, blocks=available, active=active + (identity,))
        return tag.group(0)

    return JET_TAG.sub(expand_tag, source)


BLOCK_PARAMS = re.compile(r'\{\{\s*block\s+[A-Za-z_]\w*\s*\(([^)]*)\)')
YIELD_ARGS = re.compile(r'([A-Za-z_]\w*)\s*=\s*("[^"]*"|\'[^\']*\')')


def substitute_block_args(body, owner, block_name, yield_token):
    """把 yield 的字面量具名实参代进 block 参数。

    片段里的 `name="{{field}}"` 是参数化的字段名；只按字面实参替换，
    非字面实参（变量、表达式）保持原样 —— 后者仍按动态字段名报未检查。
    """
    params_match = BLOCK_PARAMS.search(template_source(owner))
    if not params_match:
        return body
    params = {p.strip() for p in params_match.group(1).split(',') if p.strip()}
    call = BLOCK_START.sub('', yield_token)
    call = call[call.find('(') + 1:]
    values = {}
    for name, literal in YIELD_ARGS.findall(call):
        if name in params:
            values[name] = literal[1:-1]
    for name, literal in values.items():
        body = re.sub(r'\{\{\s*%s\s*\}\}' % re.escape(name), literal, body)
    return body


WRITE_METHODS = ('post', 'put', 'patch', 'delete')


def write_endpoint(attrs):
    for method in WRITE_METHODS:
        if attrs.get('hx-' + method):
            return method.upper(), attrs['hx-' + method]
    if attrs.get('method', '').lower() in WRITE_METHODS and attrs.get('action'):
        return attrs['method'].upper(), attrs['action']
    return None


HX_VALS_PAIR = re.compile(
    r'("[^"]+"|\'[^\']+\'|[A-Za-z_][\w.\-]*)\s*:\s*("[^"]*"|\'[^\']*\'|[^\s,}]+)')
HX_VALS_BARE = re.compile(r'^[A-Za-z_][\w.\-]*(?:,[A-Za-z_][\w.\-]*)*$')


def hx_vals_fields(value):
    """把 hx-vals 的静态字面量解析成具名字段集合；解析不了返回 None。

    htmx 4 的取值器先试 JSON（以 `{` 开头），失败再按 `name:value` / `name` 的
    松散语法解析。这里只认这两种、且值必须是字面量或数字：含 Jet 表达式或
    动态拼接的仍按「未检查」处理，不猜。
    返回 (字段名集合, None)；无法静态确定时返回 None。
    """
    raw = (value or '').strip()
    if not raw:
        return set()
    if raw.startswith('{'):
        try:
            parsed = json.loads(raw)
        except json.JSONDecodeError:
            return None
        if not isinstance(parsed, dict):
            return None
        keys = {str(k) for k in parsed}
    elif HX_VALS_BARE.match(raw):
        keys = {part.strip() for part in raw.split(',') if part.strip()}
    else:
        pairs = HX_VALS_PAIR.findall(raw)
        if not pairs:
            return None
        keys = {name.strip('"\'').strip() for name, _ in pairs}
    # htmx 会把 `a.b` 这类键合并成嵌套对象，DTO 的 form tag 不折叠点号：
    # 含点号的键仍按「无法静态对账」处理，不放宽。
    if any('.' in key for key in keys):
        return None
    return keys


def unresolved_endpoint(attrs):
    """写端点是否仍含未展开的 Jet 表达式（动态字段名 / 路径）。

    控件自身的 hx-* 与原生表单的 action 都算：只看其中一个会让
    action 里的 {{r.ID}} 混过掩码后变成 `__JET_…__` 却被当成静态路径。
    """
    for name, value in attrs.items():
        if name.startswith('hx-') and str(value or '').find('__JET_') >= 0:
            return True
    if attrs.get('action') and str(attrs['action']).find('__JET_') >= 0:
        return True
    return False


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
        for ancestor in self.elements:
            uncertain.update(set(ancestor['attrs']) & {'hx-inherit', 'hx-disinherit'})
            # 表单之外的祖先（section / div / table…）上的 hx-include / hx-vals 会经 htmx
            # 属性继承作用于后代请求，但这里没有、也不该按 selector 去合并它们 ——
            # 静态字面量同样只能报「未检查」，不能因为可解析就冒充已对账。
            if ancestor['tag'] != 'form':
                if 'hx-include' in ancestor['attrs']:
                    uncertain.add('hx-include')
                if 'hx-vals' in ancestor['attrs']:
                    uncertain.add('hx-vals')
                if 'hx-encoding' in ancestor['attrs']:
                    uncertain.add('hx-encoding')
            else:
                # 外层表单（按钮覆盖提交时）的 hx-include / hx-vals 会被 included_fields
                # 按字面 #id 合并；只有确实可静态解析时才不算未检查。
                include = ancestor['attrs'].get('hx-include')
                if include is not None and not include_is_literal(include):
                    uncertain.add('hx-include')
                vals = ancestor['attrs'].get('hx-vals')
                if vals is not None and not hx_vals_fields(vals):
                    uncertain.add('hx-vals')
        # 元素自身：hx-include 的字面 id 选择器可静态解析（见 included_fields），
        # hx-vals 的静态字面量会并入 offered；解析不了时照旧报未检查，不放宽判据。
        uncertain.update(set(element['attrs']) & {'hx-inherit', 'hx-disinherit'})
        vals = element['attrs'].get('hx-vals')
        if vals is not None and not hx_vals_fields(vals):
            uncertain.add('hx-vals')
        include = element['attrs'].get('hx-include')
        if include is not None and not include_is_literal(include):
            uncertain.add('hx-include')
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
                    vals = hx_vals_fields(attrs.get('hx-vals'))
                    if vals:
                        offered |= vals
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
                vals = hx_vals_fields(attrs.get('hx-vals'))
                if vals:
                    offered |= vals
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


# 二元字面量分支（{{if C}}/a{{else}}/b{{end}}）静态可判定：两个端点都是确定的
# 本站路径，只是按条件二选一。与 check-write-endpoints.sh 同一判据：展开成两条
# 端点分别对账，而不是降级成「动态端点未检查」；含变量拼接、三支以上或非 / 开头的
# 仍保持未检查。
BINARY_FULL = re.compile(
    r'\{\{\s*if\s+[^}]+\}\}\s*(/[^\s"\'<>{}]*)\s*\{\{\s*else\s*\}\}\s*(/[^\s"\'<>{}]*)\s*\{\{\s*end\s*\}\}',
    re.S)
BINARY_SEG = re.compile(
    r'(/[^\s"\'<>{}]*?)\{\{\s*if\s+[^}]+\}\}\s*([^\s"\'{}]*?)\s*\{\{\s*else\s*\}\}\s*([^\s"\'{}]*?)\s*\{\{\s*end\s*\}\}',
    re.S)


def expand_binary_endpoints(src):
    src = BINARY_FULL.sub(lambda m: m.group(1) + '" data-alt-endpoint="' + m.group(2), src)
    return BINARY_SEG.sub(
        lambda m: m.group(1) + m.group(2) + '" data-alt-endpoint="' + m.group(1) + m.group(3), src)




def report(message):
    global violations
    print(message)
    violations += 1


def check_endpoint(rel, method, urls, attrs, offered):
    """对账一张表单（或其二元分支的两个端点）；返回是否已对账。"""
    global checked_forms
    passed = True
    for url in urls:
        parsed = urlsplit(url)
        if '__JET_' in parsed.path or not parsed.path.startswith('/') or parsed.netloc or parsed.scheme:
            report(f'{rel}：未检查动态或非本站端点 {method} {url}；不使用宽泛前缀匹配')
            passed = False
            continue
        endpoint = method + ' ' + parsed.path
        spec = contracts.get(endpoint)
        if spec is None:
            report(f'{rel}：端点 {endpoint} 不在写端点契约里（先重跑 go run ./cmd/write-contracts）')
            passed = False
            continue
        protocol = spec.get('protocol')
        if protocol not in ('form', 'multipart', 'json', 'query', 'nobody', 'raw') or spec.get('reason'):
            report(f'{rel}：未检查 {endpoint}：' + spec.get('reason', '契约缺少确定输入协议'))
            passed = False
            continue
        encoding = attrs.get('hx-encoding') or attrs.get('enctype') or ''
        actual = 'json' if attrs.get('data-write-builder') else ('multipart' if encoding == 'multipart/form-data' else 'form')
        if protocol == 'nobody':
            # 处理器完全不读 body：表单只能带 CSRF 这类框架字段，业务字段会被静默丢弃。
            business = {f for f in offered if f != 'csrf_token'}
            if business:
                report(f'{rel}：{endpoint} 的处理器不读请求体，表单提交的业务字段会被静默丢弃：'
                       + ', '.join(sorted(business)[:5]))
                passed = False
                continue
            checked_forms += 1
            continue
        if protocol == 'raw':
            # 原始字节协议（支付回调、采集 beacon）：由外部协议定义，不由模板提交。
            report(f'{rel}：{endpoint} 是原始字节协议端点，不应由界面表单提交')
            passed = False
            continue
        if protocol == 'query':
            # 显式查询串端点：键在 action URL 的 query 上，body 不参与。
            if attrs.get('data-write-builder'):
                report(f'{rel}：{endpoint} 是查询串端点，不能用 data-write-builder 提交 JSON 体')
                passed = False
                continue
            if offered - {'csrf_token'}:
                report(f'{rel}：{endpoint} 是查询串端点，表单体字段不会生效：'
                       + ', '.join(sorted(offered - {'csrf_token'})[:5]))
                passed = False
                continue
            allowed_query = {field['name'] for field in spec.get('fields', [])}
            for key in parse_qs(parsed.query, keep_blank_values=True):
                if key == 'csrf_token':
                    continue
                if key not in allowed_query:
                    report(f'{rel}：查询串键 "{key}" 不在 {endpoint} 的 DTO 字段表里')
                    passed = False
            checked_forms += 1
            continue
        if protocol != actual:
            report(f'{rel}：{endpoint} 输入协议不匹配：模板 {actual}，真实 binder {protocol}')
            passed = False
            continue
        if protocol == 'json':
            if not provider_ledger_valid(endpoint, spec):
                report(f'{rel}：未检查 {endpoint} 的 JSON 对象提供器字段；需要真实对象链路验证'
                       f'（重跑 scripts/tests/run_write_object_providers.sh：DTO {spec.get("dto") or "?"}、'
                       '字段表、provider 源码指纹与驱动定义指纹必须与台账一致 ——'
                       '改了驱动场景/预期值/嵌套断言也会让旧台账失效）')
                passed = False
                continue
            # 模板声明的 builder 必须与台账登记的 provider 一致：写错 builder 名会让
            # 页面提交空对象，而台账仍替这个端点「证明已核对」。
            declared = attrs.get('data-write-builder')
            recorded = OBJECT_PROVIDERS[endpoint].get('provider')
            if declared and recorded and declared != recorded:
                report(f'{rel}：端点 {endpoint} 声明 data-write-builder="{declared}"，'
                       f'台账核对的是 provider "{recorded}" —— 模板与对象链路不一致')
                passed = False
                continue
            # 台账与声明都一致：该端点已真实核对；继续核对二元分支的另一端点。
            continue
        offered.discard('csrf_token')
        # 变体是同一端点的第二个输入形状（入口按请求字段分叉）。
        variant_shapes = spec.get('variants') or []
        shapes = [spec.get('fields', [])] + [v.get('fields', []) for v in variant_shapes]
        allowed = {field['name'] for shape in shapes for field in shape}

        # 提交入口里**写死**的字段名必须落在契约里，即使这个端点还有动态名。
        # 否则「登记了动态来源」会顺带放过同表单里拼错的静态字段名。
        static_offered = {f for f in offered if '__JET_' not in f}
        for field in sorted(static_offered - allowed):
            report(f'{rel}：字段 name="{field}" 不在 {endpoint} 的 DTO 字段表里；原生 binder 不折叠点号或数组下标')
            passed = False

        if any('__JET_' in field for field in offered):
            # 动态字段名只有在该端点登记了「Go 侧静态来源」时才可对账：
            # 模板循环展开的名字来自编译期常量表，且 Go 侧有测试钉住表 ⊆ DTO。
            # 没有登记就如实报未检查 —— 不按模板名或端点白名单放过。
            static_src = STATIC_FIELD_SOURCES.get(endpoint)
            if static_src is None:
                report(f'{rel}：未检查 {endpoint} 的动态字段名')
                passed = False
                continue
            allowed_dynamic = set(static_src.get('fields', []))
            if not allowed_dynamic:
                report(f'{rel}：{endpoint} 登记了静态字段来源但字段表为空')
                passed = False
                continue
            dynamic_only = {f for f in offered if '__JET_' in f}
            if not dynamic_only and static_src.get('required'):
                report(f'{rel}：{endpoint} 的动态字段一个都没出现在表单里')
                passed = False
                continue
            # 动态名字本身不参与逐名比对（由 Go 表的测试保证 ⊆ DTO）；
            # 但登记的每个名字都必须仍在当前 DTO 字段表里，否则登记已过期。
            dto_fields = {f['name'] for f in spec.get('fields', [])}
            stale = sorted(allowed_dynamic - dto_fields)
            if stale:
                report(f'{rel}：{endpoint} 的动态字段来源登记已过期（DTO 已无这些字段：' +
                       ', '.join(stale[:5]) + ('…' if len(stale) > 5 else '') + '）')
                passed = False
                continue
            checked_forms += 1
            continue

        if variant_shapes:
            # 有变体不等于「随便哪支都行」：至少要有一支的必填字段在表单里齐备。
            # 所有分支的必填字段都缺失时，任何真实提交都必然被拒 —— 这是错误，
            # 不能因为「字段并集包含」就放过。
            complete = [shape for shape in shapes
                        if all(f['name'] in offered for f in shape if f.get('required'))]
            if not complete:
                missing = '；'.join(
                    (shape_dto_name(spec, i) or '分支') + ' 缺 ' + ', '.join(
                        sorted(f['name'] for f in shape if f.get('required') and f['name'] not in offered))
                    for i, shape in enumerate(shapes)
                    if any(f.get('required') and f['name'] not in offered for f in shape))
                report(f'{rel}：{endpoint} 的每个输入分支都缺必填字段，表单无法提交成功（{missing}）')
                passed = False
        else:
            for field in spec.get('fields', []):
                if field.get('required') and field['name'] not in offered:
                    report(f'{rel}：{spec["dto"]} 的必填字段 "{field["name"]}" 没有出现在表单里（端点 {endpoint}）')
                    passed = False
    return passed


def shape_dto_name(spec, index):
    """取第 index 个输入形状的 DTO 名（0 是主契约，其余是变体）。"""
    if index == 0:
        return spec.get('dto') or ''
    variants = spec.get('variants') or []
    if index - 1 < len(variants):
        return variants[index - 1].get('dto') or ''
    return ''


for scan_root in roots:
    root = scan_root
    for dirpath, _, files in os.walk(root):
        for name in sorted(files):
            if not name.endswith(('.html', '.jet')):
                continue
            path = os.path.join(dirpath, name)
            rel = os.path.relpath(path)
            if prefix and prefix not in rel:
                continue
            # testdata 是夹具，不是产品模板：它故意提交假端点，不能当产品表单对账。
            if rel.startswith('internal/templates/testdata/'):
                continue
            checked_templates += 1
            text = expand_binary_endpoints(expand_template(Path(path).resolve()))
            parser = FormParser()
            parser.feed(JET_TAG.sub(mask_jet, text))
            for (method, url), attrs, offered, uncertain in parser.actions():
                if uncertain:
                    report(f'{rel}：未检查 {method} {url} 的参数选择/继承声明：' + ', '.join(sorted(uncertain)))
                    continue
                urls = [url]
                if attrs.get('data-alt-endpoint'):
                    urls.append(attrs['data-alt-endpoint'])
                if check_endpoint(rel, method, urls, attrs, offered):
                    checked_forms += 1

if checked_templates == 0:
    report(f'字段门禁未检查：范围 {prefix or ", ".join(roots)} 没有模板')
if violations:
    print(f'字段门禁未通过：{violations} 项错误或未检查项；已对账 {checked_forms} 个提交入口。', file=sys.stderr)
    sys.exit(1)
print(f'字段静态对账通过：{checked_forms} 个提交入口，契约 {len(contracts)} 条；不替代真实绑定与浏览器验证。')
