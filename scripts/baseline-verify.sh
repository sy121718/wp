#!/usr/bin/env bash
# baseline-verify.sh — 初始化基线验收（需要本地 PostgreSQL）。
#
# 验收五条，任何一条不成立即非零退出：
#   1. 等价：空库跑全量迁移 与「重放 schema/baseline/init.sql 后再跑迁移」结构一致、
#      系统数据**内容**一致（逐表行数 + 行级 md5 聚合，时间字面量归一）；
#   2. 幂等：基线库再跑一次迁移，结构逐字节不变、sys_baseline 仍只有一行；
#   3. 跳过生效：匹配哈希时删掉一张基线内迁移建的表，再跑迁移它**不能被重建**
#      （证明基线覆盖的迁移确实没执行，而不是执行了再靠幂等检查混过去）；
#   4. 拒绝重放：篡改 sys_baseline.manifest_sha256 后再跑迁移必须失败并给出「基线产物已变更」提示，
#      库结构保持不变（历史迁移假定从空库开始，在终态结构上重放必然半途报错）；
#   5. 摘要口径：init.sql 记录的内容摘要必须等于 cmd/baseline-digest 的输出
#      （生成器与迁移器各算一遍，这里钉住两侧一致）。
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

# 每张表的行数与内容指纹：行级 md5（时间字面量归一）排序后再聚合。
# 只比行数证明不了终态一致 —— 缺一个必需键、同时多一条无关记录，行数照样相等。
content_fingerprints() {
  local db="$1"
  psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$db" -tA -v ON_ERROR_STOP=1 <<'SQL'
SET TimeZone = 'UTC';
SELECT format(
  'SELECT %L AS tbl, count(*) AS n, coalesce(md5(string_agg(h, '''' ORDER BY h)), '''') AS h FROM (SELECT md5(regexp_replace(x::text, %L, ''<TS>'', ''g'')) AS h FROM public.%I x) s',
  c.relname,
  '[0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]+)?\+[0-9]{2}',
  c.relname)
FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = 'public' AND c.relkind = 'r' AND c.relpersistence = 'p'
  AND c.relname <> 'sys_baseline'
ORDER BY c.relname;
\gexec
SQL
}

echo "… 1/5 建全量库 $FULL_DB 并跑完整迁移"
create_db "$FULL_DB"
migrate_db "$FULL_DB" "$LOG_DIR/full.log"

echo "… 2/5 建基线库 $BASE_DB，重放 schema/baseline/init.sql 后跑迁移"
create_db "$BASE_DB"
if ! psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$BASE_DB" -v ON_ERROR_STOP=1 \
  -f schema/baseline/init.sql >"$LOG_DIR/base-init.log" 2>&1; then
  tail -30 "$LOG_DIR/base-init.log" >&2
  fail "重放 schema/baseline/init.sql 失败"
fi
migrate_db "$BASE_DB" "$LOG_DIR/base-migrate.log"

echo "… 3/5 比较结构、系统数据内容、幂等"
struct_dump "$FULL_DB" >"$LOG_DIR/full.struct.sql"
struct_dump "$BASE_DB" >"$LOG_DIR/base.struct.sql"
if ! diff -u "$LOG_DIR/full.struct.sql" "$LOG_DIR/base.struct.sql" >"$LOG_DIR/struct.diff"; then
  echo "── 结构差异（前 40 行）──" >&2
  head -40 "$LOG_DIR/struct.diff" >&2
  fail "基线库与全量库结构不一致：$LOG_DIR/struct.diff"
fi
echo "  ✓ 结构一致（月分区除外，deparse 已归一）"

content_fingerprints "$FULL_DB" >"$LOG_DIR/full.content.txt"
content_fingerprints "$BASE_DB" >"$LOG_DIR/base.content.txt"
if ! diff -u "$LOG_DIR/full.content.txt" "$LOG_DIR/base.content.txt" >"$LOG_DIR/content.diff"; then
  echo "── 系统数据内容差异（前 30 行）──" >&2
  head -30 "$LOG_DIR/content.diff" >&2
  fail "系统数据内容不一致：$LOG_DIR/content.diff"
