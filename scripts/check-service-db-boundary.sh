#!/usr/bin/env bash
# check-service-db-boundary.sh — service 层的数据访问边界（审计 CQ-025）。
#
# 为什么需要一条脚本而不是靠评审记：AGENTS.md 给 admin 开了「service 层可经
# DB(ctx) 直查」的明文豁免，但豁免的边界只写在文档里 —— 新模块照抄 admin 的写法
# 不会有任何东西拦下来，而一旦破窗，表隔离约定就靠自觉了。
#
# 判据（命中即失败）：
#   ①  非 admin 模块的 service 出现**任意裸句柄门面** `.<Name>DB(ctx)` —— 含
#      DB(ctx) / RevisionDB(ctx)，也含 model 自定的 RouteDB(ctx) / ReceiptDB(ctx) /
#      PublicationDB(ctx) / VariantDB(ctx) 这类。
#   ①b 非 admin 模块的 service 在**事务/裸句柄**上直接拼查询
#      （tx.WithContext(ctx).Model(...) / tx.Create(...) / tx.Where(...) / tx.Exec(...) …，
#      入口是下方 ENTRY 列出的裸查询全族）。
#      句柄名 = 固定白名单（tx/txn/trx/session/dbtx/dbx/db/t）**加上动态收集**：
#      同一函数里形如 `q := s.model.ReceiptDB(ctx)` / `q = x.RouteDB(ctx)` 的局部变量名
#      也纳入扫描（q.Where(...) / q.Create(...) 同样命中）。
#      **为什么 ① 要扩到任意 …DB(ctx)**（2026-09 收口）：判据 ① 原只认 DB(ctx) /
#      RevisionDB(ctx) 两个具名门面，于是 model 只要把裸句柄改个名字暴露
#      （publication 的 RouteDB(ctx) / ReceiptDB(ctx)），service 就能照旧在它上面拼
#      .Where()/.Create() —— 绕过 model 具名方法的程度与原来完全相同，而 ① 不认、
#      ①b 的句柄白名单里也没有 receiptdb / routedb 这种名字。这是「换个名字即豁免」的洞。
#   ① 对**事务句柄**完全失明，那是 ①b 的职责：
#      Transaction(ctx, func(tx *gorm.DB) error { return tx.WithContext(ctx).Model(&X{}).Where(...).Updates(...) })
#      一句都不命中，但它绕过 model 具名方法的程度与 ① 完全相同（实测案例：
#      mail 的 SetDefaultAccount 在事务里直查 tx.WithContext(ctx).Model(&MailAccountEntity{})）。
#      ①b 的入口曾是「只认 .Model(」——tx.Where(...) / tx.Create(...) / tx.Exec(...) 是同一类
#      越界却一句都不命中（实测漏网：artifact 的 Record 事务里 tx.Create(产物行) 与
#      tx.Clauses(...).Create(内容对象)；publication 的 activateRouteIn / reservePathIn /
#      deleteRoutesByPageIn / deactivateIn 四个自由函数在事务句柄上拼 SQL）。2026-09 收口到全族。
#   ②  任何模块的 service 直接 import 其它模块的 model / service 包 ——
#      service 只能依赖对方 contract 与不可变 dto，绕过它等于绕过对方的仓储方法。
#
# **① / ①b 仍然是启发式**（脚本绿 ≠ 边界干净）。当前已知的边界，改这条判据的人请一并维护：
#   · ①b 的动态收集是**静态近似**，只认同一函数内、同一行上的 `<var> :=| = ……DB(ctx)`：
#     跨函数传递（本函数取句柄、调另一个函数在它上面拼查询）、同一变量先后取两次句柄、
#     取到的句柄存进结构体字段之后再取用 —— 这三类都扫不到；
#   · 句柄若不以 `…DB(ctx)` 形态暴露（例如 model 直接暴露 `m.db` 或某个
#     `Session(...)` 包装），① 也认不出 —— 判据锚定的是**命名形状**，不是能力；
#   · 动态收集会把「持有终端调用结果」的变量也收进来（`err := …ReceiptDB(ctx).Create(x)`
#     里的 err）—— 属过度收紧的方向，只会让门禁更严，不会放过真违规；
#   · 先取句柄存到局部再用（q := tx.Model(&X{}) 之后 q.Where(...)）只按句柄名锚定第一处；
#   · .WithContext(...) 只当透明的一层（参数支持一层括号嵌套），
#     tx.Session(&gorm.Session{}).Where( 这类链式包装不认；
#   · 只匹配同一行内的「句柄名 . 方法(」：跨行折行（gofmt 不会这样输出，手写可能）漏。
#
# ①b 的豁免清单在 scripts/service-db-boundary-allow.txt：**每条必须写明理由**，
# 条目不再命中（改成具名方法 / 函数改名 / 被删）脚本会失败 —— 只增不减的清单等于没有门禁。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TARGET="$ROOT/internal/module"

if [ ! -d "$TARGET" ]; then
  echo "找不到目标目录：$TARGET" >&2
  exit 2
fi

failed=0

