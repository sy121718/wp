#!/usr/bin/env bash
# check-menu-page-binding.sh — 菜单的 path 必须是**真实存在的页面路由**（docs/02-Z §5 门禁 2）。
#
# 为什么需要它：菜单的 path 就是浏览器地址栏那条 URL（侧栏直接 `<a href>`），而它是**数据**
# —— 运营在「菜单管理」里改得了。改错了不会报错：侧栏多一个点进去 404 的入口，或者某个
# 页面的高亮整组消失。实测（2026-10-07）库里有三条隐藏菜单指向根本不存在的页面
# （/project、/artifact、/publication），其中两条的 path 是 Vue 时代的残留。
#
# 判据（单向）：每条 path 非空的菜单行，`GET <path>` 必须出现在**运行时装配出来的路由表**里。
# 不反向要求「每条页面路由都有菜单」—— 大量子页面（编辑页、抽屉、面板）按设计不占菜单项。
#
# 两个刻意的取舍：
#   · **只查 GET**：菜单点击是导航，POST 页面不在菜单里出现；
#   · **path 为空的行放过**：那是「能力节点」—— 承载一个可授权的能力、没有页面入口
#     （样板：迁移 588 清空的 /project、/artifact、/publication）。删掉它们会让权限码在
#     授权界面上勾不到，而清掉假的 path 之后它们仍然承载授权。
#     · 菜单 95「重定向管理」的 path 是 /api/page/redirect —— 它确实是 GET 路由
#       （页面挂在 /api 前缀下的既有取舍），本判据放行。
#
# 做法与 check-permission-gaps.sh 同源：拿**运行时装配出来的路由表**比对，不是 grep 源码猜路径。
#
# 用法：bash scripts/check-menu-page-binding.sh
# 依赖：本地数据库可连（配置从 config.yaml 的 database 段读）、psql 可用。
# 退出码：0 = 全部命中；1 = 有死链或环境不满足。
set -euo pipefail
cd "$(dirname "$0")/.."

if ! command -v psql >/dev/null 2>&1; then
    echo "缺少 psql，无法比对菜单与路由。" >&2
    exit 1
fi

yaml_db() {
    awk -v key="$1" '
        /^database:/ { in_db = 1; next }
        /^[a-zA-Z]/ { in_db = 0 }
        in_db && $1 == key":" { print $2; exit }
    ' config.yaml
}

# 优先级：显式 DB_* 覆盖 → 应用的环境变量 → config.yaml（与 check-permission-gaps.sh 同规则）。
DB_HOST="${DB_HOST:-${GOWP_DATABASE_HOST:-$(yaml_db host)}}"
DB_PORT="${DB_PORT:-${GOWP_DATABASE_PORT:-$(yaml_db port)}}"
DB_USER="${DB_USER:-${GOWP_DATABASE_USER:-$(yaml_db user)}}"
DB_PASS="${DB_PASSWORD:-${GOWP_DATABASE_PASSWORD:-$(yaml_db password)}}"
DB_NAME="${DB_NAME:-${GOWP_DATABASE_DBNAME:-$(yaml_db dbname)}}"

tmp_raw=$(mktemp); tmp_gets=$(mktemp); tmp_menus=$(mktemp); tmp_dead=$(mktemp)
trap 'rm -f "$tmp_raw" "$tmp_gets" "$tmp_menus" "$tmp_dead"' EXIT

echo "→ 装配路由表（会初始化一次组件，需要数据库 ${DB_HOST}:${DB_PORT}/${DB_NAME}）…"
WP_DUMP_ROUTES=1 go test ./internal/routers/ -run TestDumpRoutesForAudit -count=1 -v 2>/dev/null > "$tmp_raw"
grep -E "^GET /" "$tmp_raw" | awk '{print $2}' | sort -u > "$tmp_gets"
if [ ! -s "$tmp_gets" ]; then
    echo "路由表为空 —— 组件装配失败（先确认数据库可连、config.yaml 正确）。" >&2
    exit 1
fi

PGPASSWORD="$DB_PASS" psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -tAc \
    "SELECT path FROM sys_menus WHERE deleted_at IS NULL AND path <> '';" \
    | sed "s/^[[:space:]]*//;s/[[:space:]]*$//" | grep -v "^$" | sort -u > "$tmp_menus"

comm -23 "$tmp_menus" "$tmp_gets" > "$tmp_dead" || true

echo "  菜单 path $(wc -l < "$tmp_menus") 条 / GET 页面路由 $(wc -l < "$tmp_gets") 条"

if [ -s "$tmp_dead" ]; then
    echo
    echo "✗ 以下菜单 path 不在运行时路由表里 —— 点进去是 404（侧栏里看不出来）："
    while IFS= read -r p; do
        rows=$(PGPASSWORD="$DB_PASS" psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -tAc \
            "SELECT string_agg(id || ':' || title, ', ') FROM sys_menus WHERE deleted_at IS NULL AND path = '$p';")
        echo "    $p   （菜单 $rows）"
    done < "$tmp_dead"
    echo
    echo "  修法二选一："
    echo "    · 菜单指的是页面 → 把 path 改成真实存在的页面路由；"
    echo "    · 菜单只是某个能力的承载节点（没有页面入口）→ 把 path 清空，"
    echo "      并在迁移里写明它承载哪条权限码（样板：public/migrations/588_menu_dead_path_clear.sql）。"
    exit 1
fi

echo "✓ 菜单 path 全部命中真实页面路由"
