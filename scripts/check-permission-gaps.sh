#!/usr/bin/env bash
# check-permission-gaps.sh — 审计「路由表 ↔ 权限点表」双向同源（AGENTS.md）。
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

# 工作树可显式复用隔离审计配置；默认仍取仓库配置。
CONFIG_PATH="${GOWP_AUDIT_CONFIG:-config.yaml}"

# 对账只读运行时的精确方法与路径，路径前缀不作为豁免依据。
permission_gaps() {
    comm -23 "$1" "$2" | comm -23 - "$3"
}
self_gaps=$(permission_gaps \
    <(printf '%s\n' 'GET /api/site/known' 'GET /api/site/unknown' 'POST /api/site/known') \
    <(printf '') \
    <(printf '%s\n' 'GET /api/site/known'))
if [[ "$self_gaps" != $'GET /api/site/unknown\nPOST /api/site/known' ]]; then
    echo "权限缺口对账自检失败：未登记路径或方法被放过" >&2
    exit 1
fi

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
    ' "$CONFIG_PATH"
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
tmp_all_routes=$(mktemp); tmp_reachable=$(mktemp); tmp_auth_objs=$(mktemp)
tmp_undeclared_routes=$(mktemp); tmp_runtime_authed=$(mktemp); tmp_registered=$(mktemp)
tmp_perm_all=$(mktemp); tmp_disabled=$(mktemp); tmp_auth_objs_used=$(mktemp)
trap 'rm -f "$tmp_routes" "$tmp_raw" "$tmp_perms" "$tmp_exempt" "$tmp_gap" "$tmp_declared" "$tmp_codes" "$tmp_perm_codes" "$tmp_undeclared" "$tmp_all_routes" "$tmp_reachable" "$tmp_auth_objs" "$tmp_undeclared_routes" "$tmp_runtime_authed" "$tmp_registered" "$tmp_perm_all" "$tmp_disabled" "$tmp_auth_objs_used"' EXIT

echo "→ 装配路由表（会初始化一次组件，需要数据库 ${DB_HOST}:${DB_PORT}/${DB_NAME}）…"
# 审计装配**必须跳过 seed**：seed 会把被删掉的权限码补回来，
# 于是「目录里根本没有这条权限点」永远被自动修复掩盖，方向 A 变成永绿。
# 关掉 run_migrations 后，装配只读现有目录，漂移才可见。
WP_AUDIT_NO_SEED=1 WP_DUMP_ROUTES=1 go test ./internal/routers/ -run TestDumpRoutesForAudit -count=1 -v 2>/dev/null > "$tmp_raw"
# 装配输出一次拿全：路由行（"METHOD /path"）与声明行（"DECLARED METHOD /path code" /
# "EXEMPT METHOD /path"）都在这里，后者由内部/permission 的声明注册表在注册时采集。
# 真实入口按「已登记的 METHOD+PATH」取，不按 /api 前缀推断：页面与 HTML 写入口
# 同样可能带权限点，访客端点也可能不在 /api 下。
grep -E "^DECLARED " "$tmp_raw" | awk '{print $2" "$3}' | sort -u > "$tmp_routes"
# 声明行格式：DECLARED <method> <path> <perm> |<kind>|<authObject>|<authMethod>|<path>
# 真实入口 = METHOD+PATH；鉴权对象 = authMethod+authObject（页面/写入口可能复用别的操作）。
grep -E "^DECLARED " "$tmp_raw" | awk '{print $2" "$3}' | sort -u > "$tmp_declared"
# 行格式是 "DECLARED m p perm |kind|authObj|authMethod|path"：权限码是第 4 个空格字段，
# 后面紧跟的 "|kind|..." 是附加列。豁免声明的权限码为空，不属于受保护入口。
grep -E "^DECLARED " "$tmp_raw" | awk '{print $4}' | { grep -v '^|' || true; } | sort -u > "$tmp_codes"
grep -E "^DECLARED " "$tmp_raw" | awk -F'|' '{split($0,a," "); print a[2]" "$3}' | sort -u > "$tmp_auth_objs"
if [ ! -s "$tmp_routes" ]; then
    echo "路由表为空 —— 组件装配失败（先确认数据库可连、config.yaml 正确）。" >&2
    exit 1
