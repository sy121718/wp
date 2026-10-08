#!/usr/bin/env bash
# check-page-get-authz.sh — 页面 GET 的鉴权 obj 必须与**该页菜单绑的权限点**同源（docs/02-Z §4.3）。
#
# 为什么需要它：菜单按权限渲染，但**菜单隐藏不是访问控制** —— 任何登录账号直输 URL 就能进。
# 页面 GET 现在与 /api/* 走**同一条** Casbin 链（shell.PageAuthz），于是多出一个静默失败态：
# **obj 写错**。obj 与菜单绑定的码不一致时，表现是「菜单看得见、点进去被拒」（或反过来
# 「菜单看不见、直输 URL 反而进得去」），而所有既有检查都是绿的 —— 路由注册成功、权限点在库、
# 策略也有。这个门禁就是钉住这条对应关系。
#
# 判据（双向）：
#   A. 菜单 → 代码：每条 path 非空的 type=2 菜单行，其页面 GET 必须挂了 shell.PageAuthz*
#      （豁免写进 scripts/page-get-authz-allow.txt；豁免条目**不再命中即失败** —— 只增不减
#      的清单等于没有门禁）；
#   B. obj ∈ 绑定码集：挂的 obj(+act) 必须等于该菜单行绑定的**某个**权限码的
#      (api_path, api_method)。obj 取自 sys_permission.api_path，这就是「同源」的可判定形式。
#
# 刻意放过的（不是整页导航目标）：抽屉/面板/片段（htmx 局部替换）、iframe 预览帧、
# 302 重定向页、只读计算端点 —— 它们的理由写在 allow 文件里，而不是靠本脚本猜。
#
# 用法：bash scripts/check-page-get-authz.sh
# 依赖：psql + 本地数据库（配置从 config.yaml 的 database 段读，与 check-menu-page-binding.sh 同规则）。
# 退出码：0 = 全部同源；1 = 有缺口或环境不满足。
set -euo pipefail
cd "$(dirname "$0")/.."

if ! command -v psql >/dev/null 2>&1; then
    echo "缺少 psql，无法比对页面鉴权与菜单绑定。" >&2
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

ALLOW_FILE="scripts/page-get-authz-allow.txt"
tmp_code=$(mktemp); tmp_menu=$(mktemp); tmp_allow=$(mktemp); tmp_used=$(mktemp)
trap 'rm -f "$tmp_code" "$tmp_menu" "$tmp_allow" "$tmp_used"' EXIT

