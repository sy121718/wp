#!/usr/bin/env bash
# 按真实取词调用识别短 key，同时保留未迁命名空间的既有检查。
# 只认 SQL 的实际 INSERT VALUES 或 INSERT SELECT FROM (VALUES) 词条行，
# 不把注释、ConditionSQL 或迁移旧新映射当作 seed。
# 静态扫描不代替 PG 终态验证；UPDATE/DELETE 和额外语言迁移由真实库测试验证。
# 短 key 缺任一基础语言、动态拼接均失败，不消耗旧基线；
# 无法静态确定的事实 key 如实输出待核对清单，不声称全覆盖。
set -euo pipefail

ROOT="${GOWP_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
INTERNAL="$ROOT/internal"
PUBLIC="$ROOT/public"
BASELINE_FILE="${I18N_KEYS_BASELINE:-$ROOT/scripts/i18n-keys-seeded-baseline.txt}"
LIST_FILE="${I18N_KEYS_LIST:-/tmp/i18n-keys-missing.txt}"
UNPAIRED_FILE="${I18N_KEYS_UNPAIRED:-/tmp/i18n-keys-unpaired.txt}"

for d in "$INTERNAL" "$PUBLIC"; do
  if [ ! -d "$d" ]; then
    echo "找不到目录：$d（可用 GOWP_ROOT 指向仓库根）" >&2
    exit 2
  fi
done

COUNTS=$(python3 - "$INTERNAL" "$PUBLIC" "$LIST_FILE" "$UNPAIRED_FILE" <<'PYEOF'
import os, re, sys, json

internal, public, out_path, unpaired_path = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4]

# 点分形状只用于尚未迁移的既有检查；短 key 由真实取词位置识别。
KEY = re.compile(r'"([a-z][a-zA-Z0-9_]*(?:\.[a-zA-Z0-9_]+){2,})"')
SCOPE = ('admin.', 'workbench.', 'ai.')
FLAT = re.compile(r'^[A-Za-z][A-Za-z0-9]{0,14}$')
CALL = re.compile(r"(?<![\w])(?:t|tr|tf|tn)\s*\(|\[\s*[\"'](?:t|tr|tf|tn)[\"']\s*\]\s*\(")
LITERAL = re.compile(r"\s*([\"'`])((?:\\.|(?!\1).)*)\1\s*(?=,|\))", re.S)
PAIR = {'zh-CN', 'en-US'}

def source_without_comments(src):
    # 保留字符串中的 URL、SQL 与中文，避免把字面量中的注释符误删。
    token = re.compile(r'''("(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|`[^`]*`)|\{\*.*?\*\}|<!--.*?-->|/\*.*?\*/|//[^\n]*''', re.S)
    return token.sub(lambda m: m.group(1) or '\n' * m.group(0).count('\n'), src)

def walk(root, exts):
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames[:] = [d for d in dirnames if d not in ('.git', 'node_modules', 'vendor')]
        for fn in filenames:
            if fn.endswith(exts):
                yield os.path.join(dirpath, fn)

def dynamic_key_argument(src, start):
    """取词调用的第一个实参里是否含字符串拼接。

    只看到第一个实参结束（同层的 , 或 )）为止，按括号配对跳过嵌套调用。
    旧做法取「到下一个换行」再找 +，会把取词结果参与外层拼接（如
    `tr(k) + x`、多行函数调用的后续参数）误判成「key 是拼出来的」。
    """
    depth = 0
    quote = ''
    for i in range(start, len(src)):
        ch = src[i]
        if quote:
            if ch == '\\':
                continue
            if ch == quote:
                quote = ''
            continue
        if ch in '"\'`':
            quote = ch
        elif ch in '([{':
            depth += 1
        elif ch in ')]}':
            if depth == 0:
                return False
            depth -= 1
        elif ch == ',' and depth == 0:
            return False
        elif ch == '+' and depth == 0:
            return True
    return False

