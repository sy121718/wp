#!/usr/bin/env bash
# check-shell-migration.sh — shell 删除迁移的「阶段零」门禁（task-7 盘点 / task-12 落地）。
#
# 为什么需要它：`internal/shell` 的终局是**删除**（责任归 admin 模块 / middleware / 通用工具）。
# 这件事最难的不是搬代码，而是「搬完了没有」—— shell 的使用点分散在 46 个 scope 里，
# 删掉三成和删掉一半在编译上没有任何区别。只有把计数固定成基线、且**只允许下降**，
# 「这一批删干净了吗」才是可回答的问题；否则迁移会在「看着差不多」的地方停下。
#
# 判据（三条，全部是**按 scope 聚合的计数**）：
#   1. `shell.*` 使用点：按 scope（module:<m> / test:<m> / routers / templates / internal:<pkg>）
#      × 生产/测试 聚合。超过基线 → 违规；低于基线 → 通过，并打印可直接粘贴的基线行。
#   2. 页面 POST 出口形态：按模块统计 shell.RenderJump 调用点、模块自建 jump/notice/fail
#      helper 定义数、c.String 直出文本处数 —— 这三个数是「页面写操作还没迁到 JSON API」
#      的可测代理（迁移方向见 lead 口径：前端 fetch 现有 API，页面 POST 逐步删除，权限点保留）。
#   3. 基线条目**不再命中即失败**（僵尸清单等于没有门禁）—— 与 check-no-internal-error-leak.sh
#      / check-route-registration-placement.sh / page-endpoint-authz-allow.txt 同规。
#
# 为什么基线只到 scope 粒度、不列具体引用：把 1371 个引用抄进清单，下一次改一行动模板就要
# 重写清单，这份门禁会在两周内被绕过 —— 而被绕过的门禁比没有门禁更坏，它让人以为守住了
# （同 scripts/check-i18n-coverage.sh 的取舍）。
#
# 口径（改口径等于改门禁，复算时三条必须一致，脚本头与 shell-migration-allow.txt 各写一份）：
#   · 只统计 *.go；**跳过整行注释**（行首可选空白后 `//`）。注释里的 `shell.RenderJump(`
#     会让计数虚高，而虚高的数字会让人不再相信这个门禁（docs/rules/testing.md 记的坑）；
#   · `internal/shell/**` 自身不计（包内自引用不是使用点）；
#   · _test.go 与 public/test/** 记 test 桶，其余记 prod 桶；
#   · 计数单位是**出现次数**，不是文件数或行数。
#
# mode 两档（写在基线文件里）：
#   down —— 迁移收敛面，只允许下降；
#   keep —— 只作「新增即红」的守卫，不要求下降（必须写明为什么不属收敛面）。
#
# 自带负向自检：树干净时，脚本把探针条目的基线**临时收紧到当前实测值**，再人为 +1 比一次
# —— 判据必须报违规，报不出来说明判据已退化成空转，此时**脚本自己判失败**（退出码 2）。
# 为什么收紧到实测值而不是直接比存储基线：down 条目允许低于基线（那正是「迁移在推进」），
# 拿「实测 +1」去比存储基线时，只有实测恰好等于基线才可能越界 —— 于是一次正常下降会被
# 误报成「判据空转」。收紧之后那个 +1 必然越界，自检才真的在验判据（正式基线文件不动）。
# 门禁绿 ≠ 没问题：本脚本是启发式的形状计数，不解析语法，注释掉的调用点、动态拼出的
# `shell.…` 名字都看不见（同 AGENTS.md「门禁绿也不等于没问题」）。
#
# 退出码：0 通过 / 1 违规或僵尸基线 / 2 环境或自检失败。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ALLOW_FILE="$ROOT/scripts/shell-migration-allow.txt"

if [ ! -f "$ALLOW_FILE" ]; then
  echo "找不到基线文件：$ALLOW_FILE" >&2
  exit 2
fi

python3 - "$ROOT" "$ALLOW_FILE" <<'PYEOF'
import collections, os, re, sys

