#!/usr/bin/env bash
# baseline-verify.sh — 初始化基线验收（需要本地 PostgreSQL）。
#
# 验收三条，任何一条不成立即非零退出：
#   1. 等价：空库跑全量迁移 与「重放 schema/baseline/init.sql 后再跑迁移」结构一致、系统数据行数一致；
#   2. 幂等：基线库再跑一次迁移，结构逐字节不变、sys_baseline 仍只有一行；
#   3. 回落：篡改 sys_baseline.manifest_sha256 后删一张基线内迁移建的表，再跑迁移必须把它建回来
#      （证明哈希不匹配时确实回落全量，而不是静默跳过）。
#
# 结构比较前要归一 pg_dump 的两种 deparse：同一 CHECK 表达式在一阶库是
# ANY ((ARRAY['a'::character varying, ...])::text[])，重放一次后变成
# ANY (ARRAY[('a'::character varying)::text, ...])。两者语义相同，见 docs/initial-baseline.md。
#
# 用法：
#   bash scripts/baseline-verify.sh           # 建两个临时库跑验收，结束即删
#   bash scripts/baseline-verify.sh --keep    # 保留临时库与日志，便于排查
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT_DIR"

yaml_db() {
  [ -f config.yaml ] || return 0
  awk -v key="$1" '/^database:/{in_db=1;next} /^[a-zA-Z]/{in_db=0} in_db && $1==key":"{print $2;exit}' config.yaml
}

DB_HOST=$(yaml_db host)
[ -n "$DB_HOST" ] || DB_HOST=127.0.0.1
DB_PORT=$(yaml_db port)
[ -n "$DB_PORT" ] || DB_PORT=5432
DB_USER=$(yaml_db user)
export PGPASSWORD=$(yaml_db password)

ADMIN_DB=postgres
MONTH_PARTITION='*_[0-9][0-9][0-9][0-9]_[0-9][0-9]'
# 回落探针表：由 manifest 内的迁移 705 建，幂等 CREATE TABLE IF NOT EXISTS，seed 不依赖它。
PROBE_TABLE=admin_side_effect_tasks

KEEP=0
if [ "$#" -ge 1 ] && [ "$1" = "--keep" ]; then KEEP=1; fi

command -v pg_dump >/dev/null 2>&1 || { echo "✗ 需要 pg_dump" >&2; exit 1; }
command -v psql >/dev/null 2>&1 || { echo "✗ 需要 psql" >&2; exit 1; }
command -v go >/dev/null 2>&1 || { echo "✗ 需要 go" >&2; exit 1; }

SUFFIX=$$
FULL_DB="wp_baseline_full_$SUFFIX"
BASE_DB="wp_baseline_base_$SUFFIX"
LOG_DIR="/tmp/wp-baseline-verify-$SUFFIX"
mkdir -p "$LOG_DIR"

