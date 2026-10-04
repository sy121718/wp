#!/usr/bin/env python3
# check-page-endpoint-authz.py — 页面写端点的鉴权门禁（审计 H3）。
#
# 为什么需要它：scripts/check-permission-gaps.sh 比的是「路由 ↔ 权限点」，而它取的
# 路由清单是 `^[A-Z]+ /api` —— **只覆盖 API 面**。页面组（/admin、/workbench、各模块的
# pages 组）挂的是原生 gin.RouterGroup，鉴权靠**每个端点显式挂** CasbinMiddlewareForPath；
# 漏挂不会让任何既有检查变红：路由照样注册成功、权限点照样在库里（由 API 侧声明），
# 只是那个页面端点变成了「登录即可用」。
# 实例：POST /workbench/instance/save —— 任意已登录账号（含只读角色）都能跨工程覆盖
# 实例文档并触发重编译发布（已在本次修复中挂上 Casbin）。
#
# 判据（按**调用形状**，不按文件/目录名 —— 目录名会漂移，形状不会）：
#   1. 第二实参是 `permission.XxxPerm` → 声明式授权路由（permission.RouteGroup 的组链
#      自带 CasbinMiddleware，权限点声明同时供 check-permission-gaps.sh 对账）→ 免检；
#   2. 否则是原生 gin 组 → 实参里必须出现 CasbinMiddleware*，或该文件在组级
#      `.Use(...)` 里挂了它；
#   3. 都不满足 → 违规，除非命中豁免清单（scripts/page-endpoint-authz-allow.txt）。
#
# 只查 POST：本仓库路由只用 GET/POST，且 GET 不构成越权写面（既有未挂 Casbin 的页面
# 端点全是只读的）。豁免清单条目**不再命中即失败** —— 只增不减的清单等于没有门禁。
#
# 纯文本扫描（自己遍历，不依赖 rg / grep 的可用性与版本），无外部服务依赖。

from __future__ import annotations

import os
import re
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
ALLOW_FILE = os.path.join(ROOT, "scripts", "page-endpoint-authz-allow.txt")
SCAN_ROOTS = ("internal", "cmd")
NEEDLE = ".POST("


def iter_go_files():
    for base in SCAN_ROOTS:
        base_dir = os.path.join(ROOT, base)
        if not os.path.isdir(base_dir):
            continue
        for dirpath, dirnames, filenames in os.walk(base_dir):
            dirnames[:] = [d for d in dirnames if d not in ("vendor", "testdata")]
            for name in sorted(filenames):
                if not name.endswith(".go") or name.endswith("_test.go"):
                    continue
                full = os.path.join(dirpath, name)
                yield os.path.relpath(full, ROOT)


def strip_comments(src):
    """去掉注释但**保持字符位置与行号**，并标出「哪些字符在字符串字面量里」。

    位置必须保持：报错要能直接给人文件行号，而注释里的 `.POST(` 不能被当成调用。
    """
    out = []
    mask = []
    i = 0
    n = len(src)
    state = "code"

    def emit(ch, in_str):
        # 逐字符 append：mask 与 clean 必须**逐下标对齐**，多字符一次 append 会让
        # 后面的 mask[j] 越界（踩过）。
        for c in ch:
            out.append(c)
            mask.append(in_str)

    while i < n:
        ch = src[i]
        nxt = src[i + 1] if i + 1 < n else ""
        if state == "code":
            if ch == "/" and nxt == "/":
                state = "line_comment"
                emit("  ", False)
                i += 2
                continue
            if ch == "/" and nxt == "*":
                state = "block_comment"
                emit("  ", False)
                i += 2
                continue
            if ch == '"':
                state = "dquote"
            elif ch == "'":
                state = "squote"
            elif ch == "`":
                state = "backtick"
            emit(ch, state != "code")
            i += 1
            continue
        if state == "line_comment":
            if ch == "\n":
                state = "code"
                emit("\n", False)
            else:
                emit(" ", False)
            i += 1
            continue
        if state == "block_comment":
            if ch == "*" and nxt == "/":
                state = "code"
                emit("  ", False)
                i += 2
                continue
            emit("\n" if ch == "\n" else " ", False)
            i += 1
            continue
        # 字符串状态：转义序列原样保留（否则 \" 会把状态机带偏）
        if ch == "\\" and state != "backtick" and i + 1 < n:
            emit(ch, True)
            emit(src[i + 1], True)
            i += 2
            continue
        if (state == "dquote" and ch == '"') or (state == "squote" and ch == "'") or (
            state == "backtick" and ch == "`"
        ):
            emit(ch, True)
            state = "code"
            i += 1
            continue
        emit(ch, True)
        i += 1

    return "".join(out), mask


