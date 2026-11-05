#!/usr/bin/env bash
# HTMX 写端点按 METHOD+PATH 与真实路由精确对账，不按 /api 过滤或用动态前缀放行。
# 缺路由/数据库、动态路径与空扫描范围均报未检查并返回非零；可传模板相对路径前缀。
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT" || exit 1

routes_file="$(mktemp)" || exit 1
trap 'rm -f "$routes_file"' EXIT
if ! WP_DUMP_ROUTES=1 go test ./internal/routers/ -run '^TestDumpRoutesForAudit$' -count=1 -v > "$routes_file" 2>&1; then
  printf '端点存在性未检查：真实路由装配失败；检查数据库与审计配置。\n' >&2
  exit 1
fi

python3 - "$routes_file" "${1:-}" <<'PY'
import re
import sys
from html.parser import HTMLParser
from pathlib import Path
from urllib.parse import urlsplit

methods = ('POST', 'PUT', 'PATCH', 'DELETE')
routes = set()
for line in Path(sys.argv[1]).read_text(encoding='utf-8').splitlines():
    parts = line.split()
    if len(parts) == 2 and parts[0] in methods and parts[1].startswith('/'):
        routes.add(tuple(parts))
if not routes:
    sys.exit('端点存在性未检查：没有获得真实写路由表。')

found = {}
class EndpointParser(HTMLParser):
    def handle_starttag(self, tag, pairs):
        attrs = dict(pairs)
        for method in methods:
            key = 'hx-' + method.lower()
            if key in attrs:
                found.setdefault((method, attrs[key] or ''), set()).add(current)

root = Path('internal/templates')
for path in sorted(root.rglob('*')):
    if path.suffix not in ('.html', '.jet'):
        continue
    current = str(path.relative_to(root))
    if not current.startswith(sys.argv[2]):
        continue
    # testdata/ 是浏览器/组件测试的夹具页（假端点 POST /write/form 之类），
    # 不是产品界面，不该要求它在真实路由表里注册。
    if 'testdata/' in current or current.startswith('testdata'):
        continue
    source = path.read_text(encoding='utf-8')
    source = re.sub(r'\{\*.*?\*\}', '', source, flags=re.S)
    # 二元字面量分支（{{if C}}/a{{else}}/b{{end}}）静态可判定：两个端点都是确定的
    # 本站路径，只是按条件二选一。把它们展开成两条端点分别对账，而不是降级成
    # 「动态端点未检查」；含变量拼接、三支以上或非 / 开头的仍保持未检查。
    #   形态一：整条端点二选一  hx-post="{{if c}}/a{{else}}/b{{end}}"
    binary_full = re.compile(r'\{\{\s*if\s+[^}]+\}\}\s*(/[^\s"\'<>{}]*)\s*\{\{\s*else\s*\}\}\s*(/[^\s"\'<>{}]*)\s*\{\{\s*end\s*\}\}', re.S)
    source = binary_full.sub(lambda m: m.group(1) + '" data-alt-endpoint="' + m.group(2), source)
    #   形态二：路径中/末段二选一  hx-post="/api/x/{{if c}}create{{else}}update{{end}}"
    #   展开成两条完整路径分别对账（前缀固定、两段都是字面量）。
    binary_seg = re.compile(r'(/[^\s"\'<>{}]*?)\{\{\s*if\s+[^}]+\}\}\s*([^\s"\'{}]*?)\s*\{\{\s*else\s*\}\}\s*([^\s"\'{}]*?)\s*\{\{\s*end\s*\}\}', re.S)
    source = binary_seg.sub(lambda m: m.group(1) + m.group(2) + '" data-alt-endpoint="' + m.group(1) + m.group(3), source)
    source = re.sub(r'\{\{.*?\}\}', '__JET_DYNAMIC__', source, flags=re.S)
    EndpointParser().feed(source)

if not found:
    sys.exit('没有 HTMX 写端点，端点存在性未验证；检查模板范围。')

violations = 0
for (method, url), files in sorted(found.items()):
    where = ', '.join(sorted(files))
    try:
        parsed = urlsplit(url)
        static = (parsed.path.startswith('/') and not parsed.netloc and not parsed.scheme
                  and '__JET_DYNAMIC__' not in parsed.path and '\\' not in url)
    except ValueError:
        static = False
    if not static:
        print(f'{where}：未检查动态或非本站端点 {method} {url}；不使用前缀匹配', file=sys.stderr)
        violations += 1
    elif (method, parsed.path) not in routes:
        print(f'{where}：HTMX 写端点 {method} {parsed.path} 没有注册（方法与路径均须匹配）', file=sys.stderr)
        violations += 1

if violations:
    sys.exit(1)
print(f'写端点精确对账通过：{len(found)} 个端点 / {len(routes)} 条真实写路由。')
PY
