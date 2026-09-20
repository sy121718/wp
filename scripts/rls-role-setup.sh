#!/usr/bin/env bash
# rls-role-setup.sh — 为 RLS 生效准备非超级用户连接角色（审计 DB-009 第二步 / DB-04）。
#
# 背景（实测，不是推测）：
#   迁移 199 试点、215 铺开，给 53 个带 project_id 的对象装了 ROW LEVEL SECURITY +
#   FORCE ROW LEVEL SECURITY，策略谓词读会话变量 app.project_id。
#   但 **PostgreSQL 的超级用户总是绕过 RLS**（FORCE 也不例外 —— FORCE 约束的是表属主，
#   不能约束 superuser / BYPASSRLS 角色）。而应用连接用的正是超级用户 root，
#   所以策略目前一行都没挡住。对照实测（wp 库的 themes 表，有 1 行数据）：
#
#     root（rolsuper=t）      未设 app.project_id  → 1 行
#     应用角色（rolsuper=f）  未设 app.project_id  → 0 行（fail closed，符合预期）
#
# 本脚本做三件事，可重复执行（幂等）：
#   1) 建 / 更新非超级角色（显式 NOSUPERUSER NOBYPASSRLS）+ 授权
#      （含 ALTER DEFAULT PRIVILEGES，将来新建的表也自动授权）；
#   2) 用管理连接核对角色属性确实是 NOSUPERUSER NOBYPASSRLS；
#   3) 用该角色真连库验证 RLS 生效（未设变量 fail closed；设了变量只看到本工程）。
#
# 口令来源（按优先级，**不再要求出现在命令行参数里**）：
#   1) 环境变量 GOWP_RLS_ROLE_PASSWORD
#   2) 口令文件（GOWP_RLS_ROLE_ENV_FILE，默认 ~/.config/go_wp/rls-role.env；
#      内容形如 GOWP_RLS_ROLE_PASSWORD=<hex>，权限建议 600）
#   3) 兼容旧式第二参数 —— 会告警：argv 会留在 shell 历史与 ps 输出里。
# 所有含口令的 SQL 都经 **stdin** 送给 psql（不用 -c），因此 ps 看不到口令。
#
# 刻意 **不** 自动改 config.yaml：切连接用户会让「没接好工程作用域」的路径全部查不到
# 数据（fail closed 而不是报错，很难定位）。必须先把各 model 的读写路径都包上
# pkg/rls.InProjectScope 之后再切 —— 顺序见 AGENTS.md「数据库」段的 RLS 条目与
# docs/rls-role-cutover.md。
#
# 用法：bash scripts/rls-role-setup.sh [角色名]
set -euo pipefail
cd "$(dirname "$0")/.."

ROLE="${1:-go_wp_app}"
LEGACY_PASS="${2:-}"
ENV_FILE="${GOWP_RLS_ROLE_ENV_FILE:-${XDG_CONFIG_HOME:-$HOME/.config}/go_wp/rls-role.env}"

if ! command -v psql >/dev/null 2>&1; then
    echo "缺少 psql。" >&2
    exit 1
fi

# 角色名直接拼进 DDL，先按 PG 标识符规则收紧（不做引号包裹，非法字符一律拒绝）。
case "$ROLE" in
    ''|*[!A-Za-z0-9_]*)
        echo "非法角色名：$ROLE（只允许字母、数字、下划线）" >&2
        exit 1
        ;;
esac

# 从口令文件取 GOWP_RLS_ROLE_PASSWORD=...（不 source：只认这一行，避免任意代码执行）。
read_env_file_password() {
    [ -f "$ENV_FILE" ] || return 1
    local line
    line="$(grep -E '^[[:space:]]*GOWP_RLS_ROLE_PASSWORD=' "$ENV_FILE" | tail -n 1 || true)"
    [ -n "$line" ] || return 1
    line="${line#*=}"
    # 去掉可选的成对引号。
    line="$(printf '%s' "$line" | sed -e 's/^"//' -e 's/"$//' -e "s/^'//" -e "s/'$//")"
    [ -n "$line" ] || return 1
    printf '%s' "$line"
}

PASS=""
PASS_SOURCE=""
if [ -n "${GOWP_RLS_ROLE_PASSWORD:-}" ]; then
    PASS="$GOWP_RLS_ROLE_PASSWORD"
    PASS_SOURCE="环境变量 GOWP_RLS_ROLE_PASSWORD"
elif PASS="$(read_env_file_password)"; then
    PASS_SOURCE="口令文件 $ENV_FILE"