def iter_calls(clean, mask, needle):
    """产出 (调用起点下标, 参数文本)，括号配平、跳过字符串内的同名文本。"""
    i = 0
    n = len(clean)
    while True:
        j = clean.find(needle, i)
        if j < 0:
            return
        i = j + len(needle)
        if mask[j]:
            continue
        # needle 以 '.' 开头（方法调用），不需要再排除「前面是标识符字符」——
        # 那样会把 `g.POST(` 里的 g 也当成人名而全部漏掉（踩过：扫出 0 个注册点）。
        line_start = clean.rfind("\n", 0, j) + 1
        if re.match(r"^\s*func\s*\([^)]*\)\s*$", clean[line_start:j]):
            continue  # 方法定义本身（`func (g *RouteGroup) POST(`），不是注册点
        recv = re.search(r"([A-Za-z_][\w.]*)$", clean[max(0, j - 80) : j])
        if recv and recv.group(1).endswith(".RouterGroup"):
            continue  # permission.RouteGroup 的底层转发：声明与 Casbin 组链已在前一步完成
        lp = j + len(needle) - 1
        depth = 0
        p = lp
        while p < n:
            if not mask[p]:
                if clean[p] == "(":
                    depth += 1
                elif clean[p] == ")":
                    depth -= 1
                    if depth == 0:
                        break
            p += 1
        yield j, clean[lp + 1 : p]


def split_args(text):
    args = []
    buf = []
    depth = 0
    quote = None
    i = 0
    n = len(text)
    while i < n:
        ch = text[i]
        if quote:
            buf.append(ch)
            if ch == "\\" and quote != "`" and i + 1 < n:
                buf.append(text[i + 1])
                i += 2
                continue
            if ch == quote:
                quote = None
            i += 1
            continue
        if ch in "\"'`":
            quote = ch
            buf.append(ch)
            i += 1
            continue
        if ch in "([{":
            depth += 1
        elif ch in ")]}":
            depth -= 1
        if ch == "," and depth == 0:
            args.append("".join(buf).strip())
            buf = []
            i += 1
            continue
        buf.append(ch)
        i += 1
    tail = "".join(buf).strip()
    if tail:
        args.append(tail)
    return args


def first_string_literal(arg):
    for quote in ('"', "`"):
        if arg.startswith(quote):
            end = arg.find(quote, 1)
            if end > 0:
                return arg[1:end]
    return arg.strip()


def line_of(clean, index):
    return clean.count("\n", 0, index) + 1


def has_group_casbin(clean, mask):
    """组级 `.Use(...)` 里挂了 Casbin 时，该文件内的端点继承鉴权。"""
    for _, args_text in iter_calls(clean, mask, ".Use("):
        if "CasbinMiddleware" in args_text:
            return True
    return False


def load_allow():
    entries = {}
    if not os.path.isfile(ALLOW_FILE):
        return entries
    with open(ALLOW_FILE, encoding="utf-8") as fh:
        for raw in fh:
            line = raw.strip()
            if not line or line.startswith("#"):
                continue
            target, _, reason = line.partition("\t")
            path, _, route = target.strip().partition(" ")
            entries.setdefault((path.strip(), route.strip()), "").__str__()
            entries[(path.strip(), route.strip())] = reason.strip()
    return entries


def main():
    allow = load_allow()
    used = set()
    hits = []
    stats = {"total": 0, "declared": 0, "guarded": 0, "allowed": 0}

    for rel in iter_go_files():
        with open(os.path.join(ROOT, rel), encoding="utf-8") as fh:
            src = fh.read()
        clean, mask = strip_comments(src)
        group_ok = has_group_casbin(clean, mask)
        for start, args_text in iter_calls(clean, mask, NEEDLE):
            args = split_args(args_text)
            if len(args) < 2:
                continue
            stats["total"] += 1
            if args[1].startswith("permission."):
                stats["declared"] += 1
                continue
            if group_ok or any("CasbinMiddleware" in a for a in args[1:]):
                stats["guarded"] += 1
                continue
            route = first_string_literal(args[0])
            key = (rel, route)
            if key in allow:
                used.add(key)
                stats["allowed"] += 1
                continue
            hits.append((rel, line_of(clean, start), route))

    stale = sorted(k for k in allow if k not in used)

    if hits:
        print("✗ 页面写端点没有鉴权（POST 未挂 Casbin，且未在豁免清单里登记理由）：", file=sys.stderr)
        for rel, line, route in hits:
            print(f"    {rel}:{line}  POST {route}", file=sys.stderr)
        print(
            "  修法：与相邻端点同形，显式挂 CasbinMiddlewareForPath(\"<对应 API 路径>\") —— "
            "页面写端点复用 API 权限点是全仓库口径（11 个模块都这么做）；"
            "确认不需要鉴权（公开端点 / 只读端点）就登记到 scripts/page-endpoint-authz-allow.txt。",
            file=sys.stderr,
        )

    if stale:
        print("✗ 豁免清单有过期条目（已不再命中，别留下只增不减的清单）：", file=sys.stderr)
        for rel, route in stale:
            print(f"    {rel}  POST {route}", file=sys.stderr)

    if hits or stale:
        print(
            f"扫描 POST 注册点 {stats['total']} 个：声明式授权 {stats['declared']}、"
            f"显式 Casbin {stats['guarded']}、豁免 {stats['allowed']}、违规 {len(hits)}、"
            f"过期豁免 {len(stale)}",
            file=sys.stderr,
        )
        return 1

    print(
        f"✓ 页面写端点鉴权齐备（POST 注册点 {stats['total']} 个：声明式授权 {stats['declared']}、"
        f"显式 Casbin {stats['guarded']}、豁免 {stats['allowed']}）"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
