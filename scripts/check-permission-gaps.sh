#!/usr/bin/env bash
# check-permission-gaps.sh — 审计「有路由、但没有对应权限点」的接口。
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
    "GET /api/admin/routes"
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
trap 'rm -f "$tmp_routes" "$tmp_raw" "$tmp_perms" "$tmp_exempt" "$tmp_gap" "$tmp_declared" "$tmp_codes" "$tmp_perm_codes" "$tmp_undeclared"' EXIT

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

# 兜底 A（审计 SEC-011）：声明式注册采集出来的路由，必须与运行时路由表逐条对上。
# 这一条理论上不可能失败（声明是注册动作的产物），留在这里是为了让「机制悄悄失效」
# （例如有人绕过 RouteGroup 直接往授权组上挂 gin 的 GET/POST）当场暴露。
if [ -s "$tmp_declared" ]; then
    comm -23 "$tmp_declared" "$tmp_routes" > "$tmp_undeclared" || true
    if [ -s "$tmp_undeclared" ]; then
        echo "✗ 声明了权限点、但运行时路由表里不存在的接口（声明式注册被绕过或被改写）：" >&2
        sed "s/^/    /" "$tmp_undeclared" >&2
        exit 1
    fi
fi
comm -23 "$tmp_routes" "$tmp_perms" | comm -23 - "$tmp_exempt" > "$tmp_gap" || true

echo "  路由 $(wc -l < "$tmp_routes") 条 / 权限点 $(wc -l < "$tmp_perms") 条 / 已豁免 $(wc -l < "$tmp_exempt") 条"

if [ -s "$tmp_gap" ]; then
    echo
    echo "✗ 以下接口挂了 Casbin 但没有权限点 —— 任何账号（含超管）调用都会被拒 403："
    sed "s/^/    /" "$tmp_gap"
    echo
    echo "  修法：新增一支 seed 迁移，插入 sys_permission 条目 + 对应超管策略"
    echo "  （样板见 public/migrations/079_content_collections_permission.sql 与 151）。"
    exit 1
fi

echo "✓ 没有缺口"

# —— 兜底 B：库里有、代码没声明的权限点（信息级，不影响退出码）——
# 声明式注册接管的是「路由 → 权限点」这一侧；库里多出来的条目要么是历史 seed 的残留，
# 要么是页面路由 / CasbinMiddlewareForPath 按指定路径 enforce 时用的权限点 —— 保留不动，
# 但把清单摆出来，双轨期的漂移（代码里删了权限点、库里还在）才看得见。
if [ -s "$tmp_codes" ]; then
    PGPASSWORD="$DB_PASS" psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -tAc \
        "SELECT permission_code FROM sys_permission WHERE status = 1 AND api_path <> '';" \
        | sed "s/^[[:space:]]*//;s/[[:space:]]*$//" | grep -v "^$" | sort -u > "$tmp_perm_codes"
    echo
    echo "· 声明式注册覆盖 $(wc -l < "$tmp_codes") 条权限点"
    # 运行时豁免（挂在 Casbin 组下但显式不声明权限点）与脚本上面的 EXEMPT 名单是**两份**：
    # 这里的几条是代码里显式写 permission.Exempt 的路由；EXEMPT 里还多出几条根本不经过
    # 声明式注册的公开路由（如 /api/captcha，它挂在没有 Casbin 的组上）。两份都摆出来便于核对。
    if grep -qE "^EXEMPT " "$tmp_raw"; then
        echo "· 代码中显式豁免的路由（permission.Exempt）："
        grep -E "^EXEMPT " "$tmp_raw" | awk '{print "    "$2" "$3}' | sort -u
    fi
    comm -23 "$tmp_perm_codes" "$tmp_codes" > "$tmp_undeclared" || true
    if [ -s "$tmp_undeclared" ]; then
        echo "· 库中存在但代码未声明的权限点（历史 seed 或页面路由入口，保留不动）："
        sed "s/^/    /" "$tmp_undeclared"
    fi
else
    echo "· 未采集到声明清单（装配输出里没有 DECLARED 行）—— 声明式注册可能未生效，请检查 internal/permission 的接入"
fi
