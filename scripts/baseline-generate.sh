#!/usr/bin/env bash
# baseline-generate.sh — 生成初始化基线产物（schema/baseline/）。
#
# 产物是同一批、缺一不可（判据见 docs/initial-baseline.md 第 3 节）：
#   structure.sql   非分区对象全量 + 3 张分区父表与其 default 分区，剔除全部 _YYYY_MM 月分区
#   system_data.sql 迁移与 seed 都跑完后的系统数据（词条/权限/菜单/字典/默认配置/初始授权），不含业务数据
#   manifest.txt    基线覆盖的两段 version（[migrations] / [seeds]），段内顺序即执行顺序
#   init.sql        \ir structure.sql → \ir system_data.sql → 写 sys_baseline 一行
#
# 基线导的是**迁移与 seed 都跑完**的终态：重放基线的库不再重跑历史 seed，
# 两边输入相同就不存在「seed 条件判断因新键已存在而跳过」的漏项。
# 旧版导「seed 前」状态正是为了绕开它，代价是 440 条历史 seed 仍要全部重放；
# 冻结前缀 + 导终态才是真收敛。
#
# 来源必须是「只跑过本仓 public/migrations 的库」：config.yaml 的业务库混着别的应用对象。
# 默认自建临时库跑全量迁移（约两分钟）；复用已有终态库时给 GOWP_BASELINE_DB。
#
# 用法：
#   bash scripts/baseline-generate.sh            # 生成/更新产物
#   bash scripts/baseline-generate.sh --check    # 只比对，过期则非零退出（CI 用）
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

yaml_db() {
  [ -f config.yaml ] || return 0
  awk -v key="$1" '/^database:/{in_db=1;next} /^[a-zA-Z]/{in_db=0} in_db && $1==key":"{print $2;exit}' config.yaml
}

DB_HOST="${DB_HOST:-${GOWP_DB_HOST:-$(yaml_db host)}}"
DB_HOST="${DB_HOST:-127.0.0.1}"
DB_PORT="${DB_PORT:-${GOWP_DB_PORT:-$(yaml_db port)}}"
DB_PORT="${DB_PORT:-5432}"
DB_USER="${DB_USER:-${GOWP_DB_USER:-$(yaml_db user)}}"
export PGPASSWORD="${DB_PASSWORD:-${GOWP_DB_PASSWORD:-$(yaml_db password)}}"

BASELINE_DIR="schema/baseline"
ADMIN_DB="${GOWP_ADMIN_DB:-postgres}"
CHECK=0
[ "${1:-}" = "--check" ] && CHECK=1

# 月分区名一律形如 <父表>_YYYY_MM；default 分区不匹配这个模式，必须保留。
MONTH_PARTITION='*_[0-9][0-9][0-9][0-9]_[0-9][0-9]'
# 时间字面量归一：与 schema/baseline/baseline.go 的 tsLiteralRe 必须逐字等价。
TS_RE='s/[0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]+)?\+[0-9]{2}/<TS>/g'

command -v pg_dump >/dev/null 2>&1 || { echo "✗ 需要 pg_dump" >&2; exit 1; }
command -v psql >/dev/null 2>&1 || { echo "✗ 需要 psql" >&2; exit 1; }
command -v go >/dev/null 2>&1 || { echo "✗ 需要 go（要跑迁移与 manifest）" >&2; exit 1; }

BASELINE_DB="${GOWP_BASELINE_DB:-}"
PROBE=0
cleanup_probe() { :; }
if [ -z "$BASELINE_DB" ]; then
  BASELINE_DB="wp_baseline_probe_$$"
  PROBE=1
  PROBE_LOG="/tmp/wp-baseline-probe-migrate.log"
  cleanup_probe() {
    psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$ADMIN_DB" \
      -c "DROP DATABASE IF EXISTS \"$BASELINE_DB\" WITH (FORCE)" >/dev/null || \
      echo "⚠ 临时库 $BASELINE_DB 未能自动删除，请手动 DROP DATABASE" >&2
  }
  trap cleanup_probe EXIT
  psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$ADMIN_DB" -v ON_ERROR_STOP=1 \
    -c "CREATE DATABASE \"$BASELINE_DB\"" >/dev/null
  echo "… 在 $BASELINE_DB 执行全量迁移与 seed（约两分钟）"
  # 导 seed 后终态：基线已覆盖历史 seed，重放基线的库不再重跑它们。
  if ! GOWP_DATABASE_DBNAME="$BASELINE_DB" go run ./cmd -migrate-only >"$PROBE_LOG" 2>&1; then
    echo "✗ 迁移失败，日志：$PROBE_LOG；临时库 $BASELINE_DB 已保留便于排查" >&2
    trap - EXIT
    exit 1
  fi
