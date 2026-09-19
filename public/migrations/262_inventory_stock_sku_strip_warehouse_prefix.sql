-- 262 · 仓库里的 SKU 永远是裸码：剥掉存量库存行 sku_code 上多余的仓码前缀。
--
-- 口径（用户 2026-09-19 拍板，照此实现，不自行改）：
--   · **仓库里的 SKU 永远是裸码**（如 DRAWERSMOKE_001），不带仓码前缀；
--     商品侧才带前缀（SZ_DRAWERSMOKE_001 —— 前缀标注归属 / 认领仓）。
--     所以 inventory_stocks.sku_code 应当是裸码，本迁移把历史遗留的前缀剥掉。
--   · 规则：若 upper(sku_code) 以 upper(warehouse.code) + '_' 开头（且前缀之后还有内容），
--     就去掉这一节。前缀判定用 upper 比较（仓短码在写入口已归一为大写，历史数据不一定）。
--   · 只对**能 JOIN 到仓库**的行做：仓库行不存在时无从判断前缀是什么，原样保留。
--
-- **先扫描、再更新**：剥掉之后若同一 (warehouse_id, sku_code) 出现重复，迁移显式
-- RAISE EXCEPTION 失败（带冲突明细），**绝不静默合并两行**。两行库存是两份事实 ——
-- 合并要么丢数量、要么丢成本、要么丢外部编码，任何一种都是丢账；而「留着一个没有
-- 上下文的 23505」也帮不上忙（看不出是哪两行），所以这里把明细直接打出来。
--
-- 幂等：改写后的值不再以「仓码 + _」开头，CheckSQL（见 register_catalog.go）
-- 判「还有带前缀的行」—— 没有则整条迁移跳过。
--
-- 固有边界（规则本身的，不是数据约定）：裸码恰好以「本仓短码 + 下划线」开头时
-- （SZ 仓里真有一条叫 SZ_001 的裸码），规则无法与「带前缀」区分。迁移只跑一次
-- （CheckSQL 判无前缀行即跳过），因此不会反复剥同一行。

DO $$
DECLARE
    dup_groups integer;
    dup_rows   integer;
    detail     text;
BEGIN
    -- 冲突明细「打回给人」而不是让迁移自己决定：每一行都给出可直接定位的字段 ——
    -- 仓短码、剥前缀后的裸码、行 id / product_id / variant_id / 各自的**原** sku_code /
    -- 数量 / 创建时间。拿到这段输出就能直接去库里对账，不需要再猜哪两行撞了。
    -- 刻意**不做任何自动处理**（不加后缀、不改码）：那种「为了过迁移而改数据」是数据篡改，
    -- 两行库存是两份事实，哪一行该改只能由人拍。
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
    SELECT COUNT(*), COALESCE(SUM(cnt), 0), string_agg(dup_detail, E'\n' ORDER BY warehouse_code, stripped_sku)
      INTO dup_groups, dup_rows, detail
      FROM (
          SELECT warehouse_code, stripped_sku, COUNT(*) AS cnt,
                 format('仓短码=%s 剥前缀后的裸码=%L 涉及 %s 行：', warehouse_code, stripped_sku, COUNT(*))
                 || E'\n' ||
                 string_agg(
                     format('    id=%s product_id=%s variant_id=%s 原 sku_code=%L 数量=%s 创建时间=%s',
                            id, product_id, variant_id, original_sku, quantity, create_time),
                     E'\n' ORDER BY create_time, id) AS dup_detail
            FROM stripped
           GROUP BY warehouse_code, warehouse_id, stripped_sku
          HAVING COUNT(*) > 1
      ) d;

    IF dup_groups > 0 THEN
        RAISE EXCEPTION E'剥掉仓码前缀后有 % 组 / % 行库存撞成同一个 (warehouse_id, sku_code)，迁移中止（不合并任何行、也不自动改码）。\n请人工决定保留哪一行 / 怎么改之后重跑迁移。\n冲突明细：\n%',
            dup_groups, dup_rows, detail
            USING ERRCODE = 'unique_violation';
    END IF;
END $$;

UPDATE inventory_stocks s
   SET sku_code = substr(s.sku_code, length(w.code) + 2),
       update_time = now()
  FROM inventory_warehouses w
 WHERE w.id = s.warehouse_id
   AND length(s.sku_code) > length(w.code) + 1
   AND upper(left(s.sku_code, length(w.code))) = upper(w.code)
   AND substr(s.sku_code, length(w.code) + 1, 1) = '_';
