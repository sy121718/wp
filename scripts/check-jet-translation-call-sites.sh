#!/usr/bin/env bash
# check-jet-translation-call-sites.sh — 禁止「赋值右侧直调取词函数」这一 Jet 写法。
#
# 为什么需要它：取词 .["t"] 的三种形态里，只有这一种是**语法层**被拒的（实测原文见
# docs/rules/jet-template-contract.md §4.3，用例 TestJetContractAssignmentFormDependsOnChannel）：
#
#     {{x := .["t"]("k", "兜底")}}     →  Jet Runtime Error: unexpected node type .["t"] in unary expression evaluating
#     {{ .["t"]("k", "兜底") }}        →  正常（直调形态，现网 3462 处）
#     {{tr := .["t"]}}{{x := tr(…)}}   →  正常（声明后复用，现网 116 处声明 + 1985 处调用）
#
# 为什么值得一条脚本：它和合法形态**只差一个左括号的位置**，写错了不报编译错、模板测试也照过，
# 翻车发生在渲染期 —— 整页走 renderError → http.Error(500)（半截内容被丢弃），htmx 片段更糟：
# 5xx 不 swap，页面上**毫无反应**（internal/templates/CLAUDE.md 第 15-17 行已记）。现网 0 处，
# 属「终局零容忍」型检查：防的是下一个照抄注释里那行写法的代理。
#
# 判据（只针对「赋值右侧带实参」这一形态，按形状判、不看变量名）：
#     <标识符> := .["t"](…)   /   <标识符> := .["tf"](…)
# 允许：{{tr := .["t"]}}（不带调用的赋值）、{{ .["t"](…) }}（直调）、map("t", .["t"], …)、
#       {{if isset(.["t"])}} 之类的非赋值用法。
#
# 与 docs/rules/jet-template-contract.md §6.2.1 的同源正则相比只多一步：**先剥掉 Jet 注释
# {* … *}**（换成等长空白、保留换行，行号仍与原文一一对应）。注释里的字面量不是调用，
# 照报是假红 —— 教训来自 check-empty-state-table-head.sh：某模板注释里写了「表头在
# {{range}} 里提不出来」，被当成真 token 压栈，于是同文件的真空态被误报、另一个文件反而漏报。
#
# 用法：bash scripts/check-jet-translation-call-sites.sh
#       退出码 0 = 干净；1 = 有违规（明细已打印）；2 = 模板目录不存在
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TPL_DIR="$ROOT/internal/templates"

if [ ! -d "$TPL_DIR" ]; then
  echo "没有找到模板目录 internal/templates：目录结构变了？" >&2
  exit 2
fi

python3 - "$TPL_DIR" "$ROOT" <<'PYEOF'
import pathlib
import re
import sys

tpl_dir = pathlib.Path(sys.argv[1])
root = pathlib.Path(sys.argv[2])

# Jet 注释 {* … *}（不支持嵌套，取到第一个 *}）。
COMMENT = re.compile(r'\{\*.*?\*}', re.S)
# 赋值右侧直调：:= 之后紧接 .["t"] / .["tf"] 再带实参（跨行也命中，故不按行扫）。
BAD = re.compile(r':\s*=\s*\.[ \t]*\["t(?:f)?"\][ \t]*\(')


def mask_comments(text):
    """把注释换成等长空白（保留换行），行号与原文一一对应。"""
    return COMMENT.sub(lambda m: re.sub(r'[^\n]', ' ', m.group(0)), text)


def line_of(text, offset):
    return text.count('\n', 0, offset) + 1


files = sorted(tpl_dir.rglob('*.html'))
hits = []
for path in files:
    masked = mask_comments(path.read_text(encoding='utf-8'))
    for m in BAD.finditer(masked):
        hits.append((path.relative_to(root), line_of(masked, m.start()), m.group(0)))

if hits:
    print('✗ Jet 取词调用形态违规 %d 处（扫描 %d 个模板）' % (len(hits), len(files)))
    for rel, no, frag in hits:
        print('  %s:%d  ←  %s' % (rel, no, frag))
        print('    原因：赋值右侧直接调用 .["t"](…) / .["tf"](…) 会被 Jet 拒绝：')
        print('         Jet Runtime Error: unexpected node type .["t"] in unary expression evaluating')
        print('    改法：先 {{tr := .["t"]}} 再 tr("key", "兜底")；或去掉赋值、直接写 {{ .["t"](…) }}。')
    sys.exit(1)

print('✓ Jet 取词赋值形态合规（扫描 %d 个模板，0 处违规）' % len(files))
PYEOF
