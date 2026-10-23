#!/usr/bin/env bash
# check-component-colors.sh — 组件 CSS 里的色值必须是令牌，不许裸写。
#
# 为什么：45 个组件曾散着 199 处裸色值（#2563eb 39 次、#6b7280 36 次、#fff 33 次…），
# 改一次品牌色要动二十几个文件，而"我们一共用了几种颜色"没人答得出来。
#
# 判据的形状（关键）：
#
#   ✅ var(--sky-c-primary, #2563eb)     —— 令牌 + 兜底。兜底是必需的：无主题页面
#                                           （纯静态产物、没有后台 data-theme）里
#                                           --sky-c-* 根本没定义，去掉兜底就变成
#                                           透明/黑色，是真实回归。
#   ❌ color: #2563eb                    —— 裸色值，改主题色时它不会跟着变。
#
# 所以本门禁拦的是**裸色值**，不是色值本身。把 `var(--x, #hex)` 里的兜底也禁掉，
# 等于砍掉无主题渲染路径 —— 那是把一种正确写法判成错误。
#
# 覆盖范围：
#   · internal/builder/components/*/*.css  —— 组件样式（主要对象）
#   · 不含 *_test.go 与 testdata/golden（后者是产物基线，令牌来自源样式）
#
# 用法：
#   bash scripts/check-component-colors.sh          # 检查
#   bash scripts/check-component-colors.sh --list   # 列出全部裸色值（定位用）
set -uo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

LIST_ONLY=0
[ "${1:-}" = "--list" ] && LIST_ONLY=1

# 裸色值：前面不是 var(--…) 的逗号/括号上下文。
# 用 python 判上下文比 sed 可靠（要判断"是否处在 var( 的参数里"）。
python3 - "$LIST_ONLY" <<'PY'
import pathlib, re, sys

root = pathlib.Path('internal/builder/components')
list_only = sys.argv[1] == '1'

# 色值形态：#rgb / #rrggbb / #rrggbbaa
COLOR = re.compile(r'#[0-9a-fA-F]{3,8}\b')

def is_fallback(src: str, pos: int) -> bool:
    """判断这个色值是否处在 var(--token, <fallback>) 的兜底位置。

    做法：向前找最近的未闭合 "var(" —— 若找到且其第一个参数以 -- 开头，
    这个色值就是兜底值。
    """
    depth = 0
    i = pos - 1
    while i >= 0:
        c = src[i]
        if c == ')':
            depth += 1
        elif c == '(':
            if depth == 0:
                # 找到包裹开括号，回看函数名
                j = i - 1
                while j >= 0 and (src[j].isalnum() or src[j] in '-_'):
                    j -= 1
                fn = src[j+1:i]
                if fn == 'var':
                    # 取 var( 之后的第一个参数
                    rest = src[i+1:pos]
                    first = rest.split(',')[0].strip()
                    return first.startswith('--')
                return False
            depth -= 1
        i -= 1
    return False

violations = []
total_naked = 0
total_fallback = 0
for f in sorted(root.glob('*/*.css')):
    src = f.read_text()
    # 跳过注释（注释里的示例色值不算）
    lines = src.split('\n')
    for lineno, line in enumerate(lines, 1):
        stripped = line.strip()
        if stripped.startswith('/*') or stripped.startswith('*') or stripped.startswith('//'):
            continue
        line_start = sum(len(l) + 1 for l in lines[:lineno-1])
        for m in COLOR.finditer(src, line_start, line_start + len(line)):
            if is_fallback(src, m.start()):
                total_fallback += 1
                continue
            total_naked += 1
            violations.append(f'{f}:{lineno}  {m.group(0)}')

if list_only:
    print('\n'.join(violations))
    print(f'\n裸色值 {total_naked} 处 / 令牌兜底 {total_fallback} 处', file=sys.stderr)
    sys.exit(0)

if violations:
    print(f'✗ 组件 CSS 里有 {total_naked} 处裸色值（应写成 var(--sky-c-*, #兜底)）：', file=sys.stderr)
    for v in violations[:40]:
        print('    ' + v, file=sys.stderr)
    if len(violations) > 40:
        print(f'    …另有 {len(violations)-40} 处', file=sys.stderr)
    print('', file=sys.stderr)
    print('  正确的写法是「令牌 + 兜底」：var(--sky-c-primary, #2563eb)', file=sys.stderr)
    print('  兜底不能省 —— 无主题页面（纯静态产物）里 --sky-c-* 没有定义。', file=sys.stderr)
    sys.exit(1)

print(f'✓ 组件 CSS 无裸色值（令牌兜底 {total_fallback} 处，均为正确形态）')
PY