admin_psql() { psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$ADMIN_DB" -v ON_ERROR_STOP=1 "$@"; }
db_psql() {
  local db="$1"; shift
  psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$db" -tA -v ON_ERROR_STOP=1 "$@"
}
drop_db() { admin_psql -c "DROP DATABASE IF EXISTS \"$1\" WITH (FORCE)" >/dev/null 2>&1 || true; }
create_db() { admin_psql -c "CREATE DATABASE \"$1\"" >/dev/null; }

cleanup() {
  if [ "$KEEP" -eq 1 ]; then
    echo "… 保留临时库：$FULL_DB / $BASE_DB；日志：$LOG_DIR"
    return 0
  fi
  drop_db "$FULL_DB"
  drop_db "$BASE_DB"
}
trap cleanup EXIT

fail() { echo "✗ $*" >&2; exit 1; }

migrate_db() {
  local db="$1" log="$2"
  if ! GOWP_DATABASE_DBNAME="$db" go run ./cmd -migrate-only >"$log" 2>&1; then
    tail -40 "$log" >&2
    fail "$db 跑迁移失败，日志：$log"
  fi
}

normalize() {
  sed -E \
    -e 's/\(\(ARRAY\[(.*)\]\)::text\[\]\)/(ARRAY[\1])/' \
    -e "s/\('([^']*)'::character varying\)::text/'\1'::character varying/g" \
    -e '/^-- Dumped (from|by|on) /d' \
    -e '/^\\(un)?restrict /d' \
    -e '/^SET (transaction_timeout|idle_in_transaction_session_timeout) =/d' \
    -e 's/[[:space:]]+$//'
}

struct_dump() {
  pg_dump -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$1" \
    --schema-only --no-owner --no-privileges --no-comments \
    --exclude-table="$MONTH_PARTITION" 2>/dev/null | normalize
}

table_counts() {
  local db="$1" q
  q=$(db_psql "$db" -c "SELECT string_agg(format('SELECT %L AS tbl, count(*) AS n FROM public.%I', c.relname, c.relname), ' UNION ALL ' ORDER BY c.relname)
        FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname = 'public' AND c.relkind = 'r' AND c.relpersistence = 'p'
          AND c.relname <> 'sys_baseline'")
  [ -n "$q" ] || fail "$db 没有可比对的表"
  db_psql "$db" -c "$q ORDER BY tbl"
}

echo "… 1/4 建全量库 $FULL_DB 并跑完整迁移"
create_db "$FULL_DB"
migrate_db "$FULL_DB" "$LOG_DIR/full.log"

echo "… 2/4 建基线库 $BASE_DB，重放 schema/baseline/init.sql 后跑迁移"
create_db "$BASE_DB"
if ! psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$BASE_DB" -v ON_ERROR_STOP=1 \
  -f schema/baseline/init.sql >"$LOG_DIR/base-init.log" 2>&1; then
  tail -30 "$LOG_DIR/base-init.log" >&2
  fail "重放 schema/baseline/init.sql 失败"
fi
migrate_db "$BASE_DB" "$LOG_DIR/base-migrate.log"

echo "… 3/4 比较结构、系统数据行数、幂等"
struct_dump "$FULL_DB" >"$LOG_DIR/full.struct.sql"
struct_dump "$BASE_DB" >"$LOG_DIR/base.struct.sql"
if ! diff -u "$LOG_DIR/full.struct.sql" "$LOG_DIR/base.struct.sql" >"$LOG_DIR/struct.diff"; then
  echo "── 结构差异（前 40 行）──" >&2
  head -40 "$LOG_DIR/struct.diff" >&2
  fail "基线库与全量库结构不一致：$LOG_DIR/struct.diff"
fi
echo "  ✓ 结构一致（月分区除外，deparse 已归一）"

table_counts "$FULL_DB" >"$LOG_DIR/full.counts.txt"
table_counts "$BASE_DB" >"$LOG_DIR/base.counts.txt"
if ! diff -u "$LOG_DIR/full.counts.txt" "$LOG_DIR/base.counts.txt" >"$LOG_DIR/counts.diff"; then
  echo "── 行数差异（前 30 行）──" >&2
  head -30 "$LOG_DIR/counts.diff" >&2
  fail "系统数据行数不一致：$LOG_DIR/counts.diff"
fi
echo "  ✓ $(wc -l <"$LOG_DIR/full.counts.txt" | tr -d ' ') 张表的行数一致（sys_baseline 除外）"

migrate_db "$BASE_DB" "$LOG_DIR/base-migrate-2.log"
struct_dump "$BASE_DB" >"$LOG_DIR/base.struct.2.sql"
if ! diff -q "$LOG_DIR/base.struct.sql" "$LOG_DIR/base.struct.2.sql" >/dev/null; then
  fail "基线库重复迁移后结构变化：$LOG_DIR/base.struct.2.sql"
fi
rows=$(db_psql "$BASE_DB" -c "SELECT count(*) FROM sys_baseline")
[ "$rows" = "1" ] || fail "重复迁移后 sys_baseline 行数为 $rows，应为 1"
echo "  ✓ 重复执行幂等（结构不变、sys_baseline 仍 1 行）"

echo "… 4/4 验证哈希不匹配时回落全量"
db_psql "$BASE_DB" -c "UPDATE sys_baseline SET manifest_sha256 = repeat('0', 64)" >/dev/null
db_psql "$BASE_DB" -c "DROP TABLE IF EXISTS public.$PROBE_TABLE" >/dev/null
[ "$(db_psql "$BASE_DB" -c "SELECT to_regclass('public.$PROBE_TABLE') IS NULL")" = "t" ] || fail "探针表未删除"
migrate_db "$BASE_DB" "$LOG_DIR/base-migrate-fallback.log"
[ "$(db_psql "$BASE_DB" -c "SELECT to_regclass('public.$PROBE_TABLE') IS NOT NULL")" = "t" ] || \
  fail "哈希不匹配时迁移未回落全量：$PROBE_TABLE 未被重建（日志：$LOG_DIR/base-migrate-fallback.log）"
echo "  ✓ 哈希不匹配 → 全量迁移重建了 $PROBE_TABLE"

echo "✓ 初始化基线验收通过（日志：$LOG_DIR）"
