#!/usr/bin/env bash
# baseline-adopt.sh — 把「已按本仓迁移建好」的现有库接入初始化基线。
#
# 接入的含义：往该库写一行 sys_baseline，登记基线产物的内容摘要（与迁移器的跳过判定同源）。
# 之后跑 -migrate-only 时迁移器会跳过基线覆盖的历史迁移与 seed（都已在库里），只执行段外增量。
#
# 安全默认：不加 --apply 只核验、不写库。核验不通过一律不写。
#   1. 库必须可连，且 sys_baseline 不存在或为空（已有水位说明接入过）；
#   2. 结构与「重放 schema/baseline/init.sql 再跑 -migrate-only 的临时库」一致
#      （两侧都是 pg_dump 输出，deparse 差异已归一；现有库混入的非迁移表会被点名）；
#   3. 系统数据：system_data.sql 里每张表的 COPY 行数不得多于现有库的行数（少了说明 seed 没跑全）；
#      再核必需业务键 —— 词条 (item_key, lang)、权限 (permission_code)、菜单 (非空 permission_code)
#      在基线里的每一个键都必须存在于现有库。行数够不代表键齐：缺一个必需键、同时多一条
#      无关记录，行数照样相等。
#
# 用法：
#   bash scripts/baseline-adopt.sh --db DBNAME                       # 只核验
#   bash scripts/baseline-adopt.sh --db DBNAME --apply               # 核验通过后写入水位
#   bash scripts/baseline-adopt.sh --db DBNAME --refresh --apply     # 基线产物变更后更新既有水位
#
# --refresh 只改已有水位行的摘要与版本，不做任何结构/数据写入；核验不通过同样不写。
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

MONTH_PARTITION='*_[0-9][0-9][0-9][0-9]_[0-9][0-9]'
BASELINE_DIR=schema/baseline
TARGET_DB=""
APPLY=0
REFRESH=0
while [ "$#" -gt 0 ]; do
  case "$1" in
    --db) TARGET_DB="$2"; shift 2 ;;
    --apply) APPLY=1; shift ;;
    --refresh) REFRESH=1; shift ;;
    -h|--help) sed -n '2,20p' "$0"; exit 0 ;;
    *) echo "✗ 未知参数：$1" >&2; exit 2 ;;
  esac
done
[ -n "$TARGET_DB" ] || { echo "✗ 需要 --db DBNAME" >&2; exit 2; }

TMP=$(mktemp -d)
REPLAY_DB="wp_baseline_adopt_replay_$$"
admin_psql() { psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d postgres -v ON_ERROR_STOP=1 "$@"; }
cleanup() {
  admin_psql -c "DROP DATABASE IF EXISTS \"$REPLAY_DB\" WITH (FORCE)" >/dev/null 2>&1 || true
  rm -rf "$TMP"
}
trap cleanup EXIT
fail() { echo "✗ $*" >&2; exit 1; }

