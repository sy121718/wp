#!/usr/bin/env bash
# check-i18n-keys-seeded.sh — 「key 被用但没 seed」门禁（与 check-i18n-coverage.sh 互补）。
#
# 为什么需要第二条：check-i18n-coverage.sh 只查**一个方向** ——
#   「模板里硬编码了中文，该换成 key」。它查不出另一个方向：
#   「代码/模板里用了 key，但没有任何迁移 seed 过这个词条」。
#
# 后者的表现是 t(key, "中文兜底") 命中失败 → 回落源码里的中文兜底：
#   · 不报错、不 500、日志干净；
#   · 默认语言（中文）下**完全看不出来**；
#   · 只有切到 en-US 才暴露，而那时看到的是中文。
#
# 实测（2026-10）：`admin.page.langs.*` 一个面板里 18 个 key 有 16 个没 seed，
# 而 i18n 门禁是绿的、基线是 0 —— **基线 0 只证明「没人硬编码中文」，
# 没证明「每个用到的 key 都有词条」**。同一个面板的 seed 迁移（494）自己还写着
# 「必须同批 seed，否则英文界面回落中文」，规则只对其中 2 个 key 执行了。
#
# ── 判据口径（2026-10 收紧，本脚本最容易搞错的地方）────────────────────────
# 「已 seed」的定义 = 该 item_key **真的出现在 `INSERT INTO sys_i18n … VALUES (…)` 的元组里**。
# 实现：解析 public/**/*.sql 的 INSERT 元组，取出 (item_key, lang) 对；
# **不是**「key 的文本在 public/ 的任意 .go / .sql 里出现过」。
#
# 为什么必须收紧（两条实测过的**假绿**，方向都是「该红却绿」—— 比假红危险得多）：
#   1. **注释里提到 key 就算已 seed**：接入当天，505 的 SQL 注释里写了一条例外 key
#      （「这条属别的批次，本批不补」），计数立刻从 1 掉到 0 —— 门禁把那一条真缺失掩盖掉了。
#      结论不是「别在注释里写 key」（注释是正常沟通方式），而是判据不许认注释。
#   2. **注册文件的 ConditionSQL 判据列表**里列出 key 也算已 seed —— 那只是门槛 SQL 的 IN 列表，
#      与词条有没有写入无关：判据文件写了 20 个 key、SQL 少写词条，门禁照样绿。
#
# ── 扫描范围（窄是刻意的，宽会立刻变成红着的门禁）───────────────────────────
# 只扫 `admin.*` 与 `workbench.*`：这两个命名空间的词条**只能**来自迁移 seed，
# 没有第二个来源，所以「未 seed」必然等于「回落中文」。
#
# 刻意**不扫** `site.*`：那是访客面组件文案（`site.component.{type}.{prop}`，
# docs/06-D §10.3），词条按**工程**存在内容 i18n 里、由译者在后台逐条填，
# 全新安装回落中文是设计而非缺陷。把它算进来 = 一条永远红着的门禁。
# 同理不扫 `page.*` / `product.*` 之类的**内容字段标识**（不是 UI 词条）。
#
# ── 扫描对象 ──────────────────────────────────────────────────────────────
# internal/ 下的 .go/.html/.jet/.js 字符串字面量，形如 `a.b.c`（≥2 个点、各段小写）。
# **排除 `*_test.go` 与任何 test 目录**：测试会故意引用捏造的 key 当负例，
# 把它们算进来会让门禁永远红着，而红着的门禁等于没有门禁。
#
# ── 中英成对（WARN 段，不参与退出码）──────────────────────────────────────
# 解析出的元组同时给出每个 key 的语言集合：若某个**被引用**的 key 缺 zh-CN 或 en-US 之一，
# 会打印在 WARN 段并写入 I18N_KEYS_UNPAIRED（默认 /tmp/i18n-keys-unpaired.txt）。
# 不并入主计数：主计数的语义是「被引用但完全没 seed」（必然回落中文兜底）；
# 「只 seed 了一种语言」是另一类债务，混进来会让基线的含义漂移。
#
# ── 为什么不做「前缀常量拼接」的兜底推测 ────────────────────────────────────
# 曾试过「全文命中失败后，再看末两段是否出现」这种宽松兜底，结论是**弊大于利**：
# 短段如 `status.draft` / `col.lang` 与其它 key 的文本撞车，把 16 条真缺失掩盖成 8 条（实测）。
# 而它想防的假阳性其实不会发生：key 由 `Prefix + "id"` 拼出时，完整 key 从不以字面量
# 出现在 internal/，**根本不会成为候选** → 不报，也就不会误报。两个方向都安全，故不保留。
#
# ── 已知盲区（明写在这里，别当成"全覆盖"）────────────────────────────────
#   1. 运行时完全动态拼装、且任何一段都不以字面量出现的 key（扫不到）；
#   2. seed 若不经过 .sql 迁移（例如 Go 代码里执行 INSERT）→ 会被误报为缺失。
#      当前仓库无此形态（`INSERT INTO sys_i18n` 全在 public/migrations/*.sql）；
#      将来新增这种 seed 方式时，必须同步扩展本脚本的解析来源；
#   3. seed 侧用 SQL 拼接（`EXECUTE format(...)` / 变量拼字符串）而非字面量元组 → 误报为缺失；
#   4. 不校验译文本身的质量（空串 / 与兜底不一致等）；
#   5. 不查「seed 了但没人用」的孤儿词条（另一个方向，且无害）；
#   6. 只认 INSERT 元组：删词条（DELETE）与改值（UPDATE）不属于本门禁范围。
#
# ── 基线口径变更记录（基线只降不升）────────────────────────────────────────
#   · 旧口径（key 文本出现在 public/ 任意 .go/.sql 即算已 seed）：21 →（补 505 的 20 条词条，
#     并清掉 505 注释里那条例外 key 的字面量）→ 1；
#   · 新口径（INSERT INTO sys_i18n 的 VALUES 元组）：**仍为 1** —— 收紧后计数没变，
#     说明除那处已修的注释外，全仓没有第二处「文本命中了但不是真 seed」。
#     这次改动是**判据防伪**，不是债务数字的变化：它挡的是「将来有人把 key 写进注释 /
#     判据列表，而词条并没写」这类假绿。剩余那 1 条是 customers 批量操作结果词条，
#     属 FIX-24 的机制范围，本批刻意不补。
#
# ── 改本脚本后必跑的自检（判据自己坏了会**静默假绿**，比业务缺陷更难发现）──────────
# 造一个临时 root，三个文件就能覆盖全部分支：
#   mkdir -p /tmp/probe-root/internal /tmp/probe-root/public/migrations
#   echo 'const a = "admin.probe.commentFake";' > /tmp/probe-root/internal/probe.js
#   printf -- '-- 注释里提到 admin.probe.commentFake，但没有 INSERT\n' > /tmp/probe-root/public/migrations/x.sql
#   # 再往 x.sql 追加一条真 INSERT（admin.probe.realKey 的 zh-CN / en-US 两行）
#   GOWP_ROOT=/tmp/probe-root I18N_KEYS_BASELINE=/tmp/probe-base.txt bash scripts/check-i18n-keys-seeded.sh
# 期望：**计数 1**（只有 commentFake 报缺；realKey 有真 INSERT，不算缺）。
# 计数 0 = 正则/解析退化了（key 扫不到或元组解析失效），门禁正在假绿 —— 别提交。
# 实测教训：python raw string 里写成双反斜杠（r'\\s'）会被解释成「反斜杠 + 任意字符」，
# INSERT 正则与 KEY 正则双双静默失配，计数恒 0；本条自检就是用来抓这种退化的。
#
# 计数口径：**去重后的 key 个数**。基线只降不升（与 check-i18n-coverage.sh 同规矩）。
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
import os, re, sys

