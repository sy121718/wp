#!/usr/bin/env bash
# check-inventory-sku-format.sh — 库存侧 SKU 编码"存量格式"的**只读**巡检（打回给人）。
#
# 判定口径（冻结，2026-09-19 拍板；与迁移 262、docs/14 §1.1、服务端 normalizeStockSKU 同源）：
#   · **仓库里的 SKU 永远是裸码**（DRAWERSMOKE_001），不带仓码前缀；
#     仓码前缀只属于**商品侧**（SZ_DRAWERSMOKE_001 —— 标注归属 / 认领仓）。
#   · 入库入口（RegisterReceipt / RegisterProductionInbound → normalizeStockSKU）已经归一：
#     带前缀的按目标仓短码**幂等剥掉一次**，空串一律拒绝。归一之后新建的行不会再落成下面三类。
#
# 本脚本查的是**修复前已经落库的存量行**（口径收口之前写进去的历史数据）：
#   ① inventory_stocks.sku_code 为空（含纯空白）              —— 空编码的库存行；
#   ② inventory_stocks.sku_code 以"该仓短码_"开头             —— 带仓码前缀的库存行
#      （包含只有前缀、剥完为空的那种，如 SZ_）；
#   ③ inventory_purchase_order_lines.sku_code 为空，或以**本工程任一仓**的短码加下划线开头
#      —— 采购行上的同一问题。按"任一仓"而不是只按单据上的收货仓：登记收货允许指定别的仓
#      （RegisterReceipt 的 warehouseId 可覆盖单据上的收货仓），归一用的就是那个仓的短码。
#
# **为什么不自动改**（本脚本一个字节都不改，有命中即 exit 1 走上线前卡口）：
#   · ① 空编码剥不出任何东西：这条货在仓库里叫什么**只能由人回答**（对应哪个商品 / 变体的编码？
#     还是这行本身该删？），脚本猜不出来；空串还会在 UNIQUE (warehouse_id, sku_code)（迁移 244）
#     上互相撞车，改法不止一种；
#   · ② 剥前缀后可能与同仓另一行撞成同一个 (warehouse_id, sku_code)（迁移 262 因此 RAISE
#     EXCEPTION 整条中止）—— 两行库存是两份事实，留哪一行、另一行改成什么码，只能由人拍；
#     而且裸码恰好以"本仓短码_"开头时无法与"带前缀"区分（迁移 262 记过的固有边界）；
#   · ③ 采购行的 sku_code 是**快照**：入库建库存行用的是它，事后改快照会与已入库的流水脱节。
# 自动加后缀 / 静默合并 / 丢行都是数据篡改（AGENTS.md「冲突与数据不一致一律打回给人」）。
#
# 用法：
#   bash scripts/check-inventory-sku-format.sh
#   退出码 0 = 三类都没命中；1 = 有命中（明细已打印，先人工处理再上线）；2 = 环境问题。
#
# 只读：只跑 SELECT。判定一律限定 current_schema()（并发 / 残留 schema 里的同名表会让判定
# 串味，167 与 p7_index_audit 各踩过一次）。
set -euo pipefail
cd "$(dirname "$0")/.."

if ! command -v psql >/dev/null 2>&1; then
    echo "缺少 psql，无法巡检库存 SKU 编码格式。" >&2
    exit 2
fi

# 从 config.yaml 的 database 段取值（与 check-stock-sku-prefix-collisions.sh 同一手法）。
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

echo "== 库存侧 SKU 编码存量巡检（裸码口径：仓库里的 SKU 不带仓码前缀）=="
echo "库：$DB_NAME@$DB_HOST:$DB_PORT   schema：$(psql_run -tAc 'SELECT current_schema()')"
echo

echo "── ① inventory_stocks.sku_code 为空（含纯空白）──"
psql_run <<'SQL'
SELECT 'inventory_stocks' AS table_name,
       s.id                AS row_id,
       w.code              AS warehouse_code,
       s.sku_code          AS original_sku,
       s.quantity          AS quantity,
       s.variant_id,
       s.product_id,
       s.create_time
  FROM inventory_stocks s
  JOIN inventory_warehouses w ON w.id = s.warehouse_id
 WHERE btrim(s.sku_code) = ''
 ORDER BY s.create_time, s.id;
SQL
ROWS_EMPTY=$(psql_run -tA <<'SQL'
SELECT COUNT(*) FROM inventory_stocks WHERE btrim(sku_code) = '';
SQL
)
echo "命中 $ROWS_EMPTY 行；改法（对应哪个编码 / 是否删行）由人定，脚本不改数据。"
echo

