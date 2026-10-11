#!/usr/bin/env bash
# Page authorization audit: source + reviewed route snapshot + read-only menu query.
set -euo pipefail
cd "$(dirname "$0")/.."
command -v psql >/dev/null || { echo "psql is required" >&2; exit 1; }
command -v python3 >/dev/null || { echo "python3 is required" >&2; exit 1; }
yaml_db() {
    awk -v key="$1" '/^database:/{in_db=1;next} /^[a-zA-Z]/{in_db=0} in_db && $1==key":"{print $2;exit}' config.yaml
}
DB_HOST="${DB_HOST:-${GOWP_DATABASE_HOST:-$(yaml_db host)}}"
DB_PORT="${DB_PORT:-${GOWP_DATABASE_PORT:-$(yaml_db port)}}"
DB_USER="${DB_USER:-${GOWP_DATABASE_USER:-$(yaml_db user)}}"
DB_PASS="${DB_PASSWORD:-${GOWP_DATABASE_PASSWORD:-$(yaml_db password)}}"
DB_NAME="${DB_NAME:-${GOWP_DATABASE_DBNAME:-$(yaml_db dbname)}}"
tmp_menu=$(mktemp)
trap 'rm -f "$tmp_menu"' EXIT
# No Go bootstrap: SetupRoutes also synchronizes permission/policy tables.
# This fixed SELECT runs with a read-only session default; this is not a role-level restriction.
PGPASSWORD="$DB_PASS" PGOPTIONS="-c default_transaction_read_only=on" \
    psql -X -v ON_ERROR_STOP=1 -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -tAF'|' -c \
    "SELECT m.path, coalesce(p.api_path,''), coalesce(p.api_method,''), coalesce(mp.permission_code,'')
       FROM sys_menus m
       LEFT JOIN sys_menu_permission mp ON mp.menu_id=m.id
       LEFT JOIN sys_permission p ON p.permission_code=mp.permission_code AND p.status=1
      WHERE m.type=2 AND m.status=1 AND m.deleted_at IS NULL AND m.path<>''
      ORDER BY m.path;" > "$tmp_menu"
python3 - "$tmp_menu" <<'PY'
import pathlib
import re
import sys

# Borrowed virtual API objects: intentionally declared, no route at this path.
VIRTUAL = {
    ('GET', '/api/i18n/list'): ('i18n:view', 'I18nView', 'internal/module/admin/inbound/http/admin_pages_router.go'),
    ('POST', '/api/seo/audit'): ('seo:audit', 'SEOAudit', 'internal/module/publication/inbound/http/publication_router.go'),
}
# Retained SSR route as permission object; distinct from a virtual API object.
PAGE_OBJECTS = {
    ('GET', '/admin/orders/overview'): ('order:overview', 'OrderOverview', 'internal/module/order/inbound/http/order_page_router.go'),
}


def uncomment(text):
    tokens = re.compile(r'"(?:\\.|[^"\\])*"|`[^`]*`|/\*[\s\S]*?\*/|//[^\n]*')
    return tokens.sub(lambda m: '\n' * m.group().count('\n')
                      if m.group().startswith(('//', '/*')) else m.group(), text)


def scan(text):
    result = set()
    for line in uncomment(text).splitlines():
        page = re.search(r'\.GET\("([^" ]+)"', line)
        auth = re.search(r'adminui\.PageAuthz(As)?\("([^" ]+)"(?:,\s*http\.Method([A-Za-z]+))?\)', line)
        if not page or not auth:
            continue
        if auth[1] and not auth[3]:
            raise ValueError('unrecognized explicit act: ' + line)
        result.add((page[1], auth[2], auth[3].upper() if auth[1] else 'GET'))
    return result


def has_declaration(source, constants, key, entry):
    method, obj = key
    perm, symbol, _ = entry
    declaration = (r'permission\.Declare\(\s*http\.Method' + re.escape(method.title()) +
                   r'\s*,\s*"' + re.escape(obj) + r'"\s*,\s*permission\.' + re.escape(symbol) + r'\s*\)')
    constant = r'\b' + re.escape(symbol) + r'\s+Perm\s*=\s*"' + re.escape(perm) + r'"'
    return bool(re.search(declaration, uncomment(source)) and re.search(constant, uncomment(constants)))


def validate(code, menu, routes, exemptions, virtual, page_objects, declarations):
    errors = []
    used_exemptions, used_virtual, used_pages = set(), set(), set()
    grouped = {}
    for page, obj, act, perm in menu:
        grouped.setdefault(page, set()).add((act, obj, perm))
    for page, bindings in grouped.items():
        if page == '/admin':
            if any(obj or perm for act, obj, perm in bindings):
                errors.append('/admin unexpectedly binds a permission')
            continue
        hits = {(act, obj) for path, obj, act in code if path == page.removeprefix('/admin')}
        if not hits and page in exemptions:
            used_exemptions.add(page)
            continue
        if not hits:
            errors.append(page + ': missing page authorization')
        allowed = {(act, obj) for act, obj, perm in bindings if act and obj and perm}
        for act, obj, perm in bindings:
            key = (act, obj)
            if not act or not obj or not perm:
                errors.append(page + ': missing active permission binding')
            elif key in virtual:
                if virtual[key] != perm or key not in declarations:
                    errors.append(page + ': missing virtual object declaration ' + repr(key))
                if key in routes:
                    errors.append(page + ': virtual object now has a route ' + repr(key))
                used_virtual.add(key)
            elif obj.startswith('/api/'):
                if key not in routes:
                    errors.append(page + ': missing route or virtual object registration ' + repr(key))
            elif page_objects.get(key) == perm:
                if key not in routes or key not in declarations:
                    errors.append(page + ': missing page-object route/declaration ' + repr(key))
                used_pages.add(key)
            else:
                errors.append(page + ': unregistered non-API object ' + repr(key))
        for hit in hits:
            if hit not in allowed:
                errors.append(page + ': foreign obj/act ' + repr(hit) + '; menu bindings ' + repr(sorted(allowed)))
    errors.extend('stale page exemption: ' + x for x in exemptions - used_exemptions)
    errors.extend('stale virtual object: ' + repr(x) for x in set(virtual) - used_virtual)
    errors.extend('stale page object: ' + repr(x) for x in set(page_objects) - used_pages)
    return errors


