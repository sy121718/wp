#!/usr/bin/env bash
# rls-role-setup.sh — 为 RLS 生效准备非超级用户连接角色（审计 DB-009 第二步）。
#
# 背景（实测，不是推测）：
#   迁移 199 试点、215 铺开，给 53 个带 project_id 的对象装了 ROW LEVEL SECURITY +
#   FORCE ROW LEVEL SECURITY，策略谓词读会话变量 app.project_id。
#   但 **PostgreSQL 的超级用户总是绕过 RLS**（FORCE 也不例外 —— FORCE 约束的是表属主，
#   不能约束 superuser / BYPASSRLS 角色）。而应用连接用的正是超级用户 root，
#   所以策略目前一行都没挡住。对照实测（wp 库的 themes 表，有 1 行数据）：
#
#     root（rolsuper=t）      未设 app.project_id  → 1 行
#     普通角色（rolsuper=f）  未设 app.project_id  → 0 行（fail closed，符合预期）
#
# 本脚本做两件事，可重复执行：
#   1) 建非超级角色 + 授权（含 ALTER DEFAULT PRIVILEGES，将来新建的表也自动授权）；
#   2) 用该角色验证 RLS 确实生效，并打印剩下两步。
#
# 刻意 **不** 自动改 config.yaml：切连接用户会让「没接好工程作用域」的路径全部查不到
# 数据（fail closed 而不是报错，很难定位）。必须先把各 model 的读写路径都包上
# pkg/rls.InProjectScope 之后再切 —— 顺序见 AGENTS.md「数据库」段的 RLS 条目。
#
# 用法：bash scripts/rls-role-setup.sh [角色名] [密码]
set -euo pipefail
cd "$(dirname "$0")/.."

ROLE="${1:-go_wp_app}"
PASS="${2:-}"

if ! command -v psql >/dev/null 2>&1; then
    echo "缺少 psql。" >&2
    exit 1
fi
if [ -z "$PASS" ]; then
    echo "用法：bash scripts/rls-role-setup.sh <角色名> <密码>" >&2
    echo "（不自动生成密码：它要写进 config.yaml，需要你自己掌握。）" >&2
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

run_as_admin() {
    PGPASSWORD="$DB_PASS" psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -v ON_ERROR_STOP=1 -tAc "$1"
}

echo "→ 目标库 ${DB_NAME}@${DB_HOST}:${DB_PORT}（管理连接用户 ${DB_USER}）"

# 1) 角色：显式 NOSUPERUSER NOBYPASSRLS —— 两者任一为真，RLS 就形同虚设。
if [ "$(run_as_admin "SELECT 1 FROM pg_roles WHERE rolname = '${ROLE}'")" = "1" ]; then
    echo "→ 角色 ${ROLE} 已存在，更新口令与属性"
    run_as_admin "ALTER ROLE ${ROLE} WITH LOGIN NOSUPERUSER NOBYPASSRLS PASSWORD '${PASS}'" >/dev/null
else
    echo "→ 创建角色 ${ROLE}"
    run_as_admin "CREATE ROLE ${ROLE} WITH LOGIN NOSUPERUSER NOBYPASSRLS PASSWORD '${PASS}'" >/dev/null
fi

# 2) 授权。ALTER DEFAULT PRIVILEGES 是关键：没有它，将来迁移新建的表不授权给本角色，
#    应用会在第一次访问时报 permission denied（那时才补 GRANT 就晚了）。
echo "→ 授权（schema / 现有对象 / 将来新建对象）"
run_as_admin "GRANT CONNECT ON DATABASE ${DB_NAME} TO ${ROLE}" >/dev/null
run_as_admin "GRANT USAGE ON SCHEMA public TO ${ROLE}" >/dev/null
run_as_admin "GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO ${ROLE}" >/dev/null
run_as_admin "GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO ${ROLE}" >/dev/null
run_as_admin "ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO ${ROLE}" >/dev/null
run_as_admin "ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO ${ROLE}" >/dev/null

# 3) 验证：找一张「有数据且带 project_id」的表，比较两个身份的可见行数。
echo "→ 验证 RLS 是否真的生效"
PROBE="$(run_as_admin "SELECT c.table_name FROM information_schema.columns c JOIN information_schema.tables t ON t.table_schema = c.table_schema AND t.table_name = c.table_name WHERE c.table_schema = 'public' AND c.column_name = 'project_id' AND t.table_type = 'BASE TABLE' AND c.is_nullable = 'NO' ORDER BY c.table_name LIMIT 1")"
AS_ADMIN="$(run_as_admin "SELECT count(*) FROM ${PROBE}")"
AS_APP="$(PGPASSWORD="$PASS" psql -h "$DB_HOST" -p "$DB_PORT" -U "$ROLE" -d "$DB_NAME" -tAc "SELECT count(*) FROM ${PROBE}" 2>/dev/null || echo "ERR")"

echo "   探针表 ${PROBE}：管理连接 ${AS_ADMIN} 行 / 应用角色（未设 app.project_id）${AS_APP} 行"
if [ "$AS_APP" = "0" ]; then
    echo "   ✓ RLS 生效（应用角色 fail closed）"
elif [ "$AS_APP" = "ERR" ]; then
    echo "   ✗ 应用角色连不上或无权读该表 —— 检查 GRANT 与 pg_hba.conf" >&2
    exit 1
else
    echo "   ✗ 应用角色仍能看到行 —— 角色属性或策略有问题（检查 rolsuper / rolbypassrls）" >&2
    exit 1
fi

cat <<TXT

剩下两步（顺序不能反）：

  1. 给各模块的读写路径带上工程作用域 —— 每个访问带 project_id 表的 model 方法
     用 pkg/rls.InProjectScope(ctx, db, projectID, fn) 包起来。
     **必须先做完这一步再切角色**：顺序反过来的话，没包 scope 的路径会静默返回 0 行
     （fail closed 不报错），表现为「功能突然查不到数据」而没有任何错误日志。

  2. 改 config.yaml 的 database.user / database.password 为本角色，重启应用。
     迁移与运维脚本仍用管理连接（超级用户），它们需要 DDL 与 BYPASSRLS 能力。

  参考实现：internal/module/project/model/locale_model.go 的 ListLocales / ReplaceLocales
  （project_locales 是 199 的试点，唯一的既有样板）。
TXT