# 判据 ①：service 目录下的裸 gorm 句柄（admin 豁免）。
#
# 形状是「任意名字 + DB(ctx)」而不是只认 DB / RevisionDB：model 自定的裸句柄
# （RouteDB(ctx) / ReceiptDB(ctx) …）与此前那两个具名门面是同一类越界，只认两个名字
# 等于给「换个名字」留了豁免（见文件头注释）。
DIRECT=$(grep -rn --include='*.go' -E '\.[A-Za-z0-9_]*DB\(ctx\)' "$TARGET" \
  | grep '/service/' \
  | grep -v '/admin/service/' \
  | grep -v '_test\.go:' \
  | grep -vE '^[^:]+:[0-9]+:[[:space:]]*//' || true)

if [ -n "$DIRECT" ]; then
  echo "✗ 非 admin 模块的 service 直接使用了裸 gorm 句柄：" >&2
  echo "$DIRECT" >&2
  echo "" >&2
  echo "  持久化的唯一入口是 model 的具名方法；缺查询就给 model 加方法，" >&2
  echo "  不要在 service 里拼 .Where()/.Create()（AGENTS.md「model 层定位」）。" >&2
  echo "  仅 admin 的简单 CRUD 有豁免。" >&2
  failed=1
fi

# 判据 ①b：service 在事务/裸 gorm 句柄上直接拼查询。
ALLOW_FILE="$ROOT/scripts/service-db-boundary-allow.txt"
if [ ! -f "$ALLOW_FILE" ]; then
  echo "找不到豁免清单：$ALLOW_FILE" >&2
  exit 2
fi

if ! BOUNDARY=$(python3 - "$TARGET" "$ROOT" "$ALLOW_FILE" <<'PY'
import glob, os, re, sys

target, root, allow_file = sys.argv[1], sys.argv[2], sys.argv[3]

# 句柄名白名单 + 「句柄 .<裸查询入口>(」（.WithContext(...) 视为透明的一层）。
#
# 入口是**全族**：.Model( 只是「开始拼一条查询」的其中一种标志，tx.Where(...) /
# tx.Create(...) / tx.Exec(...) 绕过 model 具名方法的程度与它完全相同（见脚本头注释）。
# ENTRY 是正则 alternation，顺序无关；gorm 新增链式入口时往这里补一条即可。
# t 是 inventory_change.go 的事务回调参数名（func(t *gorm.DB) error）；把单字母 t
# 加进白名单是安全的：testing.T 没有下面这些方法（Run / Error 等都不在族里），
# 测试代码又被扫描排除（_test.go 不进目标目录），误报概率极低 ——
# 它排在 tx/trx 之后，不会截断这两个名字。
FIXED_HANDLES = "tx|txn|trx|session|dbtx|dbx|db|t"
chain = r"(?:WithContext\s*\([^()]*(?:\([^()]*\)[^()]*)*\)\s*\.\s*)?"
ENTRY = ("Model|Where|Create|Delete|Update|Updates|Exec|Raw|First|Find|Count|Table|"
         "Clauses|Save|Scan|Pluck|Take|Last|Joins|Select|Order|Limit|Offset|Group|"
         "Having|Distinct|Not|Or")
func_pat = re.compile(r"^func\s+(?:\([^)]*\)\s*)?([A-Za-z_][A-Za-z0-9_]*)")

# 动态句柄（2026-09 收口）：同一函数里 `<var> := …DB(ctx)` / `<var> = …DB(ctx)` 取到的
# 局部变量也是「句柄」，它上面的 .Where(/.Create( 与 tx 上的是同一类越界 ——
# 此前 model 自定的裸句柄（ReceiptDB(ctx) / RouteDB(ctx) / PublicationDB(ctx) …）
# 名字不在固定白名单里，service 拿它拼查询时 ①b 一句都不命中。
#
# 只认「行首就是赋值目标」的形状：`if err = …DB(ctx)` / `return …DB(ctx)` 这类不算，
# 免得把「持有终端调用结果」的变量（err）当句柄收进来。
handle_assign = re.compile(
    r"^\s*([A-Za-z_][A-Za-z0-9_]*)\s*(?::=|=)(?!=)\s*[^=]*\.[A-Za-z0-9_]*DB\s*\(\s*ctx\s*\)")

def handle_pattern(local):
    """固定白名单 + 本函数收集到的动态句柄名，合成一条「句柄 .<入口>(」正则。"""
    alt = "|".join([FIXED_HANDLES] + sorted(local))
    return re.compile(r"(?<![A-Za-z0-9_.])(?:" + alt + r")\s*\.\s*" + chain +
                      r"(?:" + ENTRY + r")\s*\(")

def func_name(lines, idx):
    for i in range(idx, -1, -1):
        m = func_pat.match(lines[i])
        if m:
            return m.group(1)
    return "<unknown>"

def func_blocks(lines):
    """按顶层 `^func ` 切成 [start, end) 段。

    句柄白名单是**函数级**的：`q := s.model.ReceiptDB(ctx)` 只让它所在函数里的
    q.xxx( 被算作越界，不会波及其它函数（同名变量在别的函数里可能是别的东西）。
    """
    starts = [i for i, ln in enumerate(lines) if func_pat.match(ln)]
    bounds = starts + [len(lines)]
    return [(bounds[k], bounds[k + 1]) for k in range(len(starts))]

hits = []
for path in sorted(glob.glob(os.path.join(target, "**", "service", "*.go"), recursive=True)):
    if path.endswith("_test.go"):
        continue
    # admin 有明文豁免（仅限本模块表、简单 CRUD），与判据 ① 保持一致。
    if os.sep + "admin" + os.sep + "service" + os.sep in path:
        continue
    try:
        lines = open(path, encoding="utf-8").read().splitlines()
    except OSError:
        continue
    rel = os.path.relpath(path, root)
    for start, end in func_blocks(lines):
        local = set()
        for line in lines[start:end]:
            if line.lstrip().startswith("//"):
                continue
            m = handle_assign.match(line)
            if m:
                local.add(m.group(1))
        pat = handle_pattern(local)
        for i in range(start, end):
            line = lines[i]
            if line.lstrip().startswith("//"):
                continue
            if not pat.search(line):
                continue
            hits.append((rel + "#" + func_name(lines, i), "%s:%d: %s" % (rel, i + 1, line.strip())))

allow = {}
for ln, raw in enumerate(open(allow_file, encoding="utf-8"), 1):
    line = raw.rstrip("\n")
    if not line.strip() or line.lstrip().startswith("#"):
        continue
    if "\t" not in line:
        print("✗ 豁免清单第 %d 行格式不对（键与理由之间必须是一个 TAB）：%s" % (ln, line))
        sys.exit(1)
    key, reason = line.split("\t", 1)
    if not key.strip() or not reason.strip():
        print("✗ 豁免清单第 %d 行的键或理由为空：%s" % (ln, line))
        sys.exit(1)
    allow[key.strip()] = reason.strip()

seen = set()
problems = []
for key, loc in hits:
    if key in allow:
        seen.add(key)
        continue
    problems.append(loc)
stale = sorted(k for k in allow if k not in seen)

if problems:
    print("✗ 非 admin 模块的 service 在事务/裸句柄上直接拼 gorm 查询：")
    for p in sorted(problems):
        print("  " + p)
    print("")
    print("  事务回调里的句柄不是 model 的具名方法：给 model 加方法（要落在同一事务里就加 …Tx 变体，")
    print("  句柄由 service 透传），不要在 service 里拼 .Model()/.Where()（AGENTS.md「model 层定位」）。")
    print("  仅 admin 的简单 CRUD 有豁免；确属暂缓的，写进 scripts/service-db-boundary-allow.txt 并写明理由。")
    print("")
if stale:
    print("✗ 豁免清单里这些条目已经不再命中（已改成具名方法 / 被改名 / 被删）：")
    for s in stale:
        print("  " + s)
    print("")
    print("  请从 scripts/service-db-boundary-allow.txt 删掉 —— 清单只增不减会掩盖回归。")
    print("")

sys.exit(1 if (problems or stale) else 0)
PY
); then
  echo "$BOUNDARY" >&2
  failed=1
