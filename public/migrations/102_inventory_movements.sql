-- 102 · 库存流水 / 变动原因字典 / 物料清单（issue #16）。
--
-- 本迁移落地 #16 的三张持久表（第四张缓存台账见下方历史说明），围绕「库存真源 inventory_stocks」：
--   inventory_change_reasons    变动原因字典（出 / 入 / 调整三类；内置 + 自定义，引用而非自由文本）；
--   inventory_stock_movements   库存流水（每次真源变动一行：方向 / 数量 / 变动前后 / 原因 / 来源引用）；
--   inventory_bom_items         物料清单（父 SKU → 子项 SKU × 用量，扣减时按它展开）。
--
-- 历史说明：本文件仍 CREATE inventory_stock_cache_syncs（#16 时期的商品侧缓存同步台账）。
-- 迁移 121 已 DROP 该表并删除 product_variants 上的 stock_total / stock_synced_at ——
-- #32 合并商品与库存模块后，展示值直接读真源投影，**勿再维护缓存同步逻辑**。
--
-- 三条不可动摇的语义（121 之后仍成立）：
--   1. 可用量的判定只读 inventory_stocks 并加行锁；流水是**事后记账**，不是判定依据；
--   2. 变动原因必须是字典里的条目（reason_id + reason_code），不接受自由文本；
--   3. 商品查询期的库存展示走 inventory 真源投影，不参与扣减（见 AGENTS.md product/inventory 行）。
--
-- 外键选择：warehouse_id / variant_id 随仓、随变体级联删除（与 099 同口径：
-- 库存行与流水都是仓库与变体的派生物）；reason_id 用 ON DELETE SET NULL ——
-- 原因被删不能让历史流水消失，reason_code 是快照列，历史可读。

CREATE TABLE IF NOT EXISTS inventory_change_reasons (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- project_id 为 NULL 表示**内置原因**：全工程可见，不随工程创建而复制；
    -- 自定义原因必须归属某个工程（工程内 code 唯一）。
    project_id  uuid NULL REFERENCES projects(id),
    code        text NOT NULL,
    name        text NOT NULL,
    direction   text NOT NULL,
    is_builtin  boolean NOT NULL DEFAULT false,
    status      text NOT NULL DEFAULT 'active',
    sort        integer NOT NULL DEFAULT 0,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT inventory_change_reasons_direction_check CHECK (direction IN ('in', 'out', 'adjust')),
    CONSTRAINT inventory_change_reasons_status_check CHECK (status IN ('active', 'disabled'))
);
-- 内置原因按 code 全局唯一（大小写不敏感）。
CREATE UNIQUE INDEX IF NOT EXISTS uq_inventory_reasons_builtin_code
    ON inventory_change_reasons(lower(code)) WHERE project_id IS NULL;
-- 自定义原因按 (工程, code) 唯一（大小写不敏感）。
CREATE UNIQUE INDEX IF NOT EXISTS uq_inventory_reasons_project_code
    ON inventory_change_reasons(project_id, lower(code)) WHERE project_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_inventory_reasons_project ON inventory_change_reasons(project_id, direction, sort);

