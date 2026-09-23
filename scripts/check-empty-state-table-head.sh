#!/usr/bin/env bash
# check-empty-state-table-head.sh — 禁止「空态吃掉表头」（审计 02-L §P0-3）。
#
# 为什么需要一条脚本：这个模式写起来同样太顺手了 ——
#
#     {{if len(.Rows) == 0}}
#     <div class="empty-state">…</div>
#     {{else}}
#     <form …><table class="data-table"><thead>…</thead>…
#
# 空数据 / 筛选无结果时**整张表连同表头一起不渲染**：用户看不到有哪些列，
# 也无从确认自己是不是筛错了列，比看到一张只有表头的空表格更困惑。
# 它是全站最普遍的一类布局缺陷（首轮静态扫描在 43 处命中，横跨 12 个模块），
# 而且**修的时候极容易只改一个页面、下一个页面又照旧写**（管理域四页修过之后，
# 同模块的 departments / menus / i18n / navigations / dashboard 仍是旧写法）。
#
# 判据（按**形状**，不按字面量）：对每个 `{{if}}` 块，
#   if 段渲染了 `.empty-state`  ∧  if 段里**没有** `<table`  ∧  if 段里**没有** `colspan=`
#   ∧ else 段里有 `<table`
# ⇒ 命中。第三条（`colspan`）是**已正确形态的识别特征**：正确的写法把空态放进
# `<tbody>` 的一个 `<td colspan="列数">` 行里，表头因此始终存在。
#
# Jet 的 `{{if}} / {{range}} / {{block}}` … `{{else}} / {{end}}` 用栈配对解析，
# 所以嵌套块、`{{range}}` 里的 if、一行写多个 token 都不会误判（纯文本正则会）。
#
# **解析前必须先剥掉 Jet 注释**（`{* … *}`）：注释里写的字面 `{{range}}` / `{{if}}`
# 会被 token 正则当成真 token 压栈，之后每一个 `{{end}}` 都少配一层，`{{else}}` 会配到
# 伪节点上 —— 实测踩过：某模板注释里写了「表头在 `{{range}}` 里提不出来」，导致同文件的
# 真空态被误报、另一个文件反而侥幸漏报。剥离方式是把注释整段换成**等长空白并保留换行**，
# 这样行号仍与原文一一对应（报错行号能直接定位），同时注释里的 HTML 关键字也不再参与判定。
#
# 这是启发式：它宁可多报（else 段里的 `<table>` 属于别的卡片）也不放过 ——
# 多报的一行看一眼就能确认，漏报的那一处要等用户来问「为什么筛选完没有表头」。
# 确认不是缺陷的写进豁免清单（必须写明理由）。
#
# 豁免清单：scripts/empty-state-table-head-allow.txt（`<仓库相对路径>` TAB `<理由>`）。
# 条目不再命中即失败 —— 只增不减的清单等于没有门禁。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TPL_DIR="$ROOT/internal/templates/admin"
ALLOW="$ROOT/scripts/empty-state-table-head-allow.txt"

if [ ! -d "$TPL_DIR" ]; then
  echo "没有找到后台模板目录 internal/templates/admin：目录结构变了？" >&2
  exit 2
fi

python3 - "$TPL_DIR" "$ALLOW" <<'PY'
import pathlib
import re
import sys

tpl_dir = pathlib.Path(sys.argv[1])
allow_path = pathlib.Path(sys.argv[2])

# Jet 块起始 / 分界 / 结束 token。只认 token 名，参数不参与判定。
TOKEN = re.compile(r"\{\{\s*(if|range|block|else|end)\b", re.I)

# Jet 注释 `{* … *}`（不支持嵌套，取到第一个 `*}`）。
COMMENT = re.compile(r"\{\*.*?\*\}", re.S)

# 空态元素与「已正确形态」的两个标志。
EMPTY = "empty-state"
TABLE = "<table"
COLSPAN = "colspan"


def strip_comments(text):
    """把 Jet 注释换成等长空白（保留换行）。

    为什么不能直接删：删掉会让后面所有行的行号前移，报出来的 `文件:行` 就对不上了。
    为什么必须剥：注释里写的字面 `{{range}}` / `{{if}}` 会被 TOKEN 当成真 token 压栈，
    栈从此错位（详见文件头注释里的实测案例）。
    """
    return COMMENT.sub(lambda m: re.sub(r"[^\n]", " ", m.group(0)), text)


