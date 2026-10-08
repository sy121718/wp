#!/usr/bin/env bash
# check-template-presentation.sh — 模板只给语义，不给外观（docs/rules/template-boundary.md）。
#
# 为什么需要它：Jet 在服务端执行，客户端拿不到模板源码、也拿不到 data —— 所以模板里
# 「判断一下用什么颜色」看起来无害。但它是**业务规则**（`DeletedCount != 0` 就是危险、
# `EmailVerified` 就是绿），写在模板里有两个代价：①没法单测；②同一阈值在多个列表页
# 各抄一遍，真源分裂。正确形态是 Go 出**语义枚举**（tone：ok/warn/danger/mute/info），
# 模板把语义写进 class，CSS 决定语义长什么样（换肤/暗色只改 CSS）。
#
# 判据（都是纯文本可判定的）：
#   1. `style="` 里出现**颜色字面量**（#hex / rgb( / rgba( / hsl(）—— 外观值进模板；
#      用调色板变量（`var(--x)`）不算违规：那是引用语义变量，不是写死颜色。
#   2. `{{if …}}` 直接产出 badge 语义类（badge-danger/success/warning/mute/info）
#      —— 业务阈值进模板（`{{if .EmailVerified}}badge-success{{end}}` 是规则，不是渲染）。
#   3. `style="` 里**做算术**（`{{… + …}}` / 三元 / `calc({{…}} * …)`）—— 布局数值在模板里算
#      （`padding-left:{{8 + (depth > 3 ? 36 : depth * 12)}}px`、`calc({{rowDepth}} * 1.5rem)`），
#      应改成把 Go 算好的值放进 CSS 变量（`style="--depth:{{.Depth}}"` + CSS `calc()`）。
#      只把**值**塞进 `style` 不算违规（`padding-left:{{r.PadLeft}}px`、`var(--chart-c{{s.Color}})`
#      —— 那里模板没做计算，值来自 Go）。
#
# 豁免：scripts/template-presentation-allow.txt，**按文件**登记（行号会漂移，文件级才稳）。
# 条目**不再命中即失败** —— 收敛完一个文件就必须回来删掉那一行，清单只会变短。
#
# 用法：bash scripts/check-template-presentation.sh
# 依赖：无（纯文本扫描，不连数据库）。退出码：0 = 通过；1 = 有新增违规或过期豁免。
set -euo pipefail
cd "$(dirname "$0")/.."

TEMPLATES_DIR="internal/templates"
ALLOW_FILE="scripts/template-presentation-allow.txt"

if [ ! -d "$TEMPLATES_DIR" ]; then
    echo "找不到 ${TEMPLATES_DIR} —— 模板目录改名了？" >&2
    exit 1
fi

tmp_allow=$(mktemp); tmp_used=$(mktemp); tmp_hits=$(mktemp)
trap 'rm -f "$tmp_allow" "$tmp_used" "$tmp_hits"' EXIT

if [ -f "$ALLOW_FILE" ]; then
    { grep -v '^[[:space:]]*#' "$ALLOW_FILE" || true; } \
        | { grep -v '^[[:space:]]*$' || true; } \
        | awk '{print $1}' | sort -u > "$tmp_allow"
else
    : > "$tmp_allow"
fi

: > "$tmp_hits"
collect() { # $1 = 判据说明（进错误信息）；$2 = 正则
    grep -rnE "$2" --include='*.html' "$TEMPLATES_DIR" 2>/dev/null >> "$tmp_hits" || true
}

collect "颜色字面量"   'style="[^"]*(#[0-9a-fA-F]{3,8}|rgb\(|rgba\(|hsl\()'
collect "阈值选语义类" '\{\{if [^}]{0,90}\}\} ?badge-(danger|success|warning|mute|info)'
collect "布局数值"     'style="[^"]*(\{\{[^}]{0,90}[+*?]|calc\([^)]*\{\{)'

new_violations=0
if [ -s "$tmp_hits" ]; then
    while IFS= read -r hit; do
        rel="${hit#${TEMPLATES_DIR}/}"
        file="${rel%%:*}"
        if grep -qxF "$file" "$tmp_allow"; then
            echo "$file" >> "$tmp_used"
            continue
        fi
        echo "✗ $rel" >&2
        new_violations=$((new_violations + 1))
    done < <(sort -u "$tmp_hits")
fi

if [ "$new_violations" -gt 0 ]; then
    echo >&2
    echo "模板表现层门禁：${new_violations} 处违规。" >&2
    echo "  外观归 CSS、语义归 Go：见 docs/rules/template-boundary.md。" >&2
    echo "  存量（尚未收敛的）按文件登记在 ${ALLOW_FILE}，新增违规不豁免。" >&2
    exit 1
fi

stale=0
if [ -s "$tmp_allow" ]; then
    while IFS= read -r entry; do
        [ -n "$entry" ] || continue
        if ! grep -qxF "$entry" "$tmp_used" 2>/dev/null; then
            echo "✗ 豁免条目 ${entry} 已不再命中 —— 该文件已收敛干净，请从 ${ALLOW_FILE} 删掉这一行。" >&2
            stale=$((stale + 1))
        fi
    done < "$tmp_allow"
fi

if [ "$stale" -gt 0 ]; then
    exit 1
fi

echo "模板表现层：无新增违规（存量豁免 $(wc -l < "$tmp_allow" | tr -d ' ') 个文件，随收敛递减）。"
