#!/usr/bin/env bash
# check-page-title.sh — 后台整页渲染必须显式给 title，否则整页 500。
#
# 为什么需要一条脚本：layout.html 用点号取字段写死标题 ——
#
#     <title>{{.title}} — go_wp {{ .["t"]("shell.brand", "管理后台") }}</title>
#
# 而 shell.Prepare 只补外壳所需的键（lang / t / csrf_token / PermSet / NavGroups …），
# **不注入 title**：title 必须由每个页面 handler 自己放进 gin.H。
# 漏了不是「标题空着」这种小毛病 —— Jet 对 `.title` 取不到字段直接抛
#     there is no field or method 'title' in gin.H (.title)
# 经 internal/templates/jet_render.go 的 renderError 变成 HTTP 500，**整个页面打不开**。
# 实测一次浏览器遍历 53 条菜单路径，就抓到 3 个 500
#（/admin/system、/admin/ai/providers、/admin/ai/sessions）：三处都是新页面抄了
# 别处的 shell.Prepare 调用，而那个样板恰好也没有 title。
#
# 判据（按**形状**，不按字面量）：找出所有「渲染整页模板」的 c.HTML 调用点 ——
# 模板名是字面量、且该模板属于整页模板集合（文件里 `extends "../layout.html"`）；
# 再取这次渲染的数据源（第三个参数）：直接内联的 `shell.Prepare(c, gin.H{…})`，
# 或同一函数里 `x := shell.Prepare(c, gin.H{…})` 后传 `x` 的变量形态；
# 数据源的 gin.H 里**没有** `"title"` 键 ⇒ 命中。
#
# 只认确定的形状：模板名是变量、或数据源来自别的函数返回时，本脚本不猜（不报）。
# 宁可漏报也不制造噪音 —— 漏掉的那处会以 500 的形式在遍历菜单时立刻暴露，
# 而误报会让下一个人开始往豁免清单里塞条目。
#
# 这是静态启发式：Go 源码按括号配平切分，不解析 AST；注释里的 `c.HTML(` 也会被扫到，
# 但注释里的调用同样要满足上述形状才会命中。
#
# 豁免清单：scripts/page-title-allow.txt（`<go 文件相对路径>:<行号>` TAB `<理由>`）。
# 条目不再命中即失败 —— 只增不减的清单等于没有门禁。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ALLOW="$ROOT/scripts/page-title-allow.txt"

if [ ! -d "$ROOT/internal/templates/admin" ]; then
  echo "没有找到后台模板目录 internal/templates/admin：目录结构变了？" >&2
  exit 2
fi

python3 - "$ROOT" "$ALLOW" <<'PY'
import pathlib
import re
import sys

root = pathlib.Path(sys.argv[1])
allow_path = pathlib.Path(sys.argv[2])

TPL_ROOT = root / "internal/templates"
ADMIN_TPL = TPL_ROOT / "admin"

# 整页模板：文件里 `extends "../layout.html"`（少数写 `extends "layout.html"`）。
EXTENDS = re.compile(r'extends\s+"[^"]*layout\.html"')

# `x := shell.Prepare(` / `x = shell.Prepare(` —— 变量形态的数据源。
PREPARE_ASSIGN = re.compile(r'(\w+)\s*:?=\s*shell\.Prepare\(')

# c.HTML( 调用点。
HTML_CALL = re.compile(r'\bc\.HTML\(')


def read(path):
    return path.read_text(encoding="utf-8", errors="replace")


def skip_string(text, i):
    """i 指向引号；返回闭合引号之后的下标（Go 的 `\\` 转义只对双引号有意义）。"""
    quote = text[i]
    i += 1
    while i < len(text):
        ch = text[i]
        if quote == '"' and ch == "\\":
            i += 2
            continue
        if ch == quote:
            return i + 1
        i += 1
    return i


def split_top_level(text, start, end, sep=","):
    """按 sep 切分 text[start:end]，括号 / 花括号 / 方括号 / 字符串内的 sep 不切。

    返回 [(片段原文字, 片段起始下标), …]。
    """
    parts = []
    depth = 0
    seg_start = start
    i = start
    while i < end:
        ch = text[i]
        if ch in ('"', '`'):
            i = skip_string(text, i)
            continue
        if ch in "([{":
            depth += 1
        elif ch in ")]}":
            depth -= 1
        elif ch == sep and depth == 0:
            parts.append((text[seg_start:i], seg_start))
            seg_start = i + 1
        i += 1
    parts.append((text[seg_start:end], seg_start))
    return parts


def match_delim(text, open_idx, oc, cc):
    """open_idx 指向 oc；返回与之配对的 cc 下标（找不到返回 -1）。"""
    depth = 0
    i = open_idx
    while i < len(text):
        ch = text[i]
        if ch in ('"', '`'):
            i = skip_string(text, i)
            continue
        if ch == oc:
            depth += 1
        elif ch == cc:
            depth -= 1
            if depth == 0:
                return i
        i += 1
    return -1


def gin_literal(expr):
    """从表达式里取出 `gin.H{…}` 字面量原文；没有则返回 None。"""
    m = re.search(r'gin\.H\s*\{', expr)
    if not m:
        return None
    brace = expr.index("{", m.start())
    close = match_delim(expr, brace, "{", "}")
    if close < 0:
        return None
    return expr[brace : close + 1]


