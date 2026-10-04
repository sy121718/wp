#!/usr/bin/env bash
# check-dto-immutability.sh — 契约 DTO 的「可被调用方就地改写」形状盘点（**warn-only**）。
#
# 为什么需要它：AGENTS.md 规定跨模块只传「不可变 DTO」，但这条要求在**代码形状**上
# 此前零机器验证。调用方拿到的 Resp 里若带 []T / map / 裸指针，它就能就地改元素 ——
# 改的是提供方返回结构里的**同一块内存**（切片共享底层数组、map 共享哈希表、指针
# 指向同一个对象），提供方自己那份也跟着变。这类 bug 不报错、不 panic，只是
# 「莫名其妙少了一行」「这个字段什么时候被改了」，只能靠人盯；而形状是静态可见的。
#
# 判据（**启发式**，只认形状不认语义）：internal/module/*/dto/*.go 里名字以
# Resp / Response 结尾的 struct，字段类型字面含 `map[` / `[]` / 裸指针 `*`
# 即记一条告警（文件:行 + 类型名 + 字段名）。
#
# 已知的误报与漏报（改这条判据前先看这里）：
#   · 漏 —— 判据锚定的是**字面量形状**：`json.RawMessage`（[]byte 别名）、`type T = []X`
#     这类别名、经方法返回的可变类型，字面里没有 [] / * / map[ ，判据一律不认；
#   · 漏 —— 只扫 dto 包内定义的 struct；把可变形状藏在非 dto 包的「响应形状」里就绕过了；
#   · 误 —— `*float64` / `*utils.JSONTime` 这类「可空标量」是**刻意的零值表达**
#     （「未填」与「填了 0」是两回事）。它确实可被就地改写，但改写收益极低；
#     warn-only 的口径就是不替人做这个取舍，先把清单摆出来让人评估。
#
# **warn-only**：无论发现多少告警都 exit 0；只有脚本自身错误（目标目录不存在 /
# 没有 python3）才非 0。要升级成硬门禁时得先给基线（例如「告警数不得高于 N」或
# 「新增的切片字段必须进白名单」），不能直接改成 exit 1 —— 现在上百条告警里大部分是
# 既有设计，一刀切只会逼人把字段藏起来。
#
# 用法：bash scripts/check-dto-immutability.sh [--detail]   # 默认每模块只列前 5 条；--detail 列全
# 依赖：bash + python3（仅标准库），不连数据库。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT" || exit 1

TARGET="internal/module"

if [ ! -d "$TARGET" ]; then
  echo "找不到目标目录：$TARGET" >&2
  exit 2
fi
if ! command -v python3 >/dev/null 2>&1; then
  echo "找不到 python3，无法盘点 DTO 字段形状。" >&2
  exit 2
fi

python3 - "$ROOT" "$TARGET" <<'PY'
import collections, glob, os, re, sys

root, target = sys.argv[1], sys.argv[2]

# 只认「Resp / Response 结尾」的 struct：它们是对外响应形状，也正是跨模块流转的那批。
type_re = re.compile(r'^type\s+([A-Za-z_][A-Za-z0-9_]*)(?:\[[^\]]*\])?\s+struct\s*\{')
field_re = re.compile(r'^([A-Za-z_][A-Za-z0-9_]*)\s+(.+)$')

modules = collections.OrderedDict()   # 模块 -> [类型数, [(位置, 类型名, 字段名, 类型字面, 类别)]]
by_kind = collections.Counter()

for path in sorted(glob.glob(os.path.join(root, target, '*', 'dto', '*.go'))):
    if path.endswith('_test.go'):
        continue
    mod = path.split(os.sep)[-3]
    stat = modules.setdefault(mod, [0, []])

    with open(path, encoding='utf-8') as fh:
        lines = fh.read().splitlines()

    cur = None      # 当前正在扫的 Resp 类型名
    depth = 0       # 花括号深度；1 = 当前类型的直接字段层
    for i, raw in enumerate(lines, 1):
        # 形状判定只看代码：反引号里是 struct tag、`//` 之后是注释，两者的花括号与
        # 星号都不代表字段形状（注释里写 `[]Foo` 不该产生告警）。
        code = raw.split('`')[0]
        if '//' in code:
            code = code.split('//')[0]
        code = code.strip()

        if depth == 0:
            m = type_re.match(code)
            if not m:
                continue
            name = m.group(1)
            if not (name.endswith('Resp') or name.endswith('Response')):
                continue
            stat[0] += 1
            depth = code.count('{') - code.count('}')
            if depth <= 0:
                # 单行空 struct（type XResp struct{}）：算一个类型，没有字段可扫。
                depth = 0
                cur = None
            else:
                cur = name
            continue

        stripped = raw.strip()
        if stripped.startswith('//') or stripped.startswith('/*') or stripped.startswith('*'):
            continue

        # 只认直接字段层：匿名嵌套 struct 的内层字段（depth > 1）不是本类型的字段。
        if code and depth == 1:
            m = field_re.match(code)
            if m:
                fname, ftyp = m.group(1), m.group(2)
                if 'map[' in ftyp:
                    kind = 'map'
                elif '[]' in ftyp:
                    kind = 'slice'
                elif '*' in ftyp:
                    kind = 'pointer'
                else:
                    kind = ''
                if kind:
                    stat[1].append((
                        '%s:%d' % (os.path.relpath(path, root), i), cur, fname, ftyp, kind))
                    by_kind[kind] += 1

        depth += code.count('{') - code.count('}')
        if depth <= 0:
            depth = 0
            cur = None

total_types = sum(n for n, _ in modules.values())
total_warn = sum(len(w) for _, w in modules.values())

print("== DTO 不可变形状盘点（warn-only）==")
print("判据：internal/module/*/dto/*.go 里名字以 Resp / Response 结尾的 struct，")
print("      字段类型字面含 map[ / [] / 裸指针 * —— 这类字段可被调用方就地改写，")
print("      改的是提供方返回结构里的同一块内存。")
print()
print("按模块（类型数 / 告警数）：")
for mod, (n, warns) in modules.items():
    print("   %-16s %3d / %3d" % (mod, n, len(warns)))
print("   %-16s %3d / %3d" % ("合计", total_types, total_warn))

show_detail = len(sys.argv) > 3 and sys.argv[3] == "--detail"
DETAIL_PREVIEW = 5   # 默认每模块只列前 N 条：170+ 行明细会把 check-all 日志里真正的失败项淹掉

for mod, (n, warns) in modules.items():
    if not warns:
        continue
    print()
    print("── %s（%d 类型 / %d 告警）" % (mod, n, len(warns)))
    shown = warns if show_detail else warns[:DETAIL_PREVIEW]
    for loc, tname, fname, ftyp, kind in shown:
        print("   %s  %s.%s  %s" % (loc, tname, fname, ftyp))
    if not show_detail and len(warns) > DETAIL_PREVIEW:
        print("   … 其余 %d 条：bash scripts/check-dto-immutability.sh --detail" % (len(warns) - DETAIL_PREVIEW))

print()
kinds = "、".join("%s %d 条" % (k, by_kind[k]) for k in ("slice", "map", "pointer") if by_kind[k])
print("字段类别：" + (kinds if kinds else "无"))
print("warn-only：当前不阻断 CI，供人工评估。")
PY

exit 0