# 0) 能力标识（data-write-handler / data-picker-fetch / registerResultHandler）
#    与词条同为点分小写形状，但它们不是界面文案：
#      · data-write-handler="ai.provider.saved"  —— 写基座的结果处理器名字；
#      · registerResultHandler('ai.provider.saved', fn) —— 同一个名字的注册点。
#    这些标识没有、也不该有 sys_i18n 词条，扫进来会让门禁逼人给代码标识符编译文。
#    做法：先在源码里把这些**标识出现的位置**抹成空串，再扫剩下的候选 ——
#    只抹标识本身，不抹整行，避免连带丢掉同一行里的真词条。
#
# 只排除三类**确实指向注册名字**的属性（每个都在 JS 里有对应的注册入口）：
#   data-write-handler → write-base.js 的 registerResultHandler
#   data-write-builder → write-base.js 的 registerPayloadBuilder
#   data-picker-fetch  → 直接就是请求地址，由 ai-model-picker.js 读走
# 其余 data-*（write-extra / filter-input / row-add / row-remove …）**不排除**：
# 它们不是注册名字，值与 key 形状撞车时就该照常报红。
CAPABILITY_ATTR = re.compile(r'data-(?:write-handler|write-builder|picker-fetch)="[^"]*"')
# 注册点同时可能写成单引号或双引号：KEY 正则只认双引号，若这里只认单引号，
# registerResultHandler("ai.x.y", fn) 会被漏掉、进而误报成「未 seed 的词条」。
CAPABILITY_REG = re.compile(r'registerResultHandler\(\s*["\']([^"\']*)["\']')
CAPABILITY_BUILDER = re.compile(r'registerPayloadBuilder\(\s*["\']([^"\']*)["\']')
CAPABILITY_LIST = re.compile(r"\[\s*((?:'[^']*'\s*,?\s*)+)\]\.forEach\(\s*function\s*\(\s*name\s*\)")

def strip_capabilities(src):
    src = CAPABILITY_ATTR.sub('', src)
    src = CAPABILITY_REG.sub('', src)
    src = CAPABILITY_BUILDER.sub('', src)
    src = CAPABILITY_LIST.sub('', src)
    return src

# 短 key 不从任意字符串猜测；只收真实取词调用及本批商品错误/消息声明。
used, short_used, unresolved, invalid = {}, set(), [], []
for path in walk(internal, ('.go', '.html', '.jet', '.js')):
    if path.endswith('_test.go') or os.sep + 'test' + os.sep in path:
        continue
    # testdata/ 下是浏览器/组件测试的夹具页面（fixture.title 之类），不是产品界面文案：
    # 它们不该有、也不该被迫有 sys_i18n 词条。
    if os.sep + 'testdata' + os.sep in path:
        continue
    src = source_without_comments(open(path, encoding='utf-8').read())
    relative = os.path.relpath(path, os.path.dirname(internal))
    for m in KEY.finditer(strip_capabilities(src)):
        if m.group(1).startswith(SCOPE):
            used.setdefault(m.group(1), relative)
    calls = CALL
    if path.endswith(('.html', '.jet')):
        aliases = {'t', 'tr', 'tf', 'tn'}
        assignments = re.findall(r'''\{\{\s*([A-Za-z_]\w*)\s*:?=\s*(?:\.\[\s*["'](t|tr|tf|tn)["']\s*\]|([A-Za-z_]\w*))\s*\}\}''', src)
        # 每轮至少增加一个别名才继续，链式赋值也能覆盖且必定终止。
        while True:
            found = {name for name, direct, previous in assignments if direct or previous in aliases}
            if found <= aliases:
                break
            aliases.update(found)
        calls = re.compile(CALL.pattern + r'|(?<![\w])(?:' + '|'.join(re.escape(name) for name in sorted(aliases)) + r')\s*\(')
    for call in calls.finditer(src):
        literal = LITERAL.match(src, call.end())
        if not literal:
            expression = src[call.end():].split('\n', 1)[0][:160]
            unresolved.append(relative + ': ' + expression)
            if dynamic_key_argument(src, call.end()):
                # site.* 前缀既是既有词条命名空间，也已被本脚本整体豁免（见下方
                # key.startswith('site.') 分支）；对它们报「动态拼接」会与既有判据自相矛盾。
                if 'site.' not in expression:
                    invalid.append(relative + ': 动态拼接取词 key')
            continue
        key = literal.group(2)
        if key.startswith('site.'):
            continue
        used.setdefault(key, relative)
        if FLAT.fullmatch(key):
            short_used.add(key)
        elif '.' not in key:
            invalid.append(relative + ': 非法扁平 key ' + key)
    # 权限码不是界面词条：AGENTS.md 明确权限码、按钮权限标识不适用短 key 规则，
    # 也不进 sys_i18n。permission_codes.go 里的 ProductTranslationsSave 这类名字
    # 只是和文本常量同前缀，按文本词条扫它会永远缺基础语言。
    is_permission_codes = path.endswith(os.sep + 'permission_codes.go')
    if not is_permission_codes and os.sep + 'module' + os.sep + 'product' + os.sep + 'enums' + os.sep in path:
        for name, key in re.findall(r'^\s*((?:Err|Msg|Detail|Bulk|VariantSkip|BundleMember|ProductTranslations|ProductsAction)\w*)\s*=\s*"([^"\n]+)"', src, re.M):
            used.setdefault(key, relative)
            short_used.add(key)
            if not FLAT.fullmatch(key):
                invalid.append(relative + ': 非法业务词条 ' + name + '=' + key)

