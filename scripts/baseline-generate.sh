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
#   bash scripts/baseline-generate.sh                  # 生成/更新产物
#   bash scripts/baseline-generate.sh --check          # 比对：前缀 + 自洽 +（无新增版本时）全量重生成
#   bash scripts/baseline-generate.sh --check --static # 只做无库检查：前缀 + 摘要自洽（check-all 用）
#
# --check 的两种口径（评审 2026-11-15 指出旧版把「新增迁移」误报成「基线过期」）：
#   1. 冻结清单必须是当前注册表的前缀 —— 段外的新版本按设计不进基线，照常执行；
#   2. init.sql 里登记的摘要必须与产物内容一致 —— 手改 SQL 而不重新生成会被这条抓住；
#   3. 仅当注册表等于冻结清单时，才自建临时库重跑全量并逐份比对结构/系统数据。
#      有段外新增版本时不比：「基线＋增量」与历史全量的终态等价由
#      scripts/baseline-verify.sh 判定，不能要求每加一条迁移就重生成基线、刷所有库水位。
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
STATIC=0
for arg in "$@"; do
  case "$arg" in
    --check) CHECK=1 ;;
    --static) STATIC=1 ;;
    *) echo "✗ 未知参数：$arg" >&2; exit 2 ;;
  esac
done
if [ "$STATIC" -eq 1 ] && [ "$CHECK" -eq 0 ]; then
  echo "✗ --static 只能和 --check 一起用" >&2
  exit 2
fi

# 月分区名一律形如 <父表>_YYYY_MM；default 分区不匹配这个模式，必须保留。
MONTH_PARTITION='*_[0-9][0-9][0-9][0-9]_[0-9][0-9]'
# 时间字面量归一：与 schema/baseline/baseline.go 的 tsLiteralRe 必须逐字等价。
TS_RE='s/[0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]+)?\+[0-9]{2}/<TS>/g'

# pg_dump / psql 只在真正建库比对时才需要：--check --static 是纯文件检查，
# 必须在没有 PostgreSQL 客户端的 CI unit job 里也能跑。
command -v go >/dev/null 2>&1 || { echo "✗ 需要 go（要跑迁移与 manifest）" >&2; exit 1; }

BASELINE_DB="${GOWP_BASELINE_DB:-}"
PROBE=0
cleanup_probe() { :; }
# 只有真正要比对产物（生成 / --check 且无段外新增版本）时才建临时库：
# 静态检查不该付两分钟的迁移代价，更不该因此要求本地有库。
ensure_probe() {
  command -v pg_dump >/dev/null 2>&1 || { echo "✗ 需要 pg_dump" >&2; exit 1; }
  command -v psql >/dev/null 2>&1 || { echo "✗ 需要 psql" >&2; exit 1; }
  [ -n "$BASELINE_DB" ] && return 0
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
}

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
  status=0

  # 1) 冻结清单必须是当前注册表的前缀。段外新增版本按设计不进基线、照常执行 ——
  #    旧版拿「当前全量生成的清单」与冻结清单逐字节比，于是每加一条迁移都报过期，
  #    与「新增迁移不用重生成基线」的维护约定直接冲突。
  prefix=""
  if ! prefix=$(go run ./cmd/baseline-manifest -check-prefix "$BASELINE_DIR/manifest.txt" 2>"$TMP/prefix.err"); then
    echo "✗ $BASELINE_DIR/manifest.txt 与当前注册表不一致：" >&2
    cat "$TMP/prefix.err" >&2
    status=1
  fi
  extras=0
  [ -n "$prefix" ] && extras=$(printf '%s\n' "$prefix" | sed -n 's/.*extras=\([0-9]*\).*/\1/p')

  # 2) init.sql 登记的摘要与版本必须与产物内容自洽：手改 SQL 而不重新生成会被这条抓住，
  #    不必为此重跑两分钟的迁移。
  want_sha="$(content_sha "$BASELINE_DIR")"
  got="$(sed -n "s/^VALUES ('\([^']*\)', '\([0-9a-f]*\)', now(),.*/\1 \2/p" "$BASELINE_DIR/init.sql")"
  if [ -z "$got" ]; then
    echo "✗ $BASELINE_DIR/init.sql 里找不到登记摘要的 VALUES 行" >&2
    status=1
  else
    got_version="${got%% *}"
    got_sha="${got##* }"
    if [ "$got_sha" != "$want_sha" ]; then
      echo "✗ $BASELINE_DIR/init.sql 的摘要与产物内容不一致（登记 ${got_sha:0:8}，实际 ${want_sha:0:8}）" >&2
      status=1
    fi
    if [ "$got_version" != "baseline-${want_sha:0:8}" ]; then
      echo "✗ $BASELINE_DIR/init.sql 的版本号应为 baseline-${want_sha:0:8}，实际 $got_version" >&2
      status=1
    fi
  fi

  # 3) 只有注册表等于冻结清单时，才重跑全量逐份比对（抓「基线 SQL 内容漂移」）；
  #    有段外新增版本时，终态等价由 baseline-verify.sh 判定，不在这里要求重生成。
  if [ "$STATIC" -eq 1 ]; then
    echo "· --static：跳过全量重生成比对（需要时跑 bash scripts/baseline-generate.sh --check）"
  elif [ "$extras" -gt 0 ]; then
    echo "· 基线之后有 $extras 条新版本（按设计不在基线内，照常执行），跳过全量重生成比对"
    echo "  终态等价（基线＋增量 == 历史全量）由 bash scripts/baseline-verify.sh 判定"
  else
    ensure_probe
    emit "$TMP"
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
  fi

  if [ "$status" -eq 0 ]; then
    echo "✓ 基线自洽：${prefix:-清单不可用}，摘要 ${want_sha:0:8}"
  fi
  exit "$status"
fi

emit "$BASELINE_DIR"
TABLES=$(grep -c '^CREATE TABLE' "$BASELINE_DIR/structure.sql" || true)
ROWS=$(grep -c '^COPY ' "$BASELINE_DIR/system_data.sql" || true)
MIGRATIONS=$(awk '/^\[migrations\]/{f=1;next} /^\[seeds\]/{f=0} f&&NF&&!/^#/{c++} END{print c+0}' "$BASELINE_DIR/manifest.txt")
SEEDS=$(awk '/^\[seeds\]/{f=1;next} f&&NF&&!/^#/{c++} END{print c+0}' "$BASELINE_DIR/manifest.txt")
echo "✓ 已生成 $BASELINE_DIR：$TABLES 张表 / $MIGRATIONS 条迁移 + $SEEDS 条 seed / $ROWS 张有数据的表"