root, allow_path = sys.argv[1], sys.argv[2]

# 与门禁无关的目录一律不进统计：.scratch 是本地草稿，node_modules/vendor 是第三方。
# .worktrees 是嵌套 worktree（另一个分支的完整副本）：扫进来等于把同一份代码按分支数重复计数，
# 而且那些副本的改动不属于当前工作区，会让本门禁对比的基线无声漂移。
SKIP_DIRS = {".git", ".scratch", "node_modules", "vendor", ".review-graph", ".worktrees"}

def read(path):
    with open(path, encoding="utf-8", errors="ignore") as fh:
        return fh.read()

def drop_line_comments(src):
    """只剥整行注释，不剥行尾 —— 行尾注释里有代码时剥了会漏计（宁可多计一行让人看一眼）。"""
    return "\n".join(l for l in src.split("\n") if not l.lstrip().startswith("//"))

SHELL_REF = re.compile(r"\bshell\.[A-Z]\w*")

def scope_of(rel):
    if rel.startswith("internal/module/"):
        parts = rel.split("/")
        return "module:" + (parts[2] if len(parts) > 2 else "?")
    if rel.startswith("internal/routers"):
        return "routers"
    if rel.startswith("internal/templates"):
        return "templates"
    if rel.startswith("public/test/"):
        parts = rel.split("/")
        return "test:" + (parts[2] if len(parts) > 2 else "?")
    if rel.startswith("internal/"):
        return "internal:" + rel.split("/")[1]
    return "other:" + rel.split("/")[0]

def collect(root):
    counts = collections.Counter()

    # 判据 1：shell.* 使用点，按 scope × 生产/测试 聚合。
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames[:] = [d for d in dirnames if d not in SKIP_DIRS]
        for name in filenames:
            if not name.endswith(".go"):
                continue
            path = os.path.join(dirpath, name)
            rel = os.path.relpath(path, root).replace(os.sep, "/")
            if rel.startswith("internal/shell/"):
                continue
            n = len(SHELL_REF.findall(drop_line_comments(read(path))))
            if not n:
                continue
            kind = "test" if (name.endswith("_test.go") or rel.startswith("public/test/")) else "prod"
            counts["use:%s:%s" % (scope_of(rel), kind)] += n

    # 判据 2：页面 POST 出口形态，按模块聚合。
    modules_dir = os.path.join(root, "internal", "module")
    if not os.path.isdir(modules_dir):
        print("找不到 internal/module 目录", file=sys.stderr)
        sys.exit(2)
    for mod in sorted(os.listdir(modules_dir)):
        http = os.path.join(modules_dir, mod, "inbound", "http")
        if not os.path.isdir(http):
            continue
        jump = helper = cstr = 0
        for name in sorted(os.listdir(http)):
            if not name.endswith(".go") or name.endswith("_test.go"):
                continue
            src = drop_line_comments(read(os.path.join(http, name)))
            jump += len(re.findall(r"\bshell\.RenderJump\(", src))
            helper += len(re.findall(r"^func \w*(?:[Jj]ump|[Nn]otice|[Ff]ail)\w*\(c \*gin\.Context", src, re.M))
            cstr += len(re.findall(r"\bc\.String\(", src))
        if jump:
            counts["exit:%s:jump" % mod] = jump
        if helper:
            counts["exit:%s:ownhelper" % mod] = helper
        if cstr:
            counts["exit:%s:cstring" % mod] = cstr

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
    print("✗ 超过基线的 scope（迁移只能让计数下降；要放宽基线必须在 shell-migration-allow.txt 里")
    print("  改数字并写明理由，让它在 diff 里可见）：")
    for key, base, cur, mode, lineno, reason in viol:
        print("    %-34s 基线 %4d → 现在 %4d（%+d，mode=%s）  %s:%d"
              % (key, base, cur, cur - base, mode, allow_path, lineno))
    print("      理由字段：%s" % reason)
    print("  修的方向：把新调用点挪到终局归属包，或（只在确属收敛面外时）带理由放宽基线。")

