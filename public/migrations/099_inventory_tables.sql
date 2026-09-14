-- 099 · 仓库与库存记录（issue #15）。
--
-- 本迁移落地 inventory 域的两张表：
--   inventory_warehouses  仓库实体（短码 / 名称 / 状态 / 默认仓），「未指定仓库」时的兜底来源；
--   inventory_stocks      库存真源，「SKU × 仓库」一行的记录，唯一约束建立在这两者之上。
--
-- 三条不可动摇的语义：
--   1. 库存真源在这里，不在 products / product_variants ——
--      一切影响可用量的判断只读本表并加行锁（行锁与增减能力见 issue #16）。
--      （121 之前曾在商品侧存 stock_total 冗余缓存，已删除；见 121_drop_stock_cache.sql。）
--   2. 「必须有一个默认仓」由部分唯一索引 uq_inventory_warehouses_project_default 兜住：
--      每工程至多一行 is_default —— 并发下也不可能出现两个默认仓。
--   3. 一个 SKU 可以在多个仓各有一行（UNIQUE (variant_id, warehouse_id)），
--      本期单仓只是「每个 SKU 恰好一行」的数据状态，不是表结构约束：
--      将来加仓是加数据，不改表结构。
--
-- 外键选择：
--   · inventory_stocks.warehouse_id → inventory_warehouses(id) ON DELETE CASCADE
--     （删仓连带其库存行；有非零库存时服务层先拒绝删除）；
--   · inventory_stocks.variant_id  → product_variants(id) ON DELETE CASCADE
--     （变体被删即无货可存；库存行是变体的派生物，不做悬空保留）。
--   · sku_code 是快照列：按 SKU 查库存不必 JOIN 商品模块的表（表隔离约定）。

CREATE TABLE IF NOT EXISTS inventory_warehouses (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  uuid NOT NULL REFERENCES projects(id),
    code        text NOT NULL,
    name        text NOT NULL,
    status      text NOT NULL DEFAULT 'active',
    is_default  boolean NOT NULL DEFAULT false,
    sort        integer NOT NULL DEFAULT 0,
    metadata    jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
-- 短码在工程内唯一：SKU 编码以它开头（{仓短码}_{商品码}_{序号}），重码会让两批货串号。
-- 按 upper(code) 建唯一索引：服务层把短码归一成大写，索引兜住任何绕过服务层的写入。
CREATE UNIQUE INDEX IF NOT EXISTS uq_inventory_warehouses_project_code
    ON inventory_warehouses(project_id, upper(code));
-- 「必须有一个默认仓」：每工程至多一行默认仓。
CREATE UNIQUE INDEX IF NOT EXISTS uq_inventory_warehouses_project_default
    ON inventory_warehouses(project_id) WHERE is_default;

CREATE TABLE IF NOT EXISTS inventory_stocks (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id   uuid NOT NULL REFERENCES projects(id),
    warehouse_id uuid NOT NULL REFERENCES inventory_warehouses(id) ON DELETE CASCADE,
    product_id   uuid NOT NULL,
    variant_id   uuid NOT NULL REFERENCES product_variants(id) ON DELETE CASCADE,
    sku_code     text NOT NULL DEFAULT '',
    quantity     integer NOT NULL DEFAULT 0,
    metadata     jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    -- 库存维度 = 「SKU × 仓库」：同一 SKU 可在多个仓各有一行。
    UNIQUE (variant_id, warehouse_id)
);
CREATE INDEX IF NOT EXISTS idx_inventory_stocks_project_sku ON inventory_stocks(project_id, sku_code);
CREATE INDEX IF NOT EXISTS idx_inventory_stocks_warehouse ON inventory_stocks(warehouse_id);
CREATE INDEX IF NOT EXISTS idx_inventory_stocks_product ON inventory_stocks(product_id);

COMMENT ON TABLE inventory_warehouses IS '仓库实体（issue #15；短码 / 名称 / 状态 / 默认仓）';
COMMENT ON TABLE inventory_stocks IS '库存真源（issue #15；SKU × 仓库 一行，行级锁的对象）';
COMMENT ON COLUMN inventory_warehouses.code IS '仓库短码（工程内唯一大写），SKU 编码前缀';
COMMENT ON COLUMN inventory_warehouses.is_default IS '默认仓标记：每工程至多一行，「未指定仓库」时的兜底';
COMMENT ON COLUMN inventory_stocks.quantity IS '可用量真源；一切可用量判断只读本列并加行锁，绝不读 product_variants.stock_total';