fi

# 目录分两档读：
#   · 全部已登记码（含被管理员停用的）—— 用于判断「码是否存在」；
#   · 启用码 —— 仅用于信息展示与提示。
# 合法禁用不是目录损坏：管理员停用一个权限点后，入口仍注册、鉴权仍拒绝，
# 启动核对也放行（见 routers.verifyDeclaredPermissions）。脚本必须同一口径，
# 否则一次正常停用会让门禁变红，逼着人去改代码或恢复启动反写。
PGPASSWORD="$DB_PASS" psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -tAc \
    "SELECT DISTINCT permission_code FROM sys_permission;" \
    | sed "s/^[[:space:]]*//;s/[[:space:]]*$//" | { grep -v "^$" || true; } | sort -u > "$tmp_perm_all"
PGPASSWORD="$DB_PASS" psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -tAc \
    "SELECT DISTINCT permission_code FROM sys_permission WHERE status = 1;" \
    | sed "s/^[[:space:]]*//;s/[[:space:]]*$//" | { grep -v "^$" || true; } | sort -u > "$tmp_perm_codes"
# 受保护入口的声明权限码必须**存在于目录**（启用或禁用都算存在）：漏一条，
# 该入口挂 Casbin 后含超管在内全员 403。禁用是合法启停，不算缺口。
comm -23 "$tmp_codes" "$tmp_perm_all" > "$tmp_gap" || true
comm -23 "$tmp_codes" "$tmp_perm_codes" > "$tmp_disabled" || true

awk '/^EXEMPT / {print $2" "$3}' "$tmp_raw" | sort -u > "$tmp_exempt"


echo "  真实入口 $(wc -l < "$tmp_routes") 条 / 声明权限码 $(wc -l < "$tmp_codes") 条 / 启用目录 $(wc -l < "$tmp_perm_codes") 条 / 已豁免 $(wc -l < "$tmp_exempt") 条"

if [ -s "$tmp_gap" ]; then
    echo
    echo "✗ 以下权限码在注册处声明，但不在启用的权限目录里（该入口会全员 403）："
    sed "s/^/    /" "$tmp_gap"
    echo
    echo "  受保护入口在注册处声明权限点；访客入口保留认证/CSRF 并在同一注册处显式声明豁免。"
    echo "  权限目录只在生产迁移/seed 与 admin 业务写入中变化，启动期只核对、不反写。"
    echo "  合法禁用不算缺口；上面的失败全部是「目录里根本没有这个码」。"
    echo "  （权限点常量见各模块 enums/permission_codes.go；系统数据在 public/schema/tables/sys_permission.sql）"
    exit 1
fi

echo "✓ 没有缺口"

# —— 方向 A2：运行时 /api 入口必须已登记（硬失败）——
#
# 方向 A 只看「声明的权限码在不在目录里」，一条**根本没登记**的路由不会出现在
# 声明集合里，因此永远不会被问到。这里从真实运行时路由出发：**所有需要鉴权或
# 已明确豁免的业务入口**都必须进入同一装配的登记。
#
# 覆盖范围 = /api、/admin、/workbench、/mcp 下的全部入口（含页面 GET 与后台
# HTML 写入口），以及 /access/unlock、/analytics/collect、/payment/callback
# 这类明确声明的访客业务端点。不按路径前缀豁免任何一条：
#   · 页面 GET 另由 check-page-get-authz.sh 核对菜单绑定，但**也必须**在登记里，
#     否则「这个页面要哪项权限」在启动期不可审计；
#   · 后台 HTML 写入口复用 API 权限对象，登记写真实入口 + AuthObject，两者都能被核对。
# 纯静态资源（/static、/storage、/site）与健康检查不是权限入口，排除在外。
grep -E "^[A-Z]+ /" "$tmp_raw" | awk '{print $1" "$2}' \
    | grep -E '^[A-Z]+ /(api|admin|workbench|mcp)(/|$)|^POST /(access/unlock|analytics/collect|payment/callback)$' \
    | sort -u > "$tmp_runtime_authed"