# 2) 词条台账：解析 public 下 .sql 里 INSERT INTO sys_i18n … VALUES (...) 的 (item_key, lang) 元组。
#    不认注释、不认判据列表 —— 理由见文件头「判据口径」。
def strip_comments(sql):
    """去掉 -- 行注释与块注释，但保留字符串字面量里的内容。"""
    out, i, n, in_str = [], 0, len(sql), False
    while i < n:
        ch = sql[i]
        if in_str:
            if ch == "'":
                if i + 1 < n and sql[i + 1] == "'":
                    out.append("''")
                    i += 2
                    continue
                in_str = False
            out.append(ch)
            i += 1
            continue
        if ch == "'":
            in_str = True
            out.append(ch)
            i += 1
            continue
        if ch == '-' and i + 1 < n and sql[i + 1] == '-':
            j = sql.find('\n', i)
            if j == -1:
                break
            out.append('\n')
            i = j + 1
            continue
        if ch == '/' and i + 1 < n and sql[i + 1] == '*':
            j = sql.find('*/', i + 2)
            i = n if j == -1 else j + 2
            continue
        out.append(ch)
        i += 1
    return ''.join(out)

# 结构快照里的系统数据是位置参数（INSERT INTO public.sys_i18n VALUES (…)，列序见建表），
# 迁移时代的写成显式列名。两种都要认：有列名时按列名定位 item_key/lang，没有时按建表列序取 1/2。
INSERT = re.compile(r'INSERT\s+INTO\s+(?:public\.)?sys_i18n\s*(?:\(([^)]*)\))?\s*(?:VALUES|SELECT\s+[^;]*?\s+FROM\s*\(\s*VALUES)', re.I)

def key_lang(fields, columns):
    if columns:
        names = [c.strip() for c in columns.split(',')]
        if 'item_key' not in names or 'lang' not in names:
            return None
        return fields[names.index('item_key')], fields[names.index('lang')]
    # 建表列序：id, item_key, lang, item_value, …
    if len(fields) < 3:
        return None
    return fields[1], fields[2]

def parse_tuples(sql):
    """取每条 INSERT INTO sys_i18n 的顶层元组 → (列名列表或 None, 字段列表)。"""
    tuples = []
    for m in INSERT.finditer(sql):
        columns = m.group(1)
        i, n, depth, cur, fields, in_str, in_tuple = m.end(), len(sql), 0, '', [], False, False
        while i < n:
            ch = sql[i]
            if in_str:
                cur += ch
                if ch == "'":
                    if i + 1 < n and sql[i + 1] == "'":
                        cur += sql[i + 1]
                        i += 2
                        continue
                    in_str = False
                i += 1
                continue
            if ch == "'":
                in_str = True
                cur += ch
                i += 1
                continue
            if ch == '(':
                depth += 1
                if depth == 1:
                    in_tuple, cur, fields = True, '', []
                    i += 1
                    continue
                cur += ch
                i += 1
                continue
            if ch == ')':
                depth -= 1
                if depth < 0:
                    break
                if depth == 0 and in_tuple:
                    fields.append(cur.strip())
                    tuples.append((columns, fields))
                    in_tuple = False
                    i += 1
                    continue
                cur += ch
                i += 1
                continue
            if depth == 1 and ch == ',':
                fields.append(cur.strip())
                cur = ''
                i += 1
                continue
            if depth == 0 and ch == ';':
                break
            if depth >= 1:
                cur += ch
            i += 1
    return tuples