def self_test():
    a, b = ('GET', '/api/a'), ('GET', '/api/b')
    menu = [('/admin/a', a[1], a[0], 'a:view'), ('/admin/b', b[1], b[0], 'b:view')]
    code = {('/a', a[1], a[0]), ('/b', b[1], b[0])}
    count = 0
    def expect(name, errors, red):
        nonlocal count
        if bool(errors) != red:
            raise ValueError('self-test failed: ' + name)
        count += 1
    def check(c=code, m=menu, r={a, b}, e=set(), v={}, p={}, d=set()):
        return validate(c, m, r, e, v, p, d)
    expect('normal', check(), False)
    expect('foreign menu', check(c={('/a', b[1], b[0]), ('/b', b[1], b[0])}), True)
    expect('wrong act', check(c={('/a', a[1], 'POST'), ('/b', b[1], b[0])}), True)
    expect('missing route', check(r={b}), True)
    expect('registered virtual', check(r={b}, v={a:'a:view'}, d={a}), False)
    expect('deleted exception', check(r={b}, d={a}), True)
    expect('deleted declaration', check(r={b}, v={a:'a:view'}), True)
    expect('stale virtual', check(v={a:'a:view'}, d={a}), True)
    comment = '// pages.GET("/a", adminui.PageAuthz("/api/a"), h)\n/* pages.GET("/a", adminui.PageAuthz("/api/a"), h) */'
    expect('comment scanner', scan(comment), False)
    expect('comment cannot replace route', check(c=scan(comment) | {('/b', b[1], b[0])}), True)
    expect('stale exemption', check(e={'/admin/a'}), True)
    pk = ('GET', '/admin/overview')
    pm = [('/admin/overview', pk[1], pk[0], 'overview:view')]
    pc = {('/overview', pk[1], pk[0])}
    expect('retained page object', check(c=pc, m=pm, r={pk}, p={pk:'overview:view'}, d={pk}), False)
    expect('unregistered page object', check(c=pc, m=pm, r={pk}), True)
    decl = 'permission.Declare(http.MethodGet, "/api/a", permission.A)'
    entry = ('a:view','A','')
    expect('source declaration', not has_declaration(decl, 'A Perm = "a:view"', a, entry), False)
    expect('commented declaration', has_declaration('// '+decl, 'A Perm = "a:view"', a, entry), False)
    expect('deleted declaration source', has_declaration('', 'A Perm = "a:view"', a, entry), False)
    print('Page authz self-tests: %d passed' % count)


try:
    self_test()
    code = set()
    for path in pathlib.Path('internal').rglob('*.go'):
        if not path.name.endswith('_test.go'):
            code.update(scan(path.read_text()))
    if not code:
        raise ValueError('no actual adminui.PageAuthz registrations found')
    menu = [tuple(x.split('|')) for x in pathlib.Path(sys.argv[1]).read_text().splitlines() if x]
    routes = {tuple(x.split()) for x in pathlib.Path('internal/routers/testdata/routes.snapshot').read_text().splitlines() if x}
    if not menu or any(len(x)!=4 for x in menu) or not routes or any(len(x)!=2 for x in routes):
        raise ValueError('empty or malformed input inventories')
    exemptions = {x.split('#',1)[0].strip() for x in pathlib.Path('scripts/page-get-authz-allow.txt').read_text().splitlines()}
    exemptions.discard('')
    constants = pathlib.Path('internal/permission/codes.go').read_text()
    declarations = {key for key, entry in {**VIRTUAL, **PAGE_OBJECTS}.items()
                    if has_declaration(pathlib.Path(entry[2]).read_text(), constants, key, entry)}
    errors = validate(code, menu, routes, exemptions,
                      {k:v[0] for k,v in VIRTUAL.items()}, {k:v[0] for k,v in PAGE_OBJECTS.items()}, declarations)
    if errors:
        for error in errors:
            print('FAIL: '+error, file=sys.stderr)
        sys.exit(1)
    print('页面 GET 鉴权：绑定行 %d / 唯一菜单 path %d / 实际鉴权三元组 %d，全部同源。' % (len(menu),len({x[0] for x in menu}),len(code)))
    print('仅声明无路由对象 2 条；存量页面路由对象 1 条。路由来自已审计快照，数据库连接强制只读。')
except (OSError, ValueError) as error:
    print('FAIL: '+str(error), file=sys.stderr)
    sys.exit(1)
PY