def scan(text):
    """返回命中列表 [(line, kind)]：kind 用来区分「if 无 else」与「else 里才有表」。"""
    stack = []
    hits = []
    for m in TOKEN.finditer(text):
        kind = m.group(1).lower()
        if kind in ("if", "range", "block"):
            stack.append(
                {
                    "kind": kind,
                    "body_start": m.end(),
                    "else_token_start": None,
                    "else_end": None,
                }
            )
        elif kind == "else":
            if not stack:
                continue
            stack[-1]["else_token_start"] = m.start()
            stack[-1]["else_end"] = m.end()
        elif kind == "end":
            if not stack:
                continue
            node = stack.pop()
            body_start = node["body_start"]
            end_start = m.start()
            if node["else_token_start"] is None:
                # 没有 else：空态与表格若同段出现，表头仍在，不算命中。
                continue
            if_seg = text[body_start : node["else_token_start"]]
            else_seg = text[node["else_end"] : end_start]
            if EMPTY not in if_seg:
                continue
            if TABLE in if_seg or COLSPAN in if_seg:
                # 已经是对的形态：表头在空态之外，或空态本身就在 <td colspan> 里。
                continue
            if TABLE not in else_seg:
                continue
            line = text[: node["else_token_start"]].count("\n") + 1
            hits.append((line, node["kind"]))
    return hits


def load_allow():
    """<相对路径>\t<理由>；返回 {path: reason}。"""
    out = {}
    if not allow_path.exists():
        return out
    for raw in allow_path.read_text(encoding="utf-8").splitlines():
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        path, _, reason = line.partition("\t")
        path, reason = path.strip(), reason.strip()
        if not reason:
            print(f"✗ 豁免清单 {allow_path.name} 的这条没有写理由：{path}", file=sys.stderr)
            sys.exit(2)
        out[path] = reason
    return out


allow = load_allow()
hits = {}      # 相对路径 -> [(line, kind)]
scanned = 0

for f in sorted(tpl_dir.rglob("*.html")):
    scanned += 1
    text = f.read_text(encoding="utf-8", errors="replace")
    if EMPTY not in text:
        continue
    # 先剥注释再配对（行号由等长空白保持）。
    found = scan(strip_comments(text))
    if found:
        hits[str(f.relative_to(tpl_dir.parent.parent.parent))] = found

blocking = {p: v for p, v in hits.items() if p not in allow}
stale = [p for p in allow if p not in hits]

if stale:
    print("✗ 豁免清单里这些条目已经不再命中（修好了 / 文件改名了）—— 请从豁免清单删掉：", file=sys.stderr)
    for p in sorted(stale):
        print(f"   {p}", file=sys.stderr)
    sys.exit(1)

if blocking:
    total = sum(len(v) for v in blocking.values())
    print(f"✗ 空态分支吃掉了表头（空数据时整张表连同列名一起消失）：{total} 处 / {len(blocking)} 个文件", file=sys.stderr)
    for p in sorted(blocking):
        for line, kind in blocking[p]:
            print(f"   {p}:{line}  ({kind} 分支渲染 .empty-state，<table> 却在 {{else}} 里)", file=sys.stderr)
    print("", file=sys.stderr)
    print("  修法：把 <table>/<thead> 移到 {{if}} 外面，空态放进 <tbody> 的", file=sys.stderr)
    print('  <tr><td colspan="列数" class="cell-wrap"> 行里（样板：internal/templates/admin/permissions.html:55-75）。', file=sys.stderr)
    print("  确认不是缺陷的（整页只有引导、页面没有列表语义）写进", file=sys.stderr)
    print("  scripts/empty-state-table-head-allow.txt 并写明理由。", file=sys.stderr)
    sys.exit(1)

suffix = f"；{len(allow)} 个已登记的豁免仍待接手" if allow else ""
print(f"✓ 未发现「空态吃掉表头」的列表页（已扫描 {scanned} 个后台模板{suffix}）")
PY
