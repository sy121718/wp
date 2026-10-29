#!/usr/bin/env bash
# check-template-presentation.sh — 模板做展示判断，不写字面外观（AGENTS.md）。
#
# 口径（2026-10-08 更新）：**Go 给业务事实与权限事实，Jet 决定展示状态（含 tone 语义档），
# CSS 决定外观**。展示判断归前端，所以「模板按事实选 tone」是**合法形态**，本脚本不把它
# 当违规：旧判据「`{{if …}}` 后紧跟 badge-<类名>」已被删除 —— 它只认一种写法
# （直出分支），却漏掉同样合法的变量赋值形态（`{{tone := "mute"}}{{if …}}{{tone = "ok"}}{{end}}`
# + `class="badge badge-{{tone}}"`），等于把「写法」当成「越界」来判。
#
# 脚本要守的是**外观不落进模板**与**档位不越表**这四件事（都纯文本可判）：
#   1. `style="` 里出现颜色字面量（#hex / rgb( / rgba( / hsl(）—— 外观值进模板；
#      用调色板变量（`var(--x)`）不算违规：那是引用语义变量，不是写死颜色；
#      带兜底值的变量（`var(--x, #fff)`）仍算违规 —— 兜底链放在 CSS 的变量定义处
#      （theme.css 的 `--sky-c-surface: var(--c-surface, #fff)` 那样）。
#   2. `style="` 里做算术（`{{… + …}}` / 三元 / `calc({{…}} * …)`）—— 布局数值在模板里算，
#      应改成把 Go 算好的值放进 CSS 变量（`style="--depth:{{.Depth}}"` + CSS `calc()`）。
#      只把**值**塞进 `style` 不算违规（`padding-left:{{r.PadLeft}}px`、`var(--chart-c{{s.Color}})`
#      —— 那里模板没做计算，值来自 Go）。
#   3. 写死的调色板类名（`badge-red` / `text-gray-400` / `bg-blue-50` …）—— 颜色必须经
#      `.badge-<tone>` 别名与 CSS 变量走，否则换肤/暗色改不动；语义档 `badge-ok` / `badge-warn`
#      与既有别名 `badge-success` / `badge-warning` 不受此判据限制。
#   4. 模板产出的 tone 档**越表**：某个 `badge-{{name}}` 用的模板变量（小写开头 → 局部变量，
#      Go 字段是 `{{.XxxTone}}` / `{{r.StatusTone}}`，不在本判据范围内）在本文件里被赋了
#      ok/warn/danger/mute/info 之外的字面量。自造档（"red" / "error" / "badge-danger"）没有
#      对应的 `.badge-<tone>` 别名，表现是**静默失色**而不是报错 —— 只在真机上才看得见。
#      只检查「本文件内能读到字面量赋值」的变量：include/yield 传参进来的档不在此处判定（不误报）。
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
# tone 词表（语义档）：定义在 internal/templates/static/css/ui.css 的 `.badge-<tone>` 别名。
TONE_VOCAB="ok|warn|danger|mute|info"

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
collect "布局数值"     'style="[^"]*(\{\{[^}]{0,90}[+*?]|calc\([^)]*\{\{)'
collect "调色板类名"   '\b(badge|tag|chip|dot|text|bg|border|fill|stroke|ring|shadow)-(red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose|slate|gray|grey|zinc|neutral|stone|black|white)(-[0-9]{2,3})?\b'

# 判据 4：模板里的 tone 变量只能取词表内的档。
collect_tone_vocab() {
    local file var hitline val
    while IFS= read -r file; do
        # 只认「小写开头、不含点」的模板局部变量：Go 字段（{{.XxxTone}} / {{r.StatusTone}}）不看。
        while IFS= read -r var; do
            [ -n "$var" ] || continue
            # 采集该变量在本文件内的字面量赋值：`{{x := "…"}}`（初始化）与 `{{x = "…"}}`（分支内改写）都算。
            # `\b` 前缀是为了不把 `tabTone` 当成 `tone`；先切出 `{{…}}` 表达式再取值，
            # 免得把同一行里的 HTML 属性（`class="badge badge-{{tone}}"`）当成赋值。
            while IFS= read -r hitline; do
                while IFS= read -r val; do
                    # 空串 = 不带档；其余必须落在词表内（词表只有一份，见文件头的 TONE_VOCAB）。
                    if [ -n "$val" ] && ! printf '%s' "$val" | grep -qE "^(${TONE_VOCAB})$"; then
                        printf '%s\n' "$hitline" >> "$tmp_hits"
                    fi
                done < <(printf '%s\n' "$hitline" \
                    | grep -oE '\{\{[^}]*\}\}' \
                    | grep -oE "\\b${var}[[:space:]]*:?=[[:space:]]*\"[^\"]*\"" \
                    | sed -E 's/^[^"]*"([^"]*)"$/\1/' || true)
            done < <(grep -nHE "\{\{[^}]*\b${var}[[:space:]]*:?=[[:space:]]*\"" "$file" || true)
        done < <(grep -oE 'badge-\{\{[a-z][A-Za-z0-9_]*\}\}' "$file" 2>/dev/null \
            | sed -E 's/.*\{\{([a-z][A-Za-z0-9_]*)\}\}.*/\1/' | sort -u)
    done < <(grep -rlE 'badge-\{\{[a-z]' --include='*.html' "$TEMPLATES_DIR" 2>/dev/null || true)
}
collect_tone_vocab

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
    echo "  Go 给业务事实、Jet 决定展示状态、CSS 决定外观：见 AGENTS.md。" >&2
    echo "  上面每一条的判据说明见该文档 §语义 vs 外观（三条禁令）与 §门禁。" >&2
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

echo "模板表现层：无新增违规（tone 词表 ${TONE_VOCAB}；存量豁免 $(wc -l < "$tmp_allow" | tr -d ' ') 个文件，随收敛递减）。"
