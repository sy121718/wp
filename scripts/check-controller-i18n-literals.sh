#!/usr/bin/env bash
# check-controller-i18n-literals.sh — 控制器不得写 i18n 词条 key 字面量（判据计数，只减不增）。
#
# 为什么需要它：后台是 Go 渲染、词条真源在 `sys_i18n` 表里，key 的**归属**是各模块的 `enums`
# （`internal/module/CLAUDE.md` §enums：展示标签的形状是 `XxxLabel(value) (key, fallback)`）。
# 控制器里直接写 `"admin.pages.impact.stage_plan"` 这类字面量的代价不是「不好看」，而是：
#   · 词条改名/删词条时**没有编译期保护**，字面量在 Go 里静默失配（页面上只剩中文兜底）；
#   · 同一个 key 在多个控制器里各抄一遍，seed 的对账面（`check-i18n-keys-seeded.sh`）与实际
#     使用点之间多出一层手工同步；
#   · 二开的人要加语言，得先在控制器里找到「key 从哪来」——而它本该只有一个答案：enums。
# 用户口径：「控制器只管校验请求、编排业务层、把结果返回给用户」。
#
# 判据：`internal/module/<模块>/inbound/http/*.go`（**非** _test.go，跳过整行注释）里
# i18n key 字面量（形如 `"<名字空间>.<子域>.<...>"`）的出现次数，按模块聚合。
#   · 超过基线 → 违规；低于基线 → 通过，并打印可直接粘贴的基线行（只减不增）；
#   · 基线条目**不再命中即失败**（计数归零的条目必须删行，僵尸清单等于没有门禁）；
#   · 未登记的模块一旦出现字面量即失败（新模块必须显式登记它的存量或直接做干净）。
# 口径与 shell-migration-allow.txt 一致：计数单位是**出现次数**，不是文件数或行数；
# 跳过整行注释（注释里解释 key 是正常的），行尾注释照计（宁可多计一行让人看一眼）。
#
# 名字空间表：来自 `public/migrations/*.sql` 里 item_key 的首段（实测统计）。**新增名字空间时
# 同步这张表** —— 不加会让新名字空间的字面量从判据里漏掉（假绿），加了能让它现形。
#
# 自带负向自检：树干净时把探针条目的基线**临时收紧到实测值**再人为 +1 比一次，判据必须报违规，
# 报不出来说明判据已退化成空转，脚本自己判失败（退出码 2）。正式基线文件不动。
#
# 退出码：0 通过 / 1 违规或僵尸基线 / 2 环境或自检失败。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ALLOW_FILE="$ROOT/scripts/controller-i18n-literals-allow.txt"

if [ ! -f "$ALLOW_FILE" ]; then
  echo "找不到基线文件：$ALLOW_FILE" >&2
  exit 2
fi

python3 - "$ROOT" "$ALLOW_FILE" <<'PYEOF'
import collections, os, re, sys

root, allow_path = sys.argv[1], sys.argv[2]

# i18n 词条名字空间（首段）：来自 public/migrations/*.sql 的 item_key 实测首段统计。
NAMESPACES = (
    "admin", "site", "workbench", "user", "mail", "order", "ai", "shell", "cart",
    "webhook", "membership", "product", "comment", "page", "inventory", "navigation",
    "project", "content", "block", "plugin", "media", "masterdata", "analytics",
    "contenttemplate", "publication", "presentation", "sysconfig", "datarule", "i18n",
)
KEY_LITERAL = re.compile(
    r'"(' + "|".join(NAMESPACES) + r')\.[a-z][a-z0-9_]*\.[a-zA-Z0-9_.]+"'
)

def read(path):
    with open(path, encoding="utf-8", errors="ignore") as fh:
        return fh.read()

def drop_line_comments(src):
    """只剥整行注释，不剥行尾 —— 行尾注释里有字面量时剥了会漏计（宁可多计一行）。"""
    return "\n".join(l for l in src.split("\n") if not l.lstrip().startswith("//"))

def collect(root):
    counts = collections.Counter()
    modules_dir = os.path.join(root, "internal", "module")
    if not os.path.isdir(modules_dir):
        print("找不到 internal/module 目录", file=sys.stderr)
        sys.exit(2)
    for mod in sorted(os.listdir(modules_dir)):
        http = os.path.join(modules_dir, mod, "inbound", "http")
        if not os.path.isdir(http):
            continue
        n = 0
        for name in sorted(os.listdir(http)):
            if not name.endswith(".go") or name.endswith("_test.go"):
                continue
            n += len(KEY_LITERAL.findall(drop_line_comments(read(os.path.join(http, name)))))
        if n:
            counts["lit:%s" % mod] = n
    return counts

