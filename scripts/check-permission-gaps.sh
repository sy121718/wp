#!/usr/bin/env bash
# check-permission-gaps.sh — 审计「路由表 ↔ 权限点表」双向同源（docs/02-Y-permission-source-of-truth.md）。
#
# 判据两个方向都要过：
#   A. 每条 /api/* 路由都要有权限点 —— 否则那条路由挂了 Casbin 却没有任何策略能匹配，
#      **含超管在内全员 403**（072/077/078/079/151 各踩过一次）；
#   B. 每条权限点的 api_path 都要在路由表里 —— 否则授权界面上勾了它也没用（那个端点不存在），
#      而它看起来完全正常。
#
# 两个方向都是**硬失败**：权限点与路由漂移的两种表现都必须在 CI 就拦住。
#
# 为什么需要这条命令：authorizedAPI 组统一挂了 CasbinMiddleware()，它按**实际请求路径**
# enforce。权限点表里没有对应条目 → 没有任何策略能匹配 → **含超管在内全员 403**，
# 表现是「接口明明在，点一下就是无权限」。这个坑在 072/077/078/079 的迁移注释里各写过
# 一遍，151 又补了 page:delete 与 block:clone 两条。
#
# 做法：把**运行时装配出来的路由表**与 sys_permission 比对（不是 grep 源码猜路径 ——
# 那样会漏掉拼接出来的前缀与 Group 嵌套）。
#
# 用法：bash scripts/check-permission-gaps.sh
# 依赖：本地数据库可连（配置从 config.yaml 的 database 段读）、psql 可用。
# 退出码：0 = 无缺口；1 = 有缺口或环境不满足。
set -euo pipefail
cd "$(dirname "$0")/.."

# 有意豁免的接口：必须匿名可达（登录、验证码），或登录后人人可用的自用接口。
# 往这里加之前先回答一句：这个接口被任意登录用户随便调，会不会出事？
EXEMPT=(
    "GET /api/admin/profile"
    "GET /api/captcha"
    "POST /api/admin/login"
    "POST /api/admin/logout"
)

if ! command -v psql >/dev/null 2>&1; then
    echo "缺少 psql，无法比对权限点。" >&2
    exit 1
fi

# 从 config.yaml 的 database 段取值（脚本里不另存一份口令）。
yaml_db() {
    awk -v key="$1" '
        /^database:/ { in_db = 1; next }
        /^[a-zA-Z]/ { in_db = 0 }
        in_db && $1 == key":" { print $2; exit }
    ' config.yaml
}

# 优先级：显式 DB_* 覆盖 → 应用的环境变量（config.envBindableKeys）→ config.yaml。
# 中间那一层是必须的：CI 用 GOWP_DATABASE_PASSWORD 给**应用**注入口令，而 config.yaml 在
# CI 里是从 config.yaml.example 复制来的 —— 只读 yaml 会拿到 example 的默认口令，与实际
# 服务容器的 POSTGRES_PASSWORD 不符，表现为 psql 认证失败（2026-09-16 踩过这次）。
DB_HOST="${DB_HOST:-${GOWP_DATABASE_HOST:-$(yaml_db host)}}"
DB_PORT="${DB_PORT:-${GOWP_DATABASE_PORT:-$(yaml_db port)}}"
DB_USER="${DB_USER:-${GOWP_DATABASE_USER:-$(yaml_db user)}}"
DB_PASS="${DB_PASSWORD:-${GOWP_DATABASE_PASSWORD:-$(yaml_db password)}}"
DB_NAME="${DB_NAME:-${GOWP_DATABASE_DBNAME:-$(yaml_db dbname)}}"

tmp_routes=$(mktemp); tmp_raw=$(mktemp); tmp_perms=$(mktemp); tmp_exempt=$(mktemp); tmp_gap=$(mktemp)
tmp_declared=$(mktemp); tmp_codes=$(mktemp); tmp_perm_codes=$(mktemp); tmp_undeclared=$(mktemp)
tmp_all_routes=$(mktemp); tmp_reachable=$(mktemp)
trap 'rm -f "$tmp_routes" "$tmp_raw" "$tmp_perms" "$tmp_exempt" "$tmp_gap" "$tmp_declared" "$tmp_codes" "$tmp_perm_codes" "$tmp_undeclared" "$tmp_all_routes" "$tmp_reachable"' EXIT