elif [ -n "$LEGACY_PASS" ]; then
    PASS="$LEGACY_PASS"
    PASS_SOURCE="命令行参数（不推荐）"
else
    cat >&2 <<'TXT'
缺少口令。三选一（推荐前两种；命令行参数会残留在 shell 历史与 ps 输出里）：
  1) export GOWP_RLS_ROLE_PASSWORD="$(openssl rand -hex 16)"
  2) 写入口令文件（默认 ~/.config/go_wp/rls-role.env，权限 600；可用 GOWP_RLS_ROLE_ENV_FILE
     覆盖路径）后直接重跑本脚本：
       umask 077
       echo "GOWP_RLS_ROLE_PASSWORD=$(openssl rand -hex 16)" > ~/.config/go_wp/rls-role.env
  3) bash scripts/rls-role-setup.sh <角色名> <口令>   # 旧式，会告警
口令文件路径：$GOWP_RLS_ROLE_ENV_FILE（未设置时取默认 ~/.config/go_wp/rls-role.env）
TXT
    exit 1
fi

if [ "$PASS_SOURCE" = "命令行参数（不推荐）" ]; then
    echo "⚠ 口令来自命令行参数：它会留在 shell 历史与 ps 输出里。建议改用 GOWP_RLS_ROLE_PASSWORD 或 $ENV_FILE" >&2
fi

# 单引号转义后再拼进 SQL 字面量（含口令的 SQL 只经 stdin 进 psql）。
ESCAPED_PASS="$(printf '%s' "$PASS" | sed "s/'/''/g")"

if [ ! -f config.yaml ]; then
    echo "缺少 config.yaml（从 config.yaml.example 复制一份并填好 database 段）。" >&2
    exit 1
fi

# 从 config.yaml 的 database 段取值（不在脚本里另存一份口令）。
yaml_db() {
    awk -v key="$1" '
        /^database:/ { in_db = 1; next }
        /^[a-zA-Z]/ { in_db = 0 }
        in_db && $1 == key":" { print $2; exit }
    ' config.yaml
}

DB_HOST="$(yaml_db host)"
DB_PORT="$(yaml_db port)"
DB_USER="$(yaml_db user)"
DB_PASS="$(yaml_db password)"
DB_NAME="$(yaml_db dbname)"

psql_query() {
    PGPASSWORD="$2" psql -h "$DB_HOST" -p "$DB_PORT" -U "$1" -d "$DB_NAME" -v ON_ERROR_STOP=1 -tA -f -
}
run_as_admin() { psql_query "$DB_USER" "$DB_PASS"; }
run_as_app() { psql_query "$ROLE" "$PASS"; }

# 从 psql 输出里取 KEY=value（value 不含空格；psql 还会打 BEGIN / t / COMMIT 噪声）。
# 末尾的 || true 是必须的：赋值语句里管道失败会在 set -e 下静默退出脚本。
pick_value() {
    grep -oE "^$1=.*" | head -n 1 | cut -d= -f2- || true
}

echo "→ 目标库 $DB_NAME@$DB_HOST:$DB_PORT（管理连接用户 $DB_USER；口令来源：$PASS_SOURCE）"

# 1) 角色：显式 NOSUPERUSER NOBYPASSRLS —— 两者任一为真，RLS 就形同虚设。
if [ "$(printf '%s\n' "SELECT 1 FROM pg_roles WHERE rolname = '$ROLE'" | run_as_admin)" = "1" ]; then
    echo "→ 角色 $ROLE 已存在，更新口令与属性（幂等）"
    printf '%s\n' "ALTER ROLE $ROLE WITH LOGIN NOSUPERUSER NOBYPASSRLS PASSWORD '$ESCAPED_PASS'" | run_as_admin >/dev/null
else
    echo "→ 创建角色 $ROLE"
    printf '%s\n' "CREATE ROLE $ROLE WITH LOGIN NOSUPERUSER NOBYPASSRLS PASSWORD '$ESCAPED_PASS'" | run_as_admin >/dev/null
fi

# 2) 授权。ALTER DEFAULT PRIVILEGES 是关键：没有它，将来迁移新建的表不授权给本角色，
#    应用会在第一次访问时报 permission denied（那时才补 GRANT 就晚了）。
echo "→ 授权（schema / 现有对象 / 将来新建对象）"
printf '%s\n' "GRANT CONNECT ON DATABASE $DB_NAME TO $ROLE" | run_as_admin >/dev/null
printf '%s\n' "GRANT USAGE ON SCHEMA public TO $ROLE" | run_as_admin >/dev/null
printf '%s\n' "GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO $ROLE" | run_as_admin >/dev/null
printf '%s\n' "GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO $ROLE" | run_as_admin >/dev/null
printf '%s\n' "ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO $ROLE" | run_as_admin >/dev/null
printf '%s\n' "ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO $ROLE" | run_as_admin >/dev/null

