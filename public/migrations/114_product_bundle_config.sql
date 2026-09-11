-- 114 · 捆绑品选项规则（issue #20）。
--
-- 存储沿用既有决定（spec §数据结构精简原则）：捆绑 BOM 归商品表的 JSON 列
-- products.bundle_items，不新建关联表。本迁移把该列从「无形状的裸 JSON」收紧为
-- 「带规则的选项集」：
--
--   {
--     "maxOptions":  20,   -- 可配置的选项数量上限（服务端护栏，1..20）
--     "minTotalQty": 0,    -- 整单最小总件数（0 = 不设下限）
--     "maxTotalQty": 0,    -- 整单最大总件数（0 = 不设上限，只受库存约束）
--     "options": [
--       {"variantId": "<uuid>", "required": true, "defaultQty": 1, "minQty": 1, "maxQty": 5}
--     ]
--   }
--
-- options 来自**跨商品挑选的已存在 SKU**（spec 第 53/54 条）；maxQty / maxTotalQty 的 0
-- 是「留空」的哨兵值（不是「最多 0 件」）：数量取值域恒 >= 0，用 0 表示「不设上限」
-- 比 null 少一层分支。
--
-- CHECK 只兜「结构形状」（必须是对象、options 必须是数组）：它不表达业务规则
--（必选 / 上下限 / 整单总件数，那些在 service 层）。数据库兜底的价值在于任何写入
-- 路径都写不进一个连形状都不对的值，读取侧不必写防御性分支。

ALTER TABLE products ALTER COLUMN bundle_items
    SET DEFAULT '{"maxOptions": 20, "minTotalQty": 0, "maxTotalQty": 0, "options": []}'::jsonb;

-- 历史值（默认 '[]'::jsonb 或任意裸数组）规范化为空配置：幂等，重跑无副作用。
UPDATE products
SET bundle_items = '{"maxOptions": 20, "minTotalQty": 0, "maxTotalQty": 0, "options": []}'::jsonb
WHERE jsonb_typeof(bundle_items) IS DISTINCT FROM 'object'
   OR jsonb_typeof(bundle_items -> 'options') IS DISTINCT FROM 'array';

-- 约束用 DROP IF EXISTS + ADD 写法，可重复执行（幂等）。
ALTER TABLE products DROP CONSTRAINT IF EXISTS products_bundle_items_shape_check;
ALTER TABLE products ADD CONSTRAINT products_bundle_items_shape_check
    CHECK (jsonb_typeof(bundle_items) = 'object'
       AND jsonb_typeof(bundle_items -> 'options') = 'array');

COMMENT ON COLUMN products.bundle_items IS '捆绑品选项规则（issue #20）：{maxOptions,minTotalQty,maxTotalQty,options[{variantId,required,defaultQty,minQty,maxQty}]}；maxQty/maxTotalQty 为 0 表示不设上限（只受库存约束）';