def load_allow(path):
    entries, seen = [], set()
    for lineno, raw in enumerate(read(path).split("\n"), 1):
        line = raw.rstrip("\r")
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        parts = line.split("\t")
        if len(parts) != 4:
            print("基线条目格式错误（需要 4 个 TAB 分隔字段：key/count/mode/reason）：%s:%d"
                  % (path, lineno), file=sys.stderr)
            sys.exit(2)
        key, cnt, mode, reason = parts
        if mode not in ("down", "keep"):
            print("基线条目 mode 非法（只认 down/keep）：%s:%d %s" % (path, lineno, mode), file=sys.stderr)
            sys.exit(2)
        if not reason.strip():
            print("基线条目缺理由（没有理由的基线等于把判据藏起来）：%s:%d" % (path, lineno), file=sys.stderr)
            sys.exit(2)
        if not cnt.isdigit():
            print("基线条目 count 不是数字：%s:%d %s" % (path, lineno, cnt), file=sys.stderr)
            sys.exit(2)
        if key in seen:
            print("基线文件里 key 重复：%s:%d %s" % (path, lineno, key), file=sys.stderr)
            sys.exit(2)
        seen.add(key)
        entries.append((key, int(cnt), mode, reason, lineno))
    if not entries:
        print("基线文件没有任何条目 —— 门禁会退化成永远通过", file=sys.stderr)
        sys.exit(2)
    return entries

def compare(entries, counts):
    viol, stale, improved = [], [], []
    for key, base, mode, reason, lineno in entries:
        cur = counts.get(key, 0)
        if cur == 0:
            stale.append((key, base, lineno, reason))
        elif cur > base:
            viol.append((key, base, cur, mode, lineno, reason))
        elif cur < base:
            improved.append((key, base, cur, mode))
    registered = {e[0] for e in entries}
    unregistered = [(k, v) for k, v in sorted(counts.items()) if v and k not in registered]
    return viol, stale, improved, unregistered

entries = load_allow(allow_path)
counts = collect(root)
viol, stale, improved, unregistered = compare(entries, counts)

fail = False

if viol:
    fail = True
    print("✗ 超过基线的模块（控制器里的词条 key 一律来自模块 enums；要放宽基线必须在")
    print("  controller-i18n-literals-allow.txt 里改数字并写明理由，让它在 diff 里可见）：")
    for key, base, cur, mode, lineno, reason in viol:
        print("    %-24s 基线 %4d → 现在 %4d（%+d，mode=%s）  %s:%d"
              % (key, base, cur, cur - base, mode, allow_path, lineno))
    print("      理由字段：%s" % reason)
    print("  修的方向：把 key 提成 `internal/module/<模块>/enums/` 的常量（老式 `MsgXxx = \"MsgXxx\"`")
    print("  或点分 key 常量组，照该文件既有分组写），控制器只引用常量。")

if stale:
    fail = True
    print("✗ 僵尸基线条目（计数已归零 —— 说明这个模块已经写干净了，基线行必须跟着删掉；")
    print("  只增不减的清单等于没有门禁）：")
    for key, base, lineno, reason in stale:
        print("    %-24s 基线 %4d → 现在 0  %s:%d" % (key, base, allow_path, lineno))

if unregistered:
    fail = True
    print("✗ 未登记的模块出现了词条 key 字面量（新模块要么当场写干净、要么在这里登记存量）：")
    for key, cur in unregistered:
        print("    %-24s 现在 %4d" % (key, cur))

if not fail and improved:
    print("✓ 计数下降，基线可下调（粘贴回 %s 即可）：" % allow_path)
    for key, base, cur, mode in improved:
        print("    %-34s %4d → %d（mode=%s）" % (key, base, cur, mode))

# 负向自检：判据必须能报违规，否则它已经退化成空转。
if not fail:
    probe_key = entries[0][0]
    forced = collections.Counter(counts)
    forced[probe_key] = max(counts.get(probe_key, 0), entries[0][1]) + 1
    pv, _, _, _ = compare(entries, forced)
    if not pv:
        print("✗ 判据自检失败：人为把 %s 的计数 +1 之后没有报违规 —— 判据已退化成空转。"
              % probe_key, file=sys.stderr)
        sys.exit(2)

if fail:
    sys.exit(1)

print("✓ 控制器词条 key 字面量门禁通过（%d 条基线）" % len(entries))
PYEOF