# ── 代码侧：收集 (页面路径, obj, act) ────────────────────────────────────────────
# 页面路径是**相对组**的（组前缀 /admin 由装配层加），所以后面比对前要先去掉菜单 path 的 /admin。
grep -rnE 'shell\.PageAuthz(As)?\("' --include='*.go' internal 2>/dev/null \
    | grep -v '_test\.go' \
    | awk '
        {
            rest = $0
            sub(/^[^:]*:[0-9]+:/, "", rest)
            if (!match(rest, /\.GET\("[^"]+"/)) next
            page = substr(rest, RSTART, RLENGTH)
            sub(/\.GET\("/, "", page); sub(/"$/, "", page)
            if (!match(rest, /PageAuthz(As)?\("[^"]+"/)) next
            obj = substr(rest, RSTART, RLENGTH)
            sub(/PageAuthz(As)?\("/, "", obj); sub(/"$/, "", obj)
            act = "GET"
            if (rest ~ /PageAuthzAs\(/ && match(rest, /Method[A-Z][A-Za-z]*/)) {
                m = substr(rest, RSTART, RLENGTH); sub(/^Method/, "", m); act = toupper(m)
            }
            print page "\t" obj "\t" act
        }' | sort -u > "$tmp_code"

if [ ! -s "$tmp_code" ]; then
    echo "没扫到任何 shell.PageAuthz 调用 —— 判据 A 无从谈起（脚本的正则或目录结构变了？）。" >&2
    exit 1
fi

# ── 菜单侧：每条菜单行 + 它绑定的码声明的 (api_path, api_method) ────────────────
PGPASSWORD="$DB_PASS" psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -tAF'|' -c \
    "SELECT m.path, coalesce(p.api_path, ''), coalesce(p.api_method, '')
       FROM sys_menus m
       LEFT JOIN sys_menu_permission mp ON mp.menu_id = m.id
       LEFT JOIN sys_permission p ON p.permission_code = mp.permission_code AND p.status = 1
      WHERE m.type = 2 AND m.status = 1 AND m.deleted_at IS NULL AND m.path <> ''
      ORDER BY m.path;" > "$tmp_menu"

if [ ! -s "$tmp_menu" ]; then
    echo "菜单表里没有带 path 的页面行 —— 数据库连错库了？" >&2
    exit 1
fi

# ── 豁免清单：`页面路径  # 理由`，页面路径写**菜单 path**（含 /admin）────────────
if [ -f "$ALLOW_FILE" ]; then
    # 注意：只有注释的文件会让 grep 空匹配返回 1，set -o pipefail 会在这里直接退出
    # （踩过：脚本静默无输出）。所以两条 grep 都要兜底。
    { grep -v '^[[:space:]]*#' "$ALLOW_FILE" || true; } \
        | { grep -v '^[[:space:]]*$' || true; } \
        | awk '{print $1}' | sort -u > "$tmp_allow"
else
    : > "$tmp_allow"
fi

violations=0

while IFS='|' read -r mpath api_path api_method; do
    [ -n "$mpath" ] || continue
    # 后台首页（/admin）登录即可进，没有绑定任何权限码 —— 不是豁免，是没有可校验的同源对象。
    [ "$mpath" = "/admin" ] && continue
    src="${mpath#/admin}"

    # 页面在代码里挂的 (obj, act)（同一路径可能挂多处以兼容不同入口，任一匹配即通过）
    hits=$(awk -F'\t' -v p="$src" '$1 == p {print $2 "\t" $3}' "$tmp_code" || true)

    if [ -z "$hits" ]; then
        if grep -qxF "$mpath" "$tmp_allow"; then
            echo "$mpath" >> "$tmp_used"
            continue
        fi
        echo "✗ ${mpath}：菜单入口没有页面鉴权 —— 直输 URL 就能进这个页面（docs/02-Z §4.3）。" >&2
        echo "   修：pages.GET(\"${src}\", shell.PageAuthz(\"<该页菜单绑的码的 api_path>\"), h)" >&2
        violations=$((violations + 1))
        continue
    fi

    if [ -z "$api_path" ]; then
        # 菜单行没有绑定任何权限码：无可校验的同源对象（典型：/admin 首页，登录即可）。
        continue
    fi

    ok=0
    while IFS=$'\t' read -r obj act; do
        [ -n "$obj" ] || continue
        if [ "$obj" = "$api_path" ] && [ "$act" = "$api_method" ]; then
            ok=1
            break
        fi
        # 一个菜单可以绑多个码（例：/admin/ai/sessions 绑 provider_list + session_list），
        # 逐个比对全部绑定行。
        while IFS='|' read -r _p2 ap2 am2; do
            if [ "$obj" = "$ap2" ] && [ "$act" = "$am2" ]; then
                ok=1
                break
            fi
        done < "$tmp_menu"
        [ "$ok" = 1 ] && break
    done <<< "$hits"

    if [ "$ok" != 1 ]; then
        echo "✗ ${mpath}：页面鉴权的 obj 与该页菜单绑的码不同源。" >&2
        echo "   实际挂的：$(echo "$hits" | tr '\n' ' ')" >&2
        echo "   菜单绑的码声明：${api_path} ${api_method}" >&2
        echo "   后果：菜单可见却点进去被拒（或反之），而所有其它检查都是绿的。" >&2
        violations=$((violations + 1))
    fi
    # 一个菜单路径可能绑多个码（多行），按路径去重后只报一次。
done < <(awk -F'|' '!seen[$1]++' "$tmp_menu")

# 豁免条目不再命中 → 失败：条目过期就该删，否则清单会长成「没人敢动的白名单」。
if [ -s "$tmp_allow" ]; then
    while IFS= read -r entry; do
        [ -n "$entry" ] || continue
        if ! grep -qxF "$entry" "$tmp_used" 2>/dev/null; then
            echo "✗ 豁免条目 ${entry} 已不再命中 —— 要么页面已经挂上鉴权（删掉这条），要么菜单路径改了（更新条目）。" >&2
            violations=$((violations + 1))
        fi
    done < "$tmp_allow"
fi

if [ "$violations" -gt 0 ]; then
    echo
    echo "页面 GET 鉴权门禁：${violations} 处缺口（判据见 docs/02-Z §4.3）。" >&2
    exit 1
fi

menus=$(wc -l < "$tmp_menu" | tr -d ' ')
authz=$(wc -l < "$tmp_code" | tr -d ' ')
echo "页面 GET 鉴权：菜单页 ${menus} 条 / 已挂页面鉴权 ${authz} 处，全部同源。"