fi
echo "  ✓ $(wc -l <"$LOG_DIR/full.content.txt" | tr -d ' ') 张表的行数与内容指纹一致（sys_baseline 除外）"

migrate_db "$BASE_DB" "$LOG_DIR/base-migrate-2.log"
struct_dump "$BASE_DB" >"$LOG_DIR/base.struct.2.sql"
if ! diff -q "$LOG_DIR/base.struct.sql" "$LOG_DIR/base.struct.2.sql" >/dev/null; then
  fail "基线库重复迁移后结构变化：$LOG_DIR/base.struct.2.sql"
fi
rows=$(db_psql "$BASE_DB" -c "SELECT count(*) FROM sys_baseline")
[ "$rows" = "1" ] || fail "重复迁移后 sys_baseline 行数为 $rows，应为 1"
echo "  ✓ 重复执行幂等（结构不变、sys_baseline 仍 1 行）"

echo "… 4/5 验证匹配哈希时被覆盖的迁移确实没执行"
db_psql "$BASE_DB" -c "DROP TABLE IF EXISTS public.$PROBE_TABLE" >/dev/null
[ "$(db_psql "$BASE_DB" -c "SELECT to_regclass('public.$PROBE_TABLE') IS NULL")" = "t" ] || fail "探针表未删除"
migrate_db "$BASE_DB" "$LOG_DIR/base-migrate-skip.log"
if [ "$(db_psql "$BASE_DB" -c "SELECT to_regclass('public.$PROBE_TABLE') IS NULL")" != "t" ]; then
  fail "匹配哈希时仍执行了基线内迁移：$PROBE_TABLE 被重建（日志：$LOG_DIR/base-migrate-skip.log）"
fi
echo "  ✓ 匹配哈希 → 基线内迁移未执行（$PROBE_TABLE 仍缺失）"

echo "… 5/5 验证哈希不匹配时明确拒绝（不静默重放历史迁移）"
# 回落全量看着「安全」，实际跑不通：历史迁移假定从空库开始（164 的 UPDATE 引用 updated_at，
# 而 223 已 DROP 它建的索引、201/205 已改名），在终态结构上重放必然半途报错。
# 正确行为是拒绝并指出处置办法，同时保持库不变。
db_psql "$BASE_DB" -c "UPDATE sys_baseline SET manifest_sha256 = repeat('0', 64)" >/dev/null
if GOWP_DATABASE_DBNAME="$BASE_DB" go run ./cmd -migrate-only >"$LOG_DIR/base-migrate-mismatch.log" 2>&1; then
  fail "哈希不匹配时迁移仍成功退出：历史迁移被静默重放（日志：$LOG_DIR/base-migrate-mismatch.log）"
fi
grep -q '基线产物已变更' "$LOG_DIR/base-migrate-mismatch.log" || \
  fail "哈希不匹配时没有给出「基线产物已变更」提示（日志：$LOG_DIR/base-migrate-mismatch.log）"
if [ "$(db_psql "$BASE_DB" -c "SELECT to_regclass('public.$PROBE_TABLE') IS NULL")" != "t" ]; then
  fail "哈希不匹配时仍重放了历史迁移：$PROBE_TABLE 被重建"
fi
echo "  ✓ 哈希不匹配 → 拒绝重放，库结构保持不变"

recorded=$(grep -oE "'[0-9a-f]{64}'" schema/baseline/init.sql | head -1 | tr -d "'")
computed=$(go run ./cmd/baseline-digest)
[ "$recorded" = "$computed" ] || fail "init.sql 记录 $recorded，当前产物摘要 $computed：重跑 bash scripts/baseline-generate.sh"
echo "  ✓ init.sql 的内容摘要与生成器一致（$(printf '%s' "$computed" | cut -c1-8)）"

echo "✓ 初始化基线验收通过（日志：$LOG_DIR）"