cat "$tmp_declared" "$tmp_exempt" | sort -u > "$tmp_registered"
comm -23 "$tmp_runtime_authed" "$tmp_registered" > "$tmp_undeclared_routes" || true
if [ -s "$tmp_undeclared_routes" ]; then
    echo
    echo "✗ 以下运行时入口没有进入同一装配的权限登记 —— 鉴权/豁免事实不可审计："
    sed "s/^/    /" "$tmp_undeclared_routes"
    echo
    echo "  受保护入口用 RouteGroup 的 GET/POST 或 DeclareRoute 声明权限点；"
    echo "  访客/登录等入口在同组用 ExemptGET/ExemptPOST 显式豁免。"
    exit 1
fi

echo "✓ 运行时入口全部已登记"

# —— 方向 B：权限点行的 api_path 必须在运行时路由表里存在（硬失败）——
#
# 为什么必须硬失败：一条指向不存在端点的权限点看起来完全正常（授权界面列得出、勾得上），
# 但勾了之后那个功能照样打不开（端点不在），而排查的人会先去怀疑角色与策略 —— 方向全错。
# 实测来源：页面路由搬走后权限点还指着旧路径（mail 的联系人写动作）、
# 页面路径被当成权限点但页面组不声明（/admin/orders/overview）。
grep -E "^[A-Z]+ /" "$tmp_raw" | sort -u > "$tmp_all_routes"
# 从数据库读**全部**权限点的真实路径（不只看启用行）：方向 B 判的是目录里的路径
# 是否还指向真实端点，与管理员的启停状态无关。
PGPASSWORD="$DB_PASS" psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -tAc \
    "SELECT DISTINCT upper(api_method) || ' ' || api_path FROM sys_permission
     WHERE api_path IS NOT NULL AND btrim(api_path) <> '';" \
    | sed "s/^[[:space:]]*//;s/[[:space:]]*$//" | { grep -v "^$" || true; } | sort -u > "$tmp_perms"
if [ ! -s "$tmp_perms" ]; then
    echo "权限点表为空 —— 无法做方向 B 检查（先确认迁移已执行）。" >&2
    exit 1
fi
# 可达集合只由**真实事实**构成：
#   · 运行时 Gin 路由（METHOD + PATH）—— 端点必须真的能被请求到；
#   · 页面/写入口复用的策略对象（AuthObject/AuthMethod）—— 它没有同名 API 路由，
#     但确实由某条**已登记的真实入口**在检查（如 /api/i18n/list、/api/seo/audit）。
#
# 关键约束：AuthObject 只有在「它被某条真实入口引用」时才算可达。
# 把「所有声明里的 AuthObject」无条件并入，会让一条指向不存在端点的权限点
# 把 AuthObject 也写成自己、从而自证可达 —— 这正是要堵的反例。
# 逐条声明检查：只有**真实入口本身存在于运行时路由**时，它的 AuthObject 才算可达。
# 这样「声明一个不存在的入口、并把 AuthObject 也指向它」无法自证 ——
# 因为那条入口不在运行时路由里，AuthObject 不会进入可达集合。
grep -E "^DECLARED " "$tmp_raw" \
    | awk -F'|' '{
        n = split($0, parts, " ");
        printf "%s %s %s %s\n", parts[2], parts[3], $3, $4
      }' \
    | while read -r method path auth_obj auth_act; do
        # AuthObject/AuthMethod 为空时按自身入口处理（与 Registry 的默认一致）。
        [ -z "$auth_obj" ] && auth_obj="$path"
        [ -z "$auth_act" ] && auth_act="$method"
        if grep -qxF "$method $path" "$tmp_all_routes"; then
            printf '%s %s\n' "$auth_act" "$auth_obj"
        fi
      done | sort -u > "$tmp_auth_objs_used"
cat "$tmp_all_routes" "$tmp_auth_objs_used" | sort -u > "$tmp_reachable"
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
