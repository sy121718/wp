-- 619 · 捆绑容器的容器价必须 > 0（对 service 校验的数据库侧兜底）。
--
-- 背景：Create 里已有这条校验（ErrBundlePriceRequired），但 Update 原先直接赋值 ——
-- 把一个捆绑商品的价格改成 0 或负数能存进去，列表与详情显示 0.00，
-- 前台无从判断这是免费还是没配。service 侧已补上同一判据（product.go 的 Update）。
--
-- 为什么还要数据库约束：081_product_tables.sql 的 default_price 只有 `numeric(12,2)`，
-- 没有任何 CHECK。而「捆绑容器必须有价」是**数据不变量**，不是某一层的行为约定 ——
-- 迁移脚本、修复脚本、将来的第二个写入口都绕不过它。
--
-- 判据只对 type='bundle' 生效：变体商品的价格放在 product_variants 里，
-- 商品行上的 default_price 只是「默认变体价」的快照，允许为空或 0。
--
-- 幂等：ADD CONSTRAINT 前先查 pg_constraint，重复执行为空操作。
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'ck_products_bundle_price_positive'
    ) THEN
        ALTER TABLE products
            ADD CONSTRAINT ck_products_bundle_price_positive
            CHECK (type <> 'bundle' OR (default_price IS NOT NULL AND default_price > 0));
    END IF;
END $$;