echo "→ 装配路由表（会初始化一次组件，需要数据库 ${DB_HOST}:${DB_PORT}/${DB_NAME}）…"
# 装配输出一次拿全：路由行（"METHOD /path"）与声明行（"DECLARED METHOD /path code" /
# "EXEMPT METHOD /path"）都在这里，后者由内部/permission 的声明注册表在注册时采集。
WP_DUMP_ROUTES=1 go test ./internal/routers/ -run TestDumpRoutesForAudit -count=1 -v 2>/dev/null > "$tmp_raw"
grep -E "^[A-Z]+ /api" "$tmp_raw" | sort -u > "$tmp_routes"
grep -E "^DECLARED " "$tmp_raw" | awk '{print $2" "$3}' | sort -u > "$tmp_declared"
grep -E "^DECLARED " "$tmp_raw" | awk '{print $4}' | sort -u > "$tmp_codes"
if [ ! -s "$tmp_routes" ]; then
    echo "路由表为空 —— 组件装配失败（先确认数据库可连、config.yaml 正确）。" >&2
    exit 1
fi

PGPASSWORD="$DB_PASS" psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -tAc \
    "SELECT DISTINCT api_method || ' ' || api_path FROM sys_permission WHERE api_path <> '' AND status = 1;" \
    | sed "s/^[[:space:]]*//;s/[[:space:]]*$//" | grep -v "^$" | sort -u > "$tmp_perms"

printf '%s\n' "${EXEMPT[@]}" | sort -u > "$tmp_exempt"

comm -23 "$tmp_routes" "$tmp_perms" | comm -23 - "$tmp_exempt" > "$tmp_gap" || true

echo "  路由 $(wc -l < "$tmp_routes") 条 / 权限点 $(wc -l < "$tmp_perms") 条 / 已豁免 $(wc -l < "$tmp_exempt") 条"

if [ -s "$tmp_gap" ]; then
    echo
    echo "✗ 以下接口挂了 Casbin 但没有权限点 —— 任何账号（含超管）调用都会被拒 403："
    sed "s/^/    /" "$tmp_gap"
    echo
    echo "  修法：加一条权限点常量（internal/permission/codes.go）+ 在路由注册处声明（RouteGroup 的第二参），"
    echo "  装配末尾 permission.SyncToDB 会幂等 upsert 进 sys_permission 与超管策略（审计 SEC-011）—— 不再需要 seed 迁移。"
    echo "  （历史样板见 public/migrations/079_content_collections_permission.sql 与 151。）"
    exit 1
fi

echo "✓ 没有缺口"

# —— 方向 B：权限点行的 api_path 必须在运行时路由表里存在（硬失败）——
#
# 为什么必须硬失败：一条指向不存在端点的权限点看起来完全正常（授权界面列得出、勾得上），
# 但勾了之后那个功能照样打不开（端点不在），而排查的人会先去怀疑角色与策略 —— 方向全错。
# 实测来源：页面路由搬走后权限点还指着旧路径（mail 的联系人写动作）、
# 页面路径被当成权限点但页面组不声明（/admin/orders/overview）。
grep -E "^[A-Z]+ /" "$tmp_raw" | sort -u > "$tmp_all_routes"
# 判据是「运行时路由 ∪ 已声明的 Casbin 对象」：**页面借用的对象没有对应 API 路由**
# （如 /api/i18n/save 只是词条页的 Casbin obj、/api/seo/audit 是 seo-audit 路由额外校验的
# 第二条策略）—— 它们由 permission.Declare 在注册处显式声明，勾了确实有用，
# 所以不能只拿路由表比。缺了声明的那几条仍然会被抓住（declared 里没有它）。
cat "$tmp_all_routes" "$tmp_declared" | sort -u > "$tmp_reachable"
comm -23 "$tmp_perms" "$tmp_reachable" > "$tmp_undeclared" || true
if [ -s "$tmp_undeclared" ]; then
    echo
    echo "✗ 以下权限点指向的路径不在运行时路由表里 —— 授权界面勾了也没用（端点不存在）："
    sed "s/^/    /" "$tmp_undeclared"
    echo
    echo "  修法：路由改名/搬走后同步改 sys_permission.api_path 与 sys_casbin_rule.v1（一条迁移），"
    echo "  或者删掉这条权限点（功能已经下线）。"
    exit 1
fi

echo "✓ 权限点与路由双向同源"
