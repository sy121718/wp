#!/usr/bin/env bash
# check-service-db-boundary.sh — service 层的数据访问边界（审计 CQ-025）。
#
# 为什么需要一条脚本而不是靠评审记：AGENTS.md 给 admin 开了「service 层可经
# DB(ctx) 直查」的明文豁免，但豁免的边界只写在文档里 —— 新模块照抄 admin 的写法
# 不会有任何东西拦下来，而一旦破窗，表隔离约定就靠自觉了。
#
# 判据（命中即失败）：
#   ①  非 admin 模块的 service 出现 DB(ctx) / RevisionDB(ctx)；
#   ①b 非 admin 模块的 service 在**事务回调拿到的句柄**上直接拼查询
#      （tx.WithContext(ctx).Model(...) / tx.Model(...) / session.Model(...)）。
#      判据 ① 只认 DB(ctx) / RevisionDB(ctx) 这两个具名门面，对事务句柄完全失明：
#      Transaction(ctx, func(tx *gorm.DB) error { return tx.WithContext(ctx).Model(&X{}).Where(...).Updates(...) })
#      一句都不命中，但它绕过 model 具名方法的程度与 ① 完全相同（实测案例：
#      mail 的 SetDefaultAccount 在事务里直查 tx.WithContext(ctx).Model(&MailAccountEntity{})）。
#      **这一条是启发式**：句柄名只认 tx/txn/trx/session/dbtx/dbx/db，入口只认 .Model(，
#      所以 tx.Where(...) / tx.Create(...) / tx.Updates(...) 仍是同类漏网 —— 脚本绿 ≠ 边界干净。
#   ②  任何模块的 service 直接 import 其它模块的 model / service 包 ——
#      service 只能依赖对方 contract 与不可变 dto，绕过它等于绕过对方的仓储方法。
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
DIRECT=$(grep -rn --include='*.go' -E '\.(DB|RevisionDB)\(ctx\)' "$TARGET" \
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

# 句柄名白名单 + 「句柄 .Model(」（.WithContext(...) 视为透明的一层）。
# 只认 .Model( 这一个入口：它是「开始拼一条查询」的标志，而 tx.Create/tx.Where/tx.Exec
# 同样越界却不在扫描范围内（见脚本头注释）。
# t 是 inventory_change.go 的事务回调参数名（func(t *gorm.DB) error）；把单字母 t
# 加进白名单是安全的：testing.T 没有 Model 方法，测试代码又被扫描排除（_test.go 不进
# 目标目录），误报概率极低 —— 它排在 tx/trx 之后，不会截断这两个名字。
handle = r"(tx|txn|trx|session|dbtx|dbx|db|t)"
pat = re.compile(r"(?<![A-Za-z0-9_.])" + handle + r"\s*\.\s*(?:WithContext\s*\([^()]*\)\s*\.\s*)?Model\s*\(")
func_pat = re.compile(r"^func\s+(?:\([^)]*\)\s*)?([A-Za-z_][A-Za-z0-9_]*)")

def func_name(lines, idx):
    for i in range(idx, -1, -1):
        m = func_pat.match(lines[i])
        if m:
            return m.group(1)
    return "<unknown>"

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
    for i, line in enumerate(lines):
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
    """internal/module/<owner>/.../service/x.go → <owner>（含两级模块如 product/inventory）。"""
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
        # 本模块（含子模块）之间的引用不算跨模块：product 与 product/inventory 是一棵树。
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
  echo "  注意：只有模块间才算 —— product 与 product/inventory 属同一棵树，直接引用不算违规。" >&2
  failed=1
fi

if [ "$failed" -ne 0 ]; then
  exit 1
fi

echo "✓ service 层数据访问边界无违规（判据 ①/①b 无直查、判据 ② 无跨模块 model/service 引用）"