internal, public, out_path, unpaired_path = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4]

# key 形状：小写点分，≥2 个点。≥2 是为了避开 `node.js` / `vimeo.com` / 版本号。
KEY = re.compile(r'"([a-z][a-zA-Z0-9_]*(?:\.[a-zA-Z0-9_]+){2,})"')
# 只扫「唯一来源是迁移 seed」的命名空间。理由见文件头注释。
# ai. 自 513 起纳入：AI 模块的响应文案 key（ai.msg.* / ai.err.*）同样只能靠迁移 seed，
# 不纳入就会重演「切到 en-US 页面露出裸 key」的缺口。
SCOPE = ('admin.', 'workbench.', 'ai.')
# 中英成对的两侧（AGENTS.md 的要求）。
PAIR = {'zh-CN', 'en-US'}

def walk(root, exts):
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames[:] = [d for d in dirnames if d not in ('.git', 'node_modules', 'vendor')]
        for fn in filenames:
            if fn.endswith(exts):
                yield os.path.join(dirpath, fn)

# 1) internal/ 里被引用的 key 候选（排除测试：测试会故意用捏造的 key 当负例）
used = {}
for path in walk(internal, ('.go', '.html', '.jet', '.js')):
    if path.endswith('_test.go') or os.sep + 'test' + os.sep in path:
        continue
    try:
        src = open(path, encoding='utf-8', errors='replace').read()
    except OSError:
        continue
    for m in KEY.finditer(src):
        k = m.group(1)
        if not k.startswith(SCOPE):
            continue
        used.setdefault(k, os.path.relpath(path, os.path.dirname(internal)))

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

INSERT = re.compile(r'INSERT\s+INTO\s+sys_i18n\s*\([^)]*\)\s*VALUES', re.I)

def parse_tuples(sql):
    """取每条 INSERT INTO sys_i18n 的顶层元组 → 字段列表的列表。"""
    tuples = []
    for m in INSERT.finditer(sql):
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
                if depth == 0 and in_tuple:
                    fields.append(cur.strip())
                    tuples.append(fields)
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
    for f in parse_tuples(sql):
        if len(f) < 2:
            continue
        seeded.setdefault(unquote(f[0]), set()).add(unquote(f[1]))

missing = sorted(k for k in used if k not in seeded)
unpaired = sorted(k for k in used if k in seeded and not PAIR <= seeded[k])

with open(out_path, 'w', encoding='utf-8') as f:
    f.write('\n'.join('%s\t(%s)' % (k, used[k]) for k in missing) + ('\n' if missing else ''))
with open(unpaired_path, 'w', encoding='utf-8') as f:
    f.write('\n'.join('%s\t(已有语言: %s)' % (k, ','.join(sorted(seeded[k]))) for k in unpaired) + ('\n' if unpaired else ''))
print(len(missing))
print(len(unpaired))
PYEOF
)

CURRENT=$(printf '%s\n' "$COUNTS" | sed -n '1p')
UNPAIRED=$(printf '%s\n' "$COUNTS" | sed -n '2p')

echo "admin/workbench 被引用但未 seed 的 i18n key 数：$CURRENT"
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
  echo "  修法：同批补一条迁移 seed（中英成对，ON CONFLICT DO NOTHING）。" >&2
  echo "  ConditionSQL 取**本批自己的代表 key** 作门槛 —— 不要用全库计数，" >&2
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
