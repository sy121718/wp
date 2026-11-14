-- 760 · 补回 inventory_stock_movements 的商品外键。
--
-- 173 用「重命名旧表 → 建分区父表 → 搬数据 → 删旧表」把这张表改成月分区表，
-- 134 之前加在旧表上的 fk_inventory_movements_product 随旧表一起消失，而 173 之后
-- 没有任何迁移补回。实测结果：**存量库有这条外键，新库没有**——服务层之外的写入
-- 在新库上可以产生悬空 product_id，正是 134 要禁止的情况。
--
-- 幂等：按父表（conrelid）判断，避免把分区上继承来的同名约束当成父表已有。
-- 分区表上的外键会自动传播到现有分区，之后新建的月分区也继承父表约束。
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'fk_inventory_movements_product'
          AND conrelid = 'inventory_stock_movements'::regclass
    ) THEN
        ALTER TABLE inventory_stock_movements ADD CONSTRAINT fk_inventory_movements_product
            FOREIGN KEY (product_id) REFERENCES products(id) ON DELETE RESTRICT;
    END IF;
END $$;