fi

# 判据 ②：service 跨模块 import model / service。
CROSS=$(python3 - "$TARGET" <<'PY'
import glob, os, re, sys

target = sys.argv[1]
hits = []
pattern = re.compile(r'"go_wp/internal/module/([^"]+)/(model|service)"')

def owner_of(path):
    """internal/module/<owner>/.../service/x.go → <owner>。"""
    parts = path.split(os.sep)
    try:
        i = parts.index("module")
    except ValueError:
        return ""
    rest = parts[i + 1:]
    j = rest.index("service")
    return "/".join(rest[:j])

for path in glob.glob(os.path.join(target, "**", "service", "*.go"), recursive=True):
    if path.endswith("_test.go"):
        continue
    owner = owner_of(path)
    try:
        src = open(path, encoding="utf-8").read()
    except OSError:
        continue
    for m in pattern.finditer(src):
        other = m.group(1)
        # 仅相同模块内的 service/model 引用放行；inventory 是独立模块。
        if other == owner or other.startswith(owner + "/") or owner.startswith(other + "/"):
            continue
        hits.append("%s -> %s" % (os.path.relpath(path, os.path.dirname(target)), m.group(0)))

for h in sorted(set(hits)):
    print(h)
PY
)

if [ -n "$CROSS" ]; then
  echo "✗ service 直接 import 了其它模块的 model / service：" >&2
  echo "$CROSS" >&2
  echo "" >&2
  echo "  跨模块只能用对方 contract 与不可变 dto（internal/module/CLAUDE.md「表隔离约定」）。" >&2
  echo "  对方的能力不够用就给对方 contract 加方法，不要伸进它的仓储层。" >&2
  failed=1
fi

if [ "$failed" -ne 0 ]; then
  exit 1
fi

echo "✓ service 层数据访问边界无违规（判据 ①/①b 无直查、判据 ② 无跨模块 model/service 引用）"