db_psql() { psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$TARGET_DB" -tA -v ON_ERROR_STOP=1 "$@"; }
[ "$(db_psql -c 'SELECT 1')" = "1" ] || fail "连不上库 $TARGET_DB"

existing=$(db_psql -c "SELECT count(*) FROM sys_baseline" 2>/dev/null || echo "-")
if [ "$existing" = "-" ]; then
  echo "  · sys_baseline 不存在，本次将建立水位"
elif [ "$existing" -gt 1 ]; then
  fail "库 $TARGET_DB 有 $existing 行 sys_baseline，水位不唯一；先人工核对并保留一行"
elif [ "$existing" = "1" ]; then
  if [ "$REFRESH" -ne 1 ]; then
    fail "库 $TARGET_DB 已接入过基线；基线产物变更后用 --refresh 更新水位"
  fi
  echo "  · 已有水位，核验通过后将更新它（--refresh）"
fi

normalize() {
  sed -E \
    -e 's/\(\(ARRAY\[(.*)\]\)::text\[\]\)/(ARRAY[\1])/' \
    -e "s/\('([^']*)'::character varying\)::text/'\1'::character varying/g" \
    -e '/^-- Dumped (from|by|on) /d' \
    -e '/^\\(un)?restrict /d' \
    -e '/^SET (transaction_timeout|idle_in_transaction_session_timeout) =/d' \
    -e 's/[[:space:]]+$//'
}

echo "… 重放基线到临时库 $REPLAY_DB"
admin_psql -c "DROP DATABASE IF EXISTS \"$REPLAY_DB\" WITH (FORCE)" >/dev/null 2>&1 || true
admin_psql -c "CREATE DATABASE \"$REPLAY_DB\"" >/dev/null
if ! psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$REPLAY_DB" -v ON_ERROR_STOP=1 \
  -f "$BASELINE_DIR/init.sql" >"$TMP/replay-init.log" 2>&1; then
  tail -20 "$TMP/replay-init.log" >&2
  fail "重放 $BASELINE_DIR/init.sql 失败"
fi
if ! GOWP_DATABASE_DBNAME="$REPLAY_DB" go run ./cmd -migrate-only >"$TMP/replay-migrate.log" 2>&1; then
  tail -30 "$TMP/replay-migrate.log" >&2
  fail "重放库跑迁移失败"
fi

echo "… 核验结构（现有库 vs 重放库）"
struct_dump() {
  pg_dump -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$1" \
    --schema-only --no-owner --no-privileges --no-comments \
    --exclude-table="$MONTH_PARTITION" 2>/dev/null | normalize
}
tables_of() {
  grep -oE '^CREATE TABLE public\.[^ ]+' "$1" | sed -E 's/^CREATE TABLE public\."?([^"]*)"?/\1/' | sort -u
}
struct_dump "$REPLAY_DB" >"$TMP/replay.sql"
struct_dump "$TARGET_DB" >"$TMP/live.sql"
extra=$(comm -13 <(tables_of "$TMP/replay.sql") <(tables_of "$TMP/live.sql"))
if [ -n "$extra" ]; then
  echo "── 现有库多出的表（$(printf "%s\n" "$extra" | wc -l | tr -d " ") 张，非本仓迁移产物）──" >&2
  printf "%s\n" "$extra" | sed "s/^/  + /" >&2
  fail "库 $TARGET_DB 混入其他项目的对象，不是本仓迁移产物库，不能接入基线"
fi
if ! diff -u "$TMP/replay.sql" "$TMP/live.sql" >"$TMP/struct.diff"; then
  echo "── 结构差异（前 60 行）──" >&2
  head -60 "$TMP/struct.diff" >&2
  fail "库 $TARGET_DB 的结构与基线不一致，先跑 -migrate-only 补齐再接入"
fi
echo "  ✓ 结构一致"

echo "… 核验系统数据"
awk '
  /^COPY public\./ { name=$2; sub(/^public\./, "", name); cnt=0; in_copy=1; next }
  in_copy && /^\\\.$/ { print name "|" cnt; in_copy=0; next }
  in_copy { cnt++ }
' "$BASELINE_DIR/system_data.sql" >"$TMP/base.rows"
missing=0
while IFS='|' read -r tbl rows; do
  [ -n "$tbl" ] || continue
  # 计数查询失败必须当作核验不成立：旧写法 || echo 0 把「查不动」写成「0 行」，
  # 所有表都会被判成数据不足 —— 看着像拒绝，实际是错误被吞掉后的误报。
  if ! live=$(db_psql -c "SELECT count(*) FROM public.\"$tbl\""); then
    fail "统计 $tbl 行数失败，核验未成立，拒绝接入"
  fi
  if [ "$live" -lt "$rows" ]; then
    echo "  ! $tbl：基线 $rows 行，库里只有 $live 行" >&2
    missing=$((missing + 1))
  fi
done <"$TMP/base.rows"
if [ "$missing" -gt 0 ]; then
  fail "$missing 张系统表的行数少于基线，先跑 -migrate-only 补齐 seed 再接入"
fi
echo "  ✓ $(wc -l <"$TMP/base.rows" | tr -d ' ') 张系统表的行数不少于基线"

echo "… 核验必需业务键"
# 把基线重放库的键集合 COPY 出来，载入目标库的临时表后做 anti-join。
# COPY 文本格式处理转义，比在 bash 里解析 COPY 行可靠。
required_key_missing=0
key_check_failed=0
check_required_keys() {
  local tbl="$1" cols="$2" filter="${3:-}" cond out
  cond=$(printf '%s' "$cols" | awk -F, '{for(i=1;i<=NF;i++){printf "%st.%s=b.%s", (i>1?" AND ":""), $i, $i}}')
  if ! psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$REPLAY_DB" -tA -v ON_ERROR_STOP=1 \
    -c "COPY (SELECT $cols FROM public.$tbl) TO STDOUT" >"$TMP/$tbl.keys"; then
    echo "  ✗ $tbl：导出基线键失败（SQL 错误）" >&2
    key_check_failed=1
    return 1
  fi
  # -q 必须加：不加会把 CREATE TABLE AS / COPY n 这类状态标签混进输出，anti-join 结果为空时仍被判成缺键。
  if ! out=$(psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$TARGET_DB" -q -tA -v ON_ERROR_STOP=1 <<SQL
CREATE TEMP TABLE _baseline_keys AS SELECT $cols FROM public.$tbl WITH NO DATA;
\copy _baseline_keys FROM '$TMP/$tbl.keys'
SELECT b.* FROM _baseline_keys b
WHERE NOT EXISTS (SELECT 1 FROM public.$tbl t WHERE $cond) $filter;
SQL
  ); then
    echo "  ✗ $tbl：键存在性查询失败（SQL 错误）" >&2
    key_check_failed=1
    return 1
  fi
  if [ -n "$out" ]; then
    echo "  ! $tbl 缺少基线里的键（前 5 条）：" >&2
    printf '%s\n' "$out" | head -5 | sed 's/^/    /' >&2
    required_key_missing=$((required_key_missing + 1))
    return 1
  fi
  echo "  ✓ $tbl：基线键全部存在"
}
# 失败不能被 || true 吞掉：SQL 出错时 out 为空，anti-join 看起来「没有缺键」，
# 核验其实从未成立。函数自己记 key_check_failed，调用处只吞返回值。
check_required_keys sys_i18n item_key,lang || true
check_required_keys sys_permission permission_code || true
check_required_keys sys_menus permission_code 'AND b.permission_code IS NOT NULL' || true
if [ "$key_check_failed" -ne 0 ]; then
  fail "必需键核验过程中有 SQL 错误，核验未成立，拒绝接入"
fi
if [ "$required_key_missing" -gt 0 ]; then
  fail "$required_key_missing 张系统表缺少基线里的必需键，先跑 -migrate-only 补齐 seed 再接入"
fi

# 摘要必须与迁移器的跳过判定同源：自己算 manifest 哈希会与 ContentSHA256 漂移，
# 接入后每次迁移都匹配不上、静默回落全量。
sha="$(go run ./cmd/baseline-digest)"
version="adopt-$(date +%Y-%m-%d)-${sha:0:8}"

if [ "$APPLY" -ne 1 ]; then
  echo "… 核验通过（未写库）。--apply 将执行："
  if [ "$REFRESH" -eq 1 ] && [ "$existing" = "1" ]; then
    echo "    UPDATE sys_baseline SET version='$version', manifest_sha256='$sha', applied_at=now()"
  else
    echo "    INSERT INTO sys_baseline (version, manifest_sha256, applied_at, note)"
    echo "    VALUES ('$version', '$sha', now(), '现有库接入基线');"
  fi
  exit 0
fi

echo "… 写入水位"
db_psql -c "CREATE TABLE IF NOT EXISTS sys_baseline (
  id bigserial PRIMARY KEY,
  version text NOT NULL,
  manifest_sha256 char(64) NOT NULL,
  applied_at timestamptz NOT NULL DEFAULT now(),
  note text NOT NULL DEFAULT ''
)" >/dev/null
if [ "$REFRESH" -eq 1 ] && [ "$existing" = "1" ]; then
  db_psql -c "UPDATE sys_baseline SET version = '$version', manifest_sha256 = '$sha', applied_at = now(), note = '基线产物变更后更新水位'" >/dev/null
else
  db_psql -c "INSERT INTO sys_baseline (version, manifest_sha256, note) VALUES ('$version', '$sha', '现有库接入基线')" >/dev/null
fi
echo "✓ 已接入：$TARGET_DB → $version ($sha)"
MIGRATIONS=$(awk '/^\[migrations\]/{f=1;next} /^\[seeds\]/{f=0} f&&NF&&!/^#/{c++} END{print c+0}' "$BASELINE_DIR/manifest.txt")
SEEDS=$(awk '/^\[seeds\]/{f=1;next} f&&NF&&!/^#/{c++} END{print c+0}' "$BASELINE_DIR/manifest.txt")
echo "  之后跑 ./app -migrate-only 会跳过基线覆盖的 $MIGRATIONS 条迁移与 $SEEDS 条 seed，只执行段外增量"
