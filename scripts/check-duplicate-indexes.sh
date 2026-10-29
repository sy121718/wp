#!/usr/bin/env bash
# check-duplicate-indexes.sh — 找出**前缀重复**的索引（B08 / DB-11 的只读前置）。
#
# 为什么只报告不删除：删索引是不可逆的运维动作，而"看起来冗余"有两种完全不同的成因：
#
#   · 真冗余：`idx(a)` 与 `idx(a,b)` 并存，前者能被后者完全覆盖（B-tree 前缀可用）。
#   · 假冗余：`idx(a)` 与 `idx(a,b)` 看起来是前缀关系，但前者是 UNIQUE、
#     或者是某个部分索引（WHERE）、或者表达式索引 —— 语义不同，删了就丢约束。
#
# 而且**是否该删**还要看两件真实数据：
#   ① 各索引的实际扫描次数（pg_stat_user_indexes.idx_scan）；
#   ② 写入放大（每个多余索引都让每次 INSERT/UPDATE 多维护一棵树）。
# 两样都拿不到时（比如本机库没有生产流量），唯一负责的做法是**报告 + 留给人判断**，
# 而不是按规则自动 DROP。任务书 §3.3 写明"没有生产行数、索引使用率、执行计划时，
# 不承诺索引收益"。
#
# 用法：
#   bash scripts/check-duplicate-indexes.sh            # 只报告（退出码恒 0）
#   bash scripts/check-duplicate-indexes.sh --strict   # 有前缀重复即退出码 1（CI 收紧时用）
#
# 连接参数与其它 DB 脚本一致：来自 config.yaml，可用 PG* 环境变量覆盖。

set -uo pipefail
cd "$(dirname "$0")/.."

STRICT=0
[[ "${1:-}" == "--strict" ]] && STRICT=1

if ! command -v psql >/dev/null 2>&1; then
  echo "· psql 不可用，跳过重复索引巡检"
  exit 0
fi