fi

# 只剥「每次生成都会变、但与内容无关」的行：生成时间、pg_dump 的随机 \restrict 标记。
# 注意这里**不清理行尾空白**：数据 dump 里行尾的 tab 是「最后几个字段为空字符串」的
# 分隔符，删掉会让 COPY 的字段数变少（实测 mail_templates 少两个 tab 直接加载失败）。
strip_common_noise() {
  sed -E \
    -e '/^-- Dumped (from|by) /d' \
    -e '/^-- Dumped on /d' \
    -e '/^\\restrict /d' \
    -e '/^\\unrestrict /d' \
    -e '/^SET (transaction_timeout|idle_in_transaction_session_timeout) =/d' \
    -e "s|^SELECT pg_catalog.set_config\('search_path', '', false\);$|SET search_path = public, ext_shared;|"
}

dump_structure() {
  pg_dump -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$BASELINE_DB" \
    --schema-only --no-owner --no-privileges --no-comments \
    --exclude-table="$MONTH_PARTITION" | strip_common_noise | sed -E -e 's/[[:space:]]+$//'
}

# 月分区没有数据，排除它们只是防止「将来某个月写进基线」。
dump_system_data() {
  pg_dump -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$BASELINE_DB" \
    --data-only --no-owner --no-privileges --no-comments \
    --exclude-table="$MONTH_PARTITION" \
    --exclude-table="sys_baseline" | strip_common_noise
}

# 两段 version：先 [migrations]，再 [seeds]。
dump_manifest() {
  go run ./cmd/baseline-manifest
}

# 产物内容摘要 —— 与 schema/baseline/baseline.go 的 ContentSHA256 必须逐字节等价：
#   manifest 原文 → "\n--structure--\n" → structure.sql（剥生成时间）
#   → "\n--system-data--\n" → system_data.sql（剥生成时间、时间字面量归一）
# 时间字面量是生成时刻的 now()，不归一则每次重生成摘要都变、已接入库白白回落全量。
content_sha() {
  local dir="$1"
  {
    cat "$dir/manifest.txt"
    printf '\n--structure--\n'
    grep -v '^-- 生成时间：' "$dir/structure.sql" || true
    printf '\n--system-data--\n'
    grep -v '^-- 生成时间：' "$dir/system_data.sql" | sed -E "$TS_RE"
  } | sha256sum | awk '{print $1}'
}