echo "── ② inventory_stocks.sku_code 以「该仓短码_」开头（带仓码前缀的库存行）──"
psql_run <<'SQL'
SELECT 'inventory_stocks' AS table_name,
       s.id                                        AS row_id,
       w.code                                      AS warehouse_code,
       s.sku_code                                  AS original_sku,
       substr(s.sku_code, length(w.code) + 2)      AS stripped_sku,
       s.quantity                                  AS quantity,
       s.variant_id,
       s.product_id,
       s.create_time
  FROM inventory_stocks s
  JOIN inventory_warehouses w ON w.id = s.warehouse_id
 WHERE length(s.sku_code) >= length(w.code) + 1
   AND upper(left(s.sku_code, length(w.code) + 1)) = upper(w.code) || '_'
 ORDER BY w.code, s.sku_code, s.id;
SQL
ROWS_PREFIX=$(psql_run -tA <<'SQL'
SELECT COUNT(*)
  FROM inventory_stocks s
  JOIN inventory_warehouses w ON w.id = s.warehouse_id
 WHERE length(s.sku_code) >= length(w.code) + 1
   AND upper(left(s.sku_code, length(w.code) + 1)) = upper(w.code) || '_';
SQL
)
echo "命中 $ROWS_PREFIX 行；stripped_sku 为空的那些（只有前缀）剥完仍是空串，"
echo "      入库入口会直接拒绝 —— 它们一定是脏数据。剥完与同仓另一行撞车的，迁移 262 会中止。"
echo

echo "── ③ inventory_purchase_order_lines.sku_code 为空或带本工程任一仓的短码前缀 ──"
psql_run <<'SQL'
SELECT 'inventory_purchase_order_lines' AS table_name,
       l.id                  AS row_id,
       l.project_id,
       o.code                AS order_code,
       CASE WHEN btrim(l.sku_code) = '' THEN '（空串）'
            ELSE (SELECT string_agg(DISTINCT w2.code, ',' ORDER BY w2.code)
                    FROM inventory_warehouses w2
                   WHERE w2.project_id = l.project_id
                     AND length(l.sku_code) >= length(w2.code) + 1
                     AND upper(left(l.sku_code, length(w2.code) + 1)) = upper(w2.code) || '_')
       END                   AS matched_warehouse_codes,
       l.sku_code            AS original_sku,
       l.quantity            AS quantity,
       l.variant_id,
       l.product_id,
       l.create_time
  FROM inventory_purchase_order_lines l
  JOIN inventory_purchase_orders o ON o.id = l.order_id
 WHERE btrim(l.sku_code) = ''
    OR EXISTS (
        SELECT 1 FROM inventory_warehouses w3
         WHERE w3.project_id = l.project_id
           AND length(l.sku_code) >= length(w3.code) + 1
           AND upper(left(l.sku_code, length(w3.code) + 1)) = upper(w3.code) || '_'
    )
 ORDER BY o.code, l.id;
SQL
ROWS_LINES=$(psql_run -tA <<'SQL'
SELECT COUNT(*)
  FROM inventory_purchase_order_lines l
 WHERE btrim(l.sku_code) = ''
    OR EXISTS (
        SELECT 1 FROM inventory_warehouses w3
         WHERE w3.project_id = l.project_id
           AND length(l.sku_code) >= length(w3.code) + 1
           AND upper(left(l.sku_code, length(w3.code) + 1)) = upper(w3.code) || '_'
    );
SQL
)
echo "命中 $ROWS_LINES 行；采购行的 sku_code 是入库时的快照，改之前先对已入库流水与库存行。"
echo

TOTAL=$((ROWS_EMPTY + ROWS_PREFIX + ROWS_LINES))
if [ "$TOTAL" -gt 0 ]; then
    echo "结论：发现 $TOTAL 行格式异常的存量数据（① $ROWS_EMPTY / ② $ROWS_PREFIX / ③ $ROWS_LINES）"
    echo "      —— **打回给人**：上面每一行都给了可定位字段（表名 / 行 id / 仓短码 / 原 sku_code /"
    echo "      数量或单据号），由人决定改成什么码或删哪一行。脚本不改任何数据。"
    exit 1
fi

echo "结论：三类都没命中 —— 库存行与采购行的 sku_code 都是裸码（或采购行为空的历史行）。"
