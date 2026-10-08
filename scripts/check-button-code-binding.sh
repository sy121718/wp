#!/usr/bin/env bash
# check-button-code-binding.sh — 模板的「按钮码」必须能在菜单表里找到承载节点（docs/02-Z §5 门禁 3/4）。
#
# 背景：模板里的按钮显隐过去写权限码（`isset(.PermSet["order:create"])`），
# 现在写**按钮码**（`isset(.Buttons["order.create"])`）—— 按钮码是 type=3 菜单节点的
# title_key（迁移 589）。这一层间接换来的是「模板只出现菜单体系的词汇」，代价是**多了一个
# 可以静默失效的地方**：模板引用的按钮码在库里没有节点 → 那个按钮永远不显示，
# 不报错、日志干净，只是点了没反应。
#
# 判据两条，都在这个脚本里（同一件事的两面）：
#   A（需数据库）：模板里每个 `Buttons["<码>"]` 都必须在 sys_menus 里有对应的 type=3 节点
#      （deleted_at IS NULL、title_key = 该码、status = 1）。
#   B（纯文本）：模板里不得出现 `PermSet[`（旧词汇已退役）与 `Buttons["a:b"]`
#      （把权限码写进按钮码的位置 —— 它看着像能用，其实永远查不到）。
#
# 两个方向都**先剔注释**：注释里举例说明写法是正常的（partials/toolbar_create.html 的
# 文档注释里就写着 `Buttons["<按钮码>"]`）。不剔注释会把举例当成真实引用，判据立刻变成噪音。
#
# 用法：bash scripts/check-button-code-binding.sh
# 依赖：本地数据库可连（配置从 config.yaml 的 database 段读）、psql 可用。
# 退出码：0 = 绑定完整；1 = 有缺口或环境不满足。
set -euo pipefail
cd "$(dirname "$0")/.."

status=0

# —— 方向 B：纯文本扫描（不需要数据库）——
echo "→ 扫描模板里的按钮码写法…"
bad_text=$(python3 scripts/button-code-scan.py text || true)
if [ -n "$bad_text" ]; then
    echo
    echo "✗ 模板里出现了退役写法或权限码形态的按钮码："
    echo "$bad_text" | sed 's/^/    /'
    echo
    echo "  按钮码必须是菜单节点的 title_key（形如 order.create），不是权限码（order:create）；"
    echo "  按钮显隐统一写 isset(.Buttons[\"<按钮码>\"])，不再有 PermSet。"
    status=1
fi

# —— 方向 A：模板按钮码 ↔ type=3 节点 ——
if ! command -v psql >/dev/null 2>&1; then
    echo "缺少 psql，无法比对按钮码与菜单节点。" >&2
    exit 1
fi

yaml_db() {
    awk -v key="$1" '
        /^database:/ { in_db = 1; next }
        /^[a-zA-Z]/ { in_db = 0 }
        in_db && $1 == key":" { print $2; exit }
    ' config.yaml
}
DB_HOST="${DB_HOST:-${GOWP_DATABASE_HOST:-$(yaml_db host)}}"
DB_PORT="${DB_PORT:-${GOWP_DATABASE_PORT:-$(yaml_db port)}}"
DB_USER="${DB_USER:-${GOWP_DATABASE_USER:-$(yaml_db user)}}"
DB_PASS="${DB_PASSWORD:-${GOWP_DATABASE_PASSWORD:-$(yaml_db password)}}"
DB_NAME="${DB_NAME:-${GOWP_DATABASE_DBNAME:-$(yaml_db dbname)}}"

tmp_tpl=$(mktemp); tmp_nodes=$(mktemp); tmp_gap=$(mktemp)
trap 'rm -f "$tmp_tpl" "$tmp_nodes" "$tmp_gap"' EXIT

python3 scripts/button-code-scan.py codes > "$tmp_tpl"

PGPASSWORD="$DB_PASS" psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -tAc \
    "SELECT title_key FROM sys_menus WHERE deleted_at IS NULL AND type = 3 AND status = 1 AND title_key <> '';" \
    | sed "s/^[[:space:]]*//;s/[[:space:]]*$//" | grep -v "^$" | sort -u > "$tmp_nodes"

comm -23 "$tmp_tpl" "$tmp_nodes" > "$tmp_gap" || true
echo "  模板按钮码 $(wc -l < "$tmp_tpl") 个 / 菜单节点 $(wc -l < "$tmp_nodes") 个"

if [ -s "$tmp_gap" ]; then
    echo
    echo "✗ 以下按钮码在菜单表里没有承载节点 —— 对应的按钮永远不显示（无报错）："
    sed 's/^/    /' "$tmp_gap"
    echo
    echo "  修法：给这个权限点补一个 type=3 节点（title_key = 该按钮码、绑定对应权限码），"
    echo "  样板见 public/migrations/589_menu_button_code.sql。"
    status=1
fi

if [ "$status" -eq 0 ]; then
    echo "✓ 模板按钮码与菜单节点绑定完整"
fi
exit "$status"