if stale:
    fail = True
    print("✗ 僵尸基线条目（计数已归零 —— 说明这一处搬完了，基线行必须跟着删掉；")
    print("  只增不减的清单等于没有门禁）：")
    for key, base, lineno, reason in stale:
        print("    %-34s 基线 %4d → 现在 0  %s:%d" % (key, base, allow_path, lineno))

if unregistered:
    fail = True
    print("✗ 未登记的新 scope（新增的 shell 使用面必须先登记终局归属，再改基线）：")
    for key, cur in unregistered:
        print("    %-34s %4d 次" % (key, cur))
    print("  新模块/新目录出现时这条会响：先想清楚它的 shell 使用点归谁，再写进基线。")

if improved and not fail:
    print("✓ 计数下降，基线可下调（粘贴回 %s 即可）：" % allow_path)
    for key, base, cur, mode in improved:
        print("    %-34s %4d → %d（mode=%s）" % (key, base, cur, mode))

if fail:
    sys.exit(1)

# 负向自检：树干净时才做 —— 把探针条目的基线**临时收紧到现在实测值**，再人为 +1，判据必须报违规。
# 收紧的理由见文件头：拿「实测 +1」比存储基线时，只有实测恰好等于基线才可能越界，
# 一次正常下降就会被误报成「判据空转」。收紧副本只活在本次自检里，正式基线文件不动。
# 报不出来说明判据已退化成空转（例如 collect 没扫到任何文件、或 key 拼不上了），
# 此时脚本自判失败，而不是继续报「✓ 通过」。
probe = [e for e in entries if e[0].startswith("use:") and e[2] == "down"]
if not probe:
    print("✗ 自检失败：基线里没有 down 模式的 use: 条目可以做注入探针", file=sys.stderr)
    sys.exit(2)
probe_key, probe_base = probe[0][0], probe[0][1]
probe_now = counts.get(probe_key, 0)
injected = collections.Counter(counts)
injected[probe_key] += 1
tightened = [(k, (probe_now if k == probe_key else b), m, r, ln)
             for k, b, m, r, ln in entries]
pviol, _, _, _ = compare(tightened, injected)
if not any(k == probe_key for k, _, _, _, _, _ in pviol):
    print("✗ 自检失败：把 %s 的基线临时收紧到实测 %d（存储基线 %d）、再人为 +1（→ %d）后"
          "判据没有报违规 —— 判据已退化成空转"
          % (probe_key, probe_now, probe_base, probe_now + 1), file=sys.stderr)
    sys.exit(2)

# 正向自检：同一个条目人为**降到基线以下**，判据必须不报违规（下降只进 improved 那一栏）。
# 判据坏成「把下降当超限」时这条会响 —— 而那种坏法在只降不增的迁移里会让每次收敛都假红。
relaxed = collections.Counter(counts)
relaxed[probe_key] = probe_now - 1
dviol, _, dimproved, _ = compare(tightened, relaxed)
if any(k == probe_key for k, _, _, _, _, _ in dviol):
    print("✗ 自检失败：把 %s 人为降到 %d（低于基线 %d）后判据报违规 —— 判据把「下降」当成了「超限」"
          % (probe_key, probe_now - 1, probe_now), file=sys.stderr)
    sys.exit(2)
if not any(k == probe_key for k, _, _, _ in dimproved):
    print("✗ 自检失败：把 %s 人为降到 %d（低于基线 %d）后判据没有把它记成可下调 —— 下降没被识别"
          % (probe_key, probe_now - 1, probe_now), file=sys.stderr)
    sys.exit(2)

print("✓ shell 迁移门禁通过（%d 条基线；shell.* 使用点 %d 处，页面出口 %d 处）"
      % (len(entries),
         sum(v for k, v in counts.items() if k.startswith("use:")),
         sum(v for k, v in counts.items() if k.startswith("exit:"))))
PYEOF