# 四份产物一起生成到目标目录；--check 时生成到临时目录再逐字节比对。
emit() {
  local dir="$1"
  mkdir -p "$dir"
  {
    echo "-- 初始化基线：结构（由 scripts/baseline-generate.sh 生成，请勿手改）。"
    echo "-- 不含任何 _YYYY_MM 月分区：月窗口由 partition.EnsureAhead 在初始化末尾补齐。"
    echo "-- 生成时间：$(date -Iseconds)"
    echo
    dump_structure
  } >"$dir/structure.sql"
  {
    echo "-- 初始化基线：系统数据（由 scripts/baseline-generate.sh 生成，请勿手改）。"
    echo "-- 迁移与 seed 都跑完后的系统数据；不含用户、订单、密钥、自定义译文等业务数据。"
    echo "-- 生成时间：$(date -Iseconds)"
    echo
    dump_system_data
  } >"$dir/system_data.sql"
  {
    echo "# 初始化基线覆盖的集合（由 scripts/baseline-generate.sh 生成，请勿手改）。"
    echo "# 两段都是注册表的前缀：段内顺序即执行顺序，段外的新版本照常执行。"
    echo "# 跳过判定看 sys_baseline.manifest_sha256 与产物内容摘要是否一致（不是本文件自身的哈希）。"
    dump_manifest
  } >"$dir/manifest.txt"
  local sha version
  sha="$(content_sha "$dir")"
  version="baseline-${sha:0:8}"
  {
    echo "-- 初始化基线载入脚本：结构 + 系统数据 + 台账一行。"
    echo "-- 用法：psql -v ON_ERROR_STOP=1 -d <新库> -f schema/baseline/init.sql"
    echo "-- 载入后再跑 ./app -migrate-only：迁移器会跳过基线覆盖的迁移与 seed，只执行段外增量。"
    echo "\\set ON_ERROR_STOP on"
    echo "-- pg_dump 会把 search_path 清空（防恢复期受它影响），而触发器函数用未限定表名；"
    echo "-- 载入系统数据前必须把 search_path 设回来，否则 COPY 触发触发器时报 relation 不存在。"
    echo "SET search_path = public, ext_shared;"
    echo "\\ir structure.sql"
    echo "SET search_path = public, ext_shared;"
    echo "\\ir system_data.sql"
    echo
    echo "INSERT INTO sys_baseline (version, manifest_sha256, applied_at, note)"
    echo "VALUES ('$version', '$sha', now(), '由 scripts/baseline-generate.sh 生成');"
  } >"$dir/init.sql"
}

if [ "$CHECK" -eq 1 ]; then
  TMP="$(mktemp -d)"
  trap 'rm -rf "$TMP"; cleanup_probe' EXIT
  emit "$TMP"
  status=0
  for f in structure.sql system_data.sql manifest.txt init.sql; do
    case "$f" in
      *.sql)
        # structure/system_data 头部带生成时间，比对时只比正文。
        grep -v '^-- 生成时间：' "$BASELINE_DIR/$f" >"$TMP/old.$f" 2>/dev/null || true
        grep -v '^-- 生成时间：' "$TMP/$f" >"$TMP/new.$f"
        if [ "$f" = "system_data.sql" ]; then
          # 时间列的值是生成时刻的 now()，不是内容；比较前归一成占位，否则每次生成都报过期。
          sed -E -i "$TS_RE" "$TMP/old.$f" "$TMP/new.$f"
        fi
        if ! diff -u "$TMP/old.$f" "$TMP/new.$f" >"$TMP/diff.$f"; then
          echo "✗ $BASELINE_DIR/$f 已过期" >&2
          head -40 "$TMP/diff.$f" >&2
          status=1
        fi
        ;;
      *)
        # init.sql 不归一：版本号与摘要都随内容变，内容一致时它必须逐字节一致。
        if ! diff -u "$BASELINE_DIR/$f" "$TMP/$f" >"$TMP/diff.$f" 2>/dev/null; then
          echo "✗ $BASELINE_DIR/$f 已过期" >&2
          head -20 "$TMP/diff.$f" >&2
          status=1
        fi
        ;;
    esac
  done
  if [ "$status" -eq 0 ]; then
    echo "✓ 基线与当前迁移结果一致"
  fi
  exit "$status"
fi

emit "$BASELINE_DIR"
TABLES=$(grep -c '^CREATE TABLE' "$BASELINE_DIR/structure.sql" || true)
ROWS=$(grep -c '^COPY ' "$BASELINE_DIR/system_data.sql" || true)
MIGRATIONS=$(awk '/^\[migrations\]/{f=1;next} /^\[seeds\]/{f=0} f&&NF&&!/^#/{c++} END{print c+0}' "$BASELINE_DIR/manifest.txt")
SEEDS=$(awk '/^\[seeds\]/{f=1;next} f&&NF&&!/^#/{c++} END{print c+0}' "$BASELINE_DIR/manifest.txt")
echo "✓ 已生成 $BASELINE_DIR：$TABLES 张表 / $MIGRATIONS 条迁移 + $SEEDS 条 seed / $ROWS 张有数据的表"