def line_of(text, idx):
    return text[:idx].count("\n") + 1


# ---- 1. 整页模板集合 -------------------------------------------------------
page_templates = set()
for f in sorted(ADMIN_TPL.rglob("*.html")):
    if EXTENDS.search(read(f)):
        page_templates.add(str(f.relative_to(TPL_ROOT).with_suffix("")).replace("\\", "/"))

if not page_templates:
    print("✗ 一个整页模板都没识别出来（extends 写法变了？）—— 判据可能已失效", file=sys.stderr)
    sys.exit(2)

# ---- 2. 收集字符串常量 -----------------------------------------------------
# 模板名常以 `pageTemplate = "admin/ai/providers"` 的常量传入 c.HTML，先建名字表。
# 同名常量映射到多个不同值时**不猜**（跨包重名），避免把别的包的值当成模板名。
STRING_DEF = re.compile(r'\b(\w+)\s*:?=\s*"([^"]*)"')
string_consts = {}
for f in sorted((root / "internal").rglob("*.go")):
    text = read(f)
    if '"' not in text:
        continue
    for m in STRING_DEF.finditer(text):
        string_consts.setdefault(m.group(1), set()).add(m.group(2))

# ---- 3. 扫 Go 源码 ---------------------------------------------------------
hits = []  # (相对路径, 行号, 模板名)
scanned_files = 0

for f in sorted((root / "internal").rglob("*.go")):
    text = read(f)
    if "c.HTML(" not in text:
        continue
    scanned_files += 1

    # 变量形态：变量名 -> gin.H 原文
    assigns = {}
    for m in PREPARE_ASSIGN.finditer(text):
        paren = text.index("(", m.start())
        close = match_delim(text, paren, "(", ")")
        if close < 0:
            continue
        g = gin_literal(text[m.end() : close])
        if g:
            assigns[m.group(1)] = g

    for m in HTML_CALL.finditer(text):
        paren = text.index("(", m.start())
        close = match_delim(text, paren, "(", ")")
        if close < 0:
            continue
        args = split_top_level(text, paren + 1, close)
        if len(args) < 3:
            continue
        tpl_arg = args[1][0].strip()
        tm = re.fullmatch(r'"([^"]+)"', tpl_arg)
        if tm:
            tpl = tm.group(1)
        elif re.fullmatch(r"[A-Za-z_]\w*", tpl_arg):
            vals = string_consts.get(tpl_arg)
            if not vals or len(vals) != 1:
                continue  # 常量名有歧义：不猜
            tpl = next(iter(vals))
        else:
            continue  # 模板名是表达式：不猜
        if tpl not in page_templates:
            continue

        data_arg = args[2][0].strip()
        g = gin_literal(data_arg)  # 内联 shell.Prepare(c, gin.H{…})
        if g is None and re.fullmatch(r"[A-Za-z_]\w*", data_arg):
            g = assigns.get(data_arg)
        if g is None:
            continue  # 数据源来自别处：不猜
        if re.search(r'"title"\s*:', g):
            continue
        hits.append(
            (
                str(f.relative_to(root)).replace("\\", "/"),
                line_of(text, args[2][1]),
                tpl,
            )
        )


def load_allow():
    out = {}
    if not allow_path.exists():
        return out
    for raw in allow_path.read_text(encoding="utf-8").splitlines():
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        key, _, reason = line.partition("\t")
        key, reason = key.strip(), reason.strip()
        if not reason:
            print(f"✗ 豁免清单 {allow_path.name} 的这条没有写理由：{key}", file=sys.stderr)
            sys.exit(2)
        out[key] = reason
    return out


allow = load_allow()
hit_keys = {f"{p}:{ln}": (p, ln, t) for p, ln, t in hits}

blocking = {k: v for k, v in hit_keys.items() if k not in allow}
stale = [k for k in allow if k not in hit_keys]

if stale:
    print("✗ 豁免清单里这些条目已经不再命中（修好了 / 文件改了）—— 请删掉：", file=sys.stderr)
    for k in sorted(stale):
        print(f"   {k}", file=sys.stderr)
    sys.exit(1)

if blocking:
    print(
        f"✗ 整页渲染没有给 title，页面会直接 500：{len(blocking)} 处"
        f"（已扫 {len(page_templates)} 个整页模板 / {scanned_files} 个含 c.HTML 的 Go 文件）",
        file=sys.stderr,
    )
    for k in sorted(blocking):
        p, ln, t = blocking[k]
        print(f"   {p}:{ln}  渲染 {t} 的数据里没有 \"title\"", file=sys.stderr)
    print("", file=sys.stderr)
    print("  修法：在 shell.Prepare 的 gin.H 里补", file=sys.stderr)
    print('        "title": shell.TranslateFor(c)(enums.XXXTitle, "中文兜底"),', file=sys.stderr)
    print("  样板：internal/module/product/inbound/http/product_detail_template_page.go:135。", file=sys.stderr)
    print(f"  确认不是缺陷的写进 {allow_path.name} 并写明理由。", file=sys.stderr)
    sys.exit(1)

suffix = f"；{len(allow)} 个已登记的豁免仍待接手" if allow else ""
print(f"✓ 后台整页渲染都有 title（已扫 {len(page_templates)} 个整页模板 / {scanned_files} 个含 c.HTML 的 Go 文件{suffix}）")
PY