def unquote(s):
    s = s.strip()
    if len(s) >= 2 and s[0] == "'" and s[-1] == "'":
        return s[1:-1].replace("''", "'")
    return s

seeded = {}
for path in walk(public, ('.sql',)):
    try:
        sql = strip_comments(open(path, encoding='utf-8', errors='replace').read())
    except OSError:
        continue
    for columns, f in parse_tuples(sql):
        pair = key_lang(f, columns)
        if pair is None:
            continue
        seeded.setdefault(unquote(pair[0]), set()).add(unquote(pair[1]))

missing = sorted(k for k in used if k not in seeded)
unpaired = sorted(k for k in used if k in seeded and not PAIR <= seeded[k])

with open(out_path, 'w', encoding='utf-8') as f:
    f.write('\n'.join('%s\t(%s)' % (k, used[k]) for k in missing) + ('\n' if missing else ''))
with open(unpaired_path, 'w', encoding='utf-8') as f:
    f.write('\n'.join('%s\t(已有语言: %s)' % (k, ','.join(sorted(seeded[k]))) for k in unpaired) + ('\n' if unpaired else ''))
short_failures = sorted(k for k in short_used if not PAIR <= seeded.get(k, set()))
with open(out_path + '.unresolved', 'w', encoding='utf-8') as f:
    f.write('\n'.join(sorted(unresolved)) + ('\n' if unresolved else ''))
for entry in invalid:
    print(entry, file=sys.stderr)
for key in short_failures:
    print('短 key 缺基础语言: ' + key + ' (' + used[key] + ')', file=sys.stderr)
print(len(missing))
print(len(unpaired))
print(len(invalid) + len(short_failures))
print(len(unresolved))
PYEOF
)

CURRENT=$(printf '%s\n' "$COUNTS" | sed -n '1p')
UNPAIRED=$(printf '%s\n' "$COUNTS" | sed -n '2p')
STRICT=$(printf '%s\n' "$COUNTS" | sed -n '3p')
UNRESOLVED=$(printf '%s\n' "$COUNTS" | sed -n '4p')
if [ "$UNRESOLVED" -gt 0 ]; then
  echo "另有 $UNRESOLVED 处动态事实取词无法静态确定，待核对清单：$LIST_FILE.unresolved"
fi
if [ "$STRICT" -gt 0 ]; then
  echo "短词条校验失败：$STRICT 处，不能用旧基线放行" >&2
  exit 1
fi

echo "被引用但未 seed 的 i18n key 数：$CURRENT"
if [ "$UNPAIRED" -gt 0 ]; then
  echo "提示：另有 $UNPAIRED 个被引用的 key 只 seed 了一侧语言（中英成对要求，清单见 $UNPAIRED_FILE）"
fi

if [ ! -f "$BASELINE_FILE" ]; then
  echo "$CURRENT" > "$BASELINE_FILE"
  echo "已写入基线：$BASELINE_FILE"
  exit 0
fi

BASE=$(tr -d '[:space:]' < "$BASELINE_FILE")
if [ "$CURRENT" -gt "$BASE" ]; then
  echo "" >&2
  echo "✗ 新增了未 seed 的 i18n key：$BASE → $CURRENT" >&2
  echo "  修法：同批补真实迁移 seed（中英成对，保留自定义译文并检查冲突）。" >&2
  echo "  ConditionSQL 逐条检查本批对象与语言 —— 不要用全库计数，" >&2
  echo "  存量库永远满足、补词条的那条迁移永远不会执行（迁移 494 的写法可抄）。" >&2
  echo "  注意：把 key 写进注释或 ConditionSQL 列表**不会**让本门禁变绿（判据只认 INSERT 元组）。" >&2
  echo "  新增的 key（完整清单见 $LIST_FILE）：" >&2
  tail -20 "$LIST_FILE" >&2
  exit 1
fi
if [ "$CURRENT" -lt "$BASE" ]; then
  echo "✓ 补了 $((BASE - CURRENT)) 条词条，请把基线下调为 $CURRENT："
  echo "    echo $CURRENT > \"$BASELINE_FILE\""
  exit 0
fi
echo "✓ 与基线一致（$BASE）"