# 3) 属性核对：授权看起来成功、角色却仍带 BYPASSRLS，是这套改造最贵的失败方式。
ATTR_SUPER="$(printf '%s\n' "SELECT 'ATTR_SUPER=' || rolsuper FROM pg_roles WHERE rolname = '$ROLE'" | run_as_admin | pick_value ATTR_SUPER)"
ATTR_BYPASS="$(printf '%s\n' "SELECT 'ATTR_BYPASS=' || rolbypassrls FROM pg_roles WHERE rolname = '$ROLE'" | run_as_admin | pick_value ATTR_BYPASS)"
echo "   角色属性：rolsuper=$ATTR_SUPER rolbypassrls=$ATTR_BYPASS"
# PG 把布尔与文本拼接成 'true'/'false'（psql 直接显示布尔列才是 t/f），这里按 'false' 判。
if [ "$ATTR_SUPER" != "false" ] || [ "$ATTR_BYPASS" != "false" ]; then
    echo "✗ 角色 $ROLE 仍会绕过 RLS（期望两者都是 false，实际 rolsuper=$ATTR_SUPER rolbypassrls=$ATTR_BYPASS）" >&2
    exit 1
fi
echo "   ✓ NOSUPERUSER NOBYPASSRLS"

# 4) 验证：在**启用过 RLS** 的带 project_id 表里挑一张有数据的，比较两个身份的可见行数。
#    必须限定 pc.relrowsecurity，否则可能挑到一张没装策略的表（那里谁都看得见，
#    「应用角色 0 行」会变成假阴性/假阳性）；先用 LIMIT 1 廉价探测有没有数据，
#    避免在千万行的流水表上做全表 count。
echo "→ 验证 RLS 是否真的生效"
CANDIDATES="$(printf '%s\n' "SELECT c.table_name FROM information_schema.columns c JOIN information_schema.tables t ON t.table_schema = c.table_schema AND t.table_name = c.table_name JOIN pg_class pc ON pc.relnamespace = 'public'::regnamespace AND pc.relname = c.table_name WHERE c.table_schema = 'public' AND c.column_name = 'project_id' AND t.table_type = 'BASE TABLE' AND c.is_nullable = 'NO' AND pc.relrowsecurity ORDER BY c.table_name LIMIT 60" | run_as_admin)"
if [ -z "$CANDIDATES" ]; then
    echo "✗ 当前库里没有「带 project_id 且启用 RLS」的表 —— 迁移 215 是否已经跑过？" >&2
    exit 1
fi
PROBE=""
FALLBACK=""
for candidate in $CANDIDATES; do
    # 第一个候选先兜底（可能是空表）；优先选「有数据」的表，
    # 再优先选「有 ≥2 个工程」的表 —— 这样脚本自己就能验出跨工程不可见。
    [ -n "$FALLBACK" ] || FALLBACK="$candidate"
    has="$(printf '%s\n' "SELECT 'HAS=' || count(*) FROM (SELECT 1 FROM $candidate LIMIT 1) AS probe" | run_as_admin | pick_value HAS)"
    if [ "$has" = "1" ]; then
        [ -n "$PROBE" ] || PROBE="$candidate"
        distinct="$(printf '%s\n' "SELECT 'D=' || count(*) FROM (SELECT DISTINCT project_id FROM $candidate LIMIT 2) AS probe" | run_as_admin | pick_value D)"
        if [ "$distinct" = "2" ]; then
            PROBE="$candidate"
            break
        fi
    fi
done
[ -n "$PROBE" ] || PROBE="$FALLBACK"
AS_ADMIN="$(printf '%s\n' "SELECT 'ADMIN=' || count(*) FROM $PROBE" | run_as_admin | pick_value ADMIN)"
AS_APP="$(printf '%s\n' "SELECT 'APP=' || count(*) FROM $PROBE" | run_as_app 2>/dev/null | pick_value APP || true)"
[ -n "$AS_APP" ] || AS_APP="ERR"

echo "   探针表 $PROBE：管理连接 $AS_ADMIN 行 / 应用角色（未设 app.project_id）$AS_APP 行"
if [ "$AS_APP" = "0" ]; then
    echo "   ✓ RLS 生效（应用角色 fail closed）"
elif [ "$AS_APP" = "ERR" ]; then
    echo "   ✗ 应用角色连不上或无权读该表 —— 检查 GRANT 与 pg_hba.conf" >&2
    exit 1
