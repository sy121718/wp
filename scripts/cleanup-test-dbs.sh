#!/usr/bin/env bash
# 清理本仓留下的临时/探针库。默认只列名单与体积，--apply 才执行 DROP DATABASE。
#
# 模式：
#   默认       夹具随机后缀库：rls_t_* / t_* / go_test_* / wp_test_tpl_*
#   --probes   历轮审计与验证留下的一次性探针库：wp_ 前缀（保留 wp_test* 测试主库）
#
# 任何模式都不碰业务库（config.yaml 的 database.dbname）、postgres 与 artex 系列
# （artex 属其他项目，artex_mig2 是本仓的迁移验收库）。
set -euo pipefail
cd "$(dirname "$0")/.."

APPLY=0
MODE=test
for arg in "$@"; do
  case "$arg" in
    --apply)  APPLY=1 ;;
    --probes) MODE=probes ;;
    *) echo "用法：$0 [--probes] [--apply]" >&2; exit 2 ;;
  esac
done

DB_USER="${GOWP_DB_USER:-${PGUSER:-sky}}"
DB_HOST="${GOWP_DB_HOST:-127.0.0.1}"
DB_PORT="${GOWP_DB_PORT:-5432}"

yaml_dbname() {
  [ -f config.yaml ] || return 0
  sed -n 's/^  dbname: *\(.*\)$/\1/p' config.yaml | head -1
}
BUSINESS_DB="${GOWP_DATABASE_DBNAME:-$(yaml_dbname)}"
BUSINESS_DB="${BUSINESS_DB:-wp}"

if [ "$MODE" = probes ]; then
  PATTERN="^wp_"
  EXCLUDE_REGEX="^wp_test"
  LABEL="探针库"
else
  PATTERN="^(rls_t_|t_|go_test_|wp_test_tpl_)"
  EXCLUDE_REGEX="^$"
  LABEL="测试临时库"
fi

query() {
  psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d postgres -At -c "$1"
}

mapfile -t DB_LIST < <(query "select datname from pg_database
  where datname ~ '$PATTERN'
    and datname !~ '$EXCLUDE_REGEX'
    and datname <> '$BUSINESS_DB'
    and datname not in ('postgres', 'template0', 'template1')
  order by 1")

if [ "${#DB_LIST[@]}" -eq 0 ]; then
  echo "✓ 没有需要清理的$LABEL"
  exit 0
fi

TOTAL_SIZE=$(query "select pg_size_pretty(coalesce(sum(pg_database_size(datname)), 0))
  from pg_database where datname = any (array[$(printf "'%s'," "${DB_LIST[@]}" | sed 's/,$//')])")

echo "候选$LABEL：${#DB_LIST[@]} 个，合计 $TOTAL_SIZE"
printf '  %s\n' "${DB_LIST[@]}" | head -20
[ "${#DB_LIST[@]}" -gt 20 ] && echo "  …（其余 $((${#DB_LIST[@]} - 20)) 个省略）"

if [ "$APPLY" -ne 1 ]; then
  echo
  echo "只是列名单。确认后加 --apply 执行 DROP DATABASE。"
  exit 0
fi

DROPPED=0
for db in "${DB_LIST[@]}"; do
  if query "drop database if exists \"$db\" with (force)" >/dev/null 2>&1; then
    DROPPED=$((DROPPED + 1))
  else
    echo "✗ 删除失败：$db（可能仍有活动连接）" >&2
  fi
done
echo "✓ 已删除 $DROPPED / ${#DB_LIST[@]} 个$LABEL"