# 从 config.yaml 的 **database 段**读连接信息。
#
# 必须限定段：`port:` 在 server(8080) / redis(6379) / database(5432) 三处都有，
# 全局 grep 第一条会拿到 8080（实测踩过：脚本报"127.0.0.1:8080/wp 不可用"）。
cfg_get() {
  local key="$1" fallback="$2"
  local val
  val=$(awk -v k="$key" '
    /^database:[[:space:]]*$/ { in_db = 1; next }
    in_db && /^[^[:space:]#]/ { in_db = 0 }
    in_db && $0 ~ "^[[:space:]]+" k ":" {
      sub("^[[:space:]]+" k ":[[:space:]]*", "")
      sub("[[:space:]]+#.*$", "")
      gsub(/^"|"$/, "")
      print
      exit
    }
  ' config.yaml 2>/dev/null)
  [[ -z "$val" ]] && val="$fallback"
  printf '%s' "$val"
}

export PGHOST="${PGHOST:-$(cfg_get host 127.0.0.1)}"
export PGPORT="${PGPORT:-$(cfg_get port 5432)}"
export PGUSER="${PGUSER:-$(cfg_get user root)}"
export PGPASSWORD="${PGPASSWORD:-$(cfg_get password '')}"
export PGDATABASE="${PGDATABASE:-$(cfg_get dbname wp)}"

if ! psql -q -d "$PGDATABASE" -c 'SELECT 1' >/dev/null 2>&1; then
  # 连不上不等于代码有问题：与其它 DB 脚本同口径，明确说明而不是静默成功。
  echo "· PostgreSQL 不可用（$PGHOST:$PGPORT/$PGDATABASE），跳过重复索引巡检"
  exit 0
fi

# 判据：同一张表上，索引 A 的列是索引 B 的**严格前缀**，且 A 不是 UNIQUE / 不是部分索引
# / 不是表达式索引 —— 这三类语义不同，不能算被覆盖。
#
# 用 pg_index 的 indkey（列号数组）与 indisunique / indpred / indexprs 判定，而不是
# 解析 indexdef 文本：文本解析在下标 DESC / NULLS 顺序 / 操作符类不同时都会误判。
read -r -d '' SQL <<'SQL'
WITH idx AS (
  SELECT
    c.relname AS tablename,
    i.relname AS indexname,
    x.indisunique,
    (x.indpred IS NOT NULL) AS is_partial,
    (x.indexprs IS NOT NULL) AS is_expr,
    -- indkey 是 int2vector，转成 int2[] 后**下标从 0 开始**（实测 [0:1]={2,4}）。
    -- 直接对切片比较会永远为 false（[1:2] 取到的是第二个元素与越界），
    -- 所以先用 array_agg 归一到下标从 1 开始的普通数组。
    (SELECT array_agg(k ORDER BY ord) FROM unnest(x.indkey) WITH ORDINALITY AS t(k, ord))::int2[] AS cols
  FROM pg_index x
  JOIN pg_class c ON c.oid = x.indrelid
  JOIN pg_class i ON i.oid = x.indexrelid
  JOIN pg_namespace n ON n.oid = c.relnamespace
  WHERE n.nspname = 'public'
)
SELECT
  a.tablename,
  a.indexname AS redundant,
  b.indexname AS covering
FROM idx a
JOIN idx b
  ON a.tablename = b.tablename
 AND a.indexname <> b.indexname
 -- a 的列是 b 的严格前缀（b 更长）
 AND array_length(a.cols, 1) < array_length(b.cols, 1)
 AND a.cols = b.cols[1:array_length(a.cols, 1)]
 -- 三类语义不同，排除
 AND NOT a.indisunique
 AND NOT a.is_partial
 AND NOT a.is_expr
 AND NOT b.is_partial
 AND NOT b.is_expr
ORDER BY a.tablename, a.indexname;
SQL

rows=$(psql -q -At -F $'\t' -d "$PGDATABASE" -c "$SQL" 2>/dev/null)

if [[ -z "$rows" ]]; then
  printf '✓ 无前缀重复索引（public schema）\n'
  exit 0
fi

count=$(printf '%s\n' "$rows" | grep -c .)
echo "发现 $count 条**前缀重复**索引（只报告，不自动删除）："
echo
printf '%s\n' "$rows" | while IFS=$'\t' read -r table redundant covering; do
  # 扫描次数：stats collector 可能还没为这个索引建过行（新建索引 / 库刚起来），
  # 那时明确报"未采集"，而不是填 0 —— 0 会被读成"从来没被用过，可以删"。
  scans=$(psql -q -At -d "$PGDATABASE" -c \
    "SELECT idx_scan FROM pg_stat_user_indexes WHERE relname='$redundant'" 2>/dev/null)
  if [[ -z "$scans" ]]; then
    scans='未采集'
  else
    scans="${scans} 次"
  fi
  printf '  %s\n    冗余候选: %s（扫描 %s）\n    已被覆盖: %s\n' \
    "$table" "$redundant" "$scans" "$covering"
done
echo
echo "处理顺序（每条都要有依据，不要批量 DROP）："
echo "  0. **覆盖者是 UNIQUE 索引时额外想一步**：唯一索引能被普通查询用作前缀扫描，"
echo "     但它同时承担唯一性约束 —— 判断方向必须是「删前者、留后者」，反过来会丢约束。"
echo "  1. 在目标环境查 pg_stat_user_indexes.idx_scan：仍被使用的索引删了会变慢；"
echo "  2. 用真实数据的 EXPLAIN (ANALYZE, BUFFERS) 确认覆盖索引真的被选中；"
echo "  3. 确认没有唯一性/部分索引语义被顺带丢掉；"
echo "  4. 以版本化迁移删除（不手改历史迁移），并写下删除依据。"
echo
echo "依据：任务书 §3.3「没有生产行数、索引使用率、执行计划时，不承诺索引收益」。"

if [[ $STRICT -eq 1 ]]; then
  exit 1
fi
exit 0