else
    echo "   ✗ 应用角色仍能看到行 —— 角色属性或策略有问题（检查 rolsuper / rolbypassrls）" >&2
    exit 1
fi

# 5) 更强的验证（探针表有数据时）：设了 app.project_id 只看到本工程，别的工程不可见。
if [ "$AS_ADMIN" != "0" ]; then
    OWN_PID="$(printf '%s\n' "SELECT 'OWN=' || project_id::text FROM $PROBE LIMIT 1" | run_as_admin | grep -oE '^OWN=.*' | cut -d= -f2 || true)"
    OTHER_PID="$(printf '%s\n' "SELECT 'OTHER=' || project_id::text FROM $PROBE WHERE project_id::text <> '$OWN_PID' LIMIT 1" | run_as_admin | grep -oE '^OTHER=.*' | cut -d= -f2 || true)"
    # 用 %s 占位拼 SQL：单引号经 SQ 传入，避免 printf 格式串里再叠一层引号转义。
    SQ="'"
    SCOPED="$(printf 'BEGIN;\nSELECT set_config(%sapp.project_id%s, %s%s%s, true) IS NOT NULL;\nSELECT %sSCOPED=%s || count(*) FROM %s;\nCOMMIT;\n' "$SQ" "$SQ" "$SQ" "$OWN_PID" "$SQ" "$SQ" "$SQ" "$PROBE" | run_as_app | pick_value SCOPED)"
    [ -n "$SCOPED" ] || SCOPED="ERR"
    echo "   本工程 $OWN_PID：设变量后可见 $SCOPED 行"
    if [ "$SCOPED" = "0" ] || [ "$SCOPED" = "ERR" ]; then
        echo "   ✗ 设了 app.project_id 仍读不到本工程的行（SCOPED=$SCOPED）—— 策略谓词或变量名对不上" >&2
        exit 1
    fi
    if [ -n "$OTHER_PID" ]; then
        CROSS="$(printf 'BEGIN;\nSELECT set_config(%sapp.project_id%s, %s%s%s, true) IS NOT NULL;\nSELECT %sCROSS=%s || count(*) FROM %s WHERE project_id::text = %s%s%s;\nCOMMIT;\n' "$SQ" "$SQ" "$SQ" "$OWN_PID" "$SQ" "$SQ" "$SQ" "$PROBE" "$SQ" "$OTHER_PID" "$SQ" | run_as_app | pick_value CROSS)"
        [ -n "$CROSS" ] || CROSS="ERR"
        echo "   别的工程 $OTHER_PID（作用域仍是 $OWN_PID）：可见 $CROSS 行"
        if [ "$CROSS" != "0" ]; then
            echo "   ✗ 跨工程可见（CROSS=$CROSS）—— 工程隔离被破坏" >&2
            exit 1
        fi
        echo "   ✓ 本工程可读写、别的工程不可见"
    fi
else
    echo "   （探针表为空，跳过跨工程可见性验证：先跑一次应用或造一行数据）"
fi

cat <<'TXT'

剩下两步（顺序不能反）：

  1. 给各模块的读写路径带上工程作用域 —— 每个访问带 project_id 表的 model 方法
     用 pkg/rls.InProjectScope(ctx, db, projectID, fn) 包起来。
     **必须先做完这一步再切角色**：顺序反过来的话，没包 scope 的路径会静默返回 0 行
     （fail closed 不报错），表现为「功能突然查不到数据」而没有任何错误日志。
     盘点与检索命令见 docs/rls-role-cutover.md（含当前缺口清单）。

  2. 改 config.yaml 的 database.user / database.password 为本角色，重启应用。
     · 口令建议走环境变量注入：database.password 留空 + GOWP_DATABASE_PASSWORD=...
       （或从上面的口令文件读出后 export，别把明文写回 config.yaml）。
     · 切换后把 database.require_rls_role 置 true：启动期探针会核对 session_user /
       current_user / rolsuper / rolbypassrls，连接角色仍会绕过 RLS 时直接拒绝启动
       （默认 false 只打 WARN —— 迁移与运维连接用管理角色，不该被拦）。
     · 迁移与运维脚本继续用管理连接（超级用户），它们需要 DDL 与 BYPASSRLS 能力。

  参考实现：internal/module/project/model/locale_model.go 的 ListLocales / ReplaceLocales
  （project_locales 是 199 的试点样板）。
  · 回滚：把 database.user 改回管理角色并重启即可；角色本身可 DROP ROLE go_wp_app。
TXT
