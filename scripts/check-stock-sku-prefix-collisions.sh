#!/usr/bin/env bash
# check-stock-sku-prefix-collisions.sh — 迁移 262（剥仓库 SKU 前缀）的**只读**预检。
#
# 为什么需要它：迁移 262 会把 inventory_stocks.sku_code 上多余的仓码前缀剥掉
# （口径：仓库里的 SKU 永远是裸码，商品侧才带前缀）。若剥完之后同一个
# (warehouse_id, sku_code) 撞出两行，迁移会 RAISE EXCEPTION 整体失败 —— 那发生在
# **应用启动时**，属于最糟糕的暴露时机。本脚本把同一套判定提前跑一遍：
# 上线前就能看到「会剥成什么、哪些会撞、撞的是哪几行」，而不是等启动炸。
#
# 只读：本脚本只跑 SELECT，一个字节都不改。冲突的处理方式是**打回给人拍**
#（改哪一行、留哪一行由人决定）—— 迁移与脚本都**不做**「加个后缀让它过」这类
# 自动改码，那是数据篡改，两行库存是两份事实。
#
# 判定口径与迁移 262 逐字一致（三处谓词必须相同；改一处就要同步改另两处与迁移）：
#   length(s.sku_code) > length(w.code) + 1
#   AND upper(left(s.sku_code, length(w.code))) = upper(w.code)
#   AND substr(s.sku_code, length(w.code) + 1, 1) = '_'
# 且都限定 current_schema()（并发 / 残留 schema 里同名表会让判定串味）。
#
# 用法：
#   bash scripts/check-stock-sku-prefix-collisions.sh
#   退出码 0 = 无冲突可安全跑迁移；1 = 有冲突（明细已在上面打印），先去人工处理。
#
# 依赖：psql；config.yaml 的 database 段可读（脚本里不另存口令）。
set -euo pipefail
cd "$(dirname "$0")/.."

if ! command -v psql >/dev/null 2>&1; then
    echo "缺少 psql，无法预检库存 SKU 前缀冲突。" >&2
    exit 1
fi

# 从 config.yaml 的 database 段取值（与 check-permission-gaps.sh / index-usage-report.sh 同一手法）。
yaml_db() {
    awk -v key="$1" '
        /^database:/ { in_db = 1; next }
        /^[a-zA-Z]/ { in_db = 0 }
        in_db && $1 == key":" { print $2; exit }
    ' config.yaml
}

DB_HOST="${DB_HOST:-$(yaml_db host)}"
DB_PORT="${DB_PORT:-$(yaml_db port)}"
DB_USER="${DB_USER:-$(yaml_db user)}"
DB_PASS="${DB_PASSWORD:-$(yaml_db password)}"
DB_NAME="${DB_NAME:-$(yaml_db dbname)}"

psql_run() {
    PGPASSWORD="$DB_PASS" psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -v ON_ERROR_STOP=1 "$@"
}

echo "== 库存 SKU 仓码前缀预检（迁移 262）=="
echo "库：$DB_NAME@$DB_HOST:$DB_PORT   schema：$(psql_run -tAc 'SELECT current_schema()')"
echo

echo "── ① 会被剥掉前缀的库存行（迁移 262 将改写的行）──"
psql_run <<'SQL'
SELECT w.code                                        AS warehouse_code,
       s.sku_code                                    AS original_sku,
       substr(s.sku_code, length(w.code) + 2)        AS stripped_sku,
       s.id                                          AS stock_id,
       s.product_id,
       s.variant_id,
       s.quantity,
       s.create_time
  FROM inventory_stocks s
  JOIN inventory_warehouses w ON w.id = s.warehouse_id
 WHERE length(s.sku_code) > length(w.code) + 1
   AND upper(left(s.sku_code, length(w.code))) = upper(w.code)
   AND substr(s.sku_code, length(w.code) + 1, 1) = '_'
 ORDER BY w.code, stripped_sku, s.id;
SQL
echo

echo "── ② 剥完后会撞成同一 (warehouse_id, sku_code) 的组（迁移会因此中止）──"
psql_run <<'SQL'
WITH stripped AS (
    SELECT s.id, s.warehouse_id, s.product_id, s.variant_id,
           s.sku_code AS original_sku, s.quantity, s.create_time,
           w.code AS warehouse_code,
           CASE WHEN length(s.sku_code) > length(w.code) + 1
                 AND upper(left(s.sku_code, length(w.code))) = upper(w.code)
                 AND substr(s.sku_code, length(w.code) + 1, 1) = '_'
                THEN substr(s.sku_code, length(w.code) + 2)
                ELSE s.sku_code
           END AS stripped_sku
      FROM inventory_stocks s
      JOIN inventory_warehouses w ON w.id = s.warehouse_id
)
SELECT warehouse_code,
       stripped_sku,
       COUNT(*) AS rows_in_group,
       string_agg(
           format('id=%s product_id=%s variant_id=%s 原 sku_code=%s 数量=%s 创建时间=%s',
                  id, product_id, variant_id, original_sku, quantity, create_time),
           E'\n' ORDER BY create_time, id) AS involved_rows
  FROM stripped
 GROUP BY warehouse_id, warehouse_code, stripped_sku
HAVING COUNT(*) > 1
 ORDER BY warehouse_code, stripped_sku;
SQL
echo

COLLISIONS=$(psql_run -tA <<'SQL'
WITH stripped AS (
    SELECT s.id, s.warehouse_id, s.sku_code AS original_sku, w.code AS warehouse_code,
           CASE WHEN length(s.sku_code) > length(w.code) + 1
                 AND upper(left(s.sku_code, length(w.code))) = upper(w.code)
                 AND substr(s.sku_code, length(w.code) + 1, 1) = '_'
                THEN substr(s.sku_code, length(w.code) + 2)
                ELSE s.sku_code
           END AS stripped_sku
      FROM inventory_stocks s
      JOIN inventory_warehouses w ON w.id = s.warehouse_id
)
SELECT COUNT(*) FROM (
    SELECT 1 FROM stripped
     GROUP BY warehouse_id, stripped_sku
    HAVING COUNT(*) > 1
) x;
SQL
)

if [ "$COLLISIONS" -gt 0 ]; then
    echo "结论：发现 $COLLISIONS 组冲突 —— **先人工处理**（保留哪一行、另一行改成什么编码由人拍）"
    echo "      处理完重跑本脚本，直到输出 0 组，再执行迁移 262。迁移不会替你合并或改码。"
    exit 1
fi

echo "结论：无冲突。迁移 262 可安全执行（它仍会再扫一遍，判定口径与本脚本一致）。"