CREATE TABLE IF NOT EXISTS inventory_stock_movements (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id        uuid NOT NULL REFERENCES projects(id),
    warehouse_id      uuid NOT NULL REFERENCES inventory_warehouses(id) ON DELETE CASCADE,
    product_id        uuid NOT NULL,
    variant_id        uuid NOT NULL REFERENCES product_variants(id) ON DELETE CASCADE,
    sku_code          text NOT NULL DEFAULT '',
    -- 方向（in 入 / out 出 / adjust 调整）与数量：quantity 是**绝对变化量**（恒 > 0），
    -- delta 是带符号的实际变化量（out 为负）。无变化的调整不写流水（没有「变动」）。
    direction         text NOT NULL,
    quantity          integer NOT NULL,
    delta             integer NOT NULL,
    quantity_before   integer NOT NULL,
    quantity_after    integer NOT NULL,
    reason_id         uuid NULL REFERENCES inventory_change_reasons(id) ON DELETE SET NULL,
    reason_code       text NOT NULL,
    -- parent_variant_id：按物料清单展开扣减时，记录是哪个父 SKU 引出这条子项流水。
    parent_variant_id uuid NULL,
    source_type       text NOT NULL DEFAULT '',
    source_ref        text NOT NULL DEFAULT '',
    remark            text NOT NULL DEFAULT '',
    operator_id       text NOT NULL DEFAULT '',
    -- batch_id：一次变动（含展开的全部子项）共用一个批次号，便于整体回溯。
    batch_id          uuid NOT NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT inventory_stock_movements_direction_check CHECK (direction IN ('in', 'out', 'adjust')),
    CONSTRAINT inventory_stock_movements_quantity_check CHECK (quantity > 0),
    CONSTRAINT inventory_stock_movements_after_check CHECK (quantity_after >= 0)
);
CREATE INDEX IF NOT EXISTS idx_inventory_movements_project_time ON inventory_stock_movements(project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_inventory_movements_variant_time ON inventory_stock_movements(variant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_inventory_movements_batch ON inventory_stock_movements(batch_id);
CREATE INDEX IF NOT EXISTS idx_inventory_movements_reason ON inventory_stock_movements(reason_code);

CREATE TABLE IF NOT EXISTS inventory_bom_items (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id           uuid NOT NULL REFERENCES projects(id),
    parent_variant_id    uuid NOT NULL REFERENCES product_variants(id) ON DELETE CASCADE,
    parent_sku_code      text NOT NULL DEFAULT '',
    component_variant_id uuid NOT NULL REFERENCES product_variants(id) ON DELETE CASCADE,
    component_sku_code   text NOT NULL DEFAULT '',
    quantity             integer NOT NULL,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT inventory_bom_items_quantity_check CHECK (quantity > 0),
    CONSTRAINT inventory_bom_items_self_check CHECK (parent_variant_id <> component_variant_id),
    UNIQUE (parent_variant_id, component_variant_id)
);
CREATE INDEX IF NOT EXISTS idx_inventory_bom_component ON inventory_bom_items(component_variant_id);
CREATE INDEX IF NOT EXISTS idx_inventory_bom_project ON inventory_bom_items(project_id);

-- 以下表已在迁移 121 删除；保留 CREATE 仅为「已跑过 102、尚未跑 121」的中间态幂等。
CREATE TABLE IF NOT EXISTS inventory_stock_cache_syncs (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id   uuid NOT NULL REFERENCES projects(id),
    variant_id   uuid NOT NULL REFERENCES product_variants(id) ON DELETE CASCADE,
    sku_code     text NOT NULL DEFAULT '',
    -- true_total：同步时刻 inventory_stocks 的真源汇总（期望值）；
    -- cached_total：最后一次**成功**写入商品侧 stock_total 的值（失败时保持旧值）。
    true_total   integer NOT NULL,
    cached_total integer NOT NULL,
    status       text NOT NULL DEFAULT 'ok',
    error        text NOT NULL DEFAULT '',
    synced_at    timestamptz NOT NULL,
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT inventory_stock_cache_syncs_status_check CHECK (status IN ('ok', 'failed')),
    UNIQUE (variant_id)
);
CREATE INDEX IF NOT EXISTS idx_inventory_cache_syncs_project ON inventory_stock_cache_syncs(project_id, status);

COMMENT ON TABLE inventory_change_reasons IS '库存变动原因字典（issue #16；内置 project_id IS NULL + 自定义，引用而非自由文本）';
COMMENT ON TABLE inventory_stock_movements IS '库存流水（issue #16；每次真源变动一行：方向/数量/前后值/原因/来源引用）';
COMMENT ON TABLE inventory_bom_items IS '物料清单（issue #16；父 SKU → 子项 SKU × 用量，扣减时展开）';
COMMENT ON TABLE inventory_stock_cache_syncs IS '【121 已删】商品侧库存缓存同步台账（历史 issue #16）';
COMMENT ON COLUMN inventory_stock_movements.reason_code IS '变动原因 code 快照（reason_id 被删后历史仍可读）';
COMMENT ON COLUMN inventory_stock_cache_syncs.synced_at IS '【121 已删】历史列：曾与 product_variants.stock_synced_at 同源';
