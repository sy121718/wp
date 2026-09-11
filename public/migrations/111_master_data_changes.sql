-- 111 · 主数据变更记录（issue #19）。
--
-- 一张 append-only 的字段级审计表：谁在什么时候把哪个字段从什么改成了什么。
--
-- 与库存流水的**职责分离**（验收 5）：
--   inventory_stock_movements —— 「库存数量怎么变的」（入库 / 出库 / 盘点，带前后数量）；
--   master_data_changes       —— 「主数据字段怎么变的」（商品价格 / SKU 编码 / 上下架状态 /
--                                变体默认发货仓 / 货源资料，带前后取值）。
-- 两者各记一处、互不替代：收货入库在流水记「+10」，成本价回写在本表记「12.00 → 13.50」。
--
-- 三个设计要点：
--   1. 一行 = 一个字段。新增 / 修改 / 删除都逐字段落行（新增时 old 为空、删除时 new 为空），
--      读取侧永远拿到同一形状的字段级时间线，筛选维度（实体 / 字段 / 动作 / 操作人）
--      全部落在列上，不需要解析某一行里的 JSON。
--   2. 实体身份用 (entity_type, entity_id) 裸列，**不建外键**：实体被删掉之后
--      变更历史必须仍然可读（那正是审计要回答的问题）。展示名以 entity_label 快照承载。
--   3. append-only 由**数据库触发器**兜底，不只靠代码约定：
--      BEFORE UPDATE OR DELETE 直接 RAISE EXCEPTION。能把审计表改掉的历史等于没有历史。

CREATE TABLE IF NOT EXISTS master_data_changes (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id   uuid NOT NULL REFERENCES projects(id),
    entity_type  text NOT NULL,
    entity_id    uuid NOT NULL,
    -- 实体展示名快照（商品名 / SKU 编码 / 货源名），实体删除后仍可读。
    entity_label text NOT NULL DEFAULT '',
    action       text NOT NULL,
    field        text NOT NULL,
    old_value    text NOT NULL DEFAULT '',
    new_value    text NOT NULL DEFAULT '',
    -- origin：这条记录由哪条写入路径产生（product / variant / pricing / receipt / source）。
    origin       text NOT NULL DEFAULT '',
    -- operator_id：操作人（会话里的登录名，与库存流水的 operator_id 同口径）。
    operator_id  text NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT master_data_changes_action_check
        CHECK (action IN ('create', 'update', 'delete')),
    CONSTRAINT master_data_changes_entity_type_check
        CHECK (entity_type IN ('product', 'product_variant', 'inventory_source')),
    -- 字段名不可为空：本表的每一行都必须能回答「改的是哪个字段」。
    CONSTRAINT master_data_changes_field_check CHECK (field <> '')
);

-- 按实体查历史（验收 4 的主查询）：工程 + 实体类型 + 实体 id，按时间倒序取。
CREATE INDEX IF NOT EXISTS idx_master_data_changes_entity
    ON master_data_changes(project_id, entity_type, entity_id, created_at DESC);

-- 后台时间线（不带实体条件时的默认视图）。
CREATE INDEX IF NOT EXISTS idx_master_data_changes_time
    ON master_data_changes(project_id, created_at DESC);

-- 按字段查（「这个商品的价格被谁改过」）。
CREATE INDEX IF NOT EXISTS idx_master_data_changes_field
    ON master_data_changes(project_id, entity_type, field);

CREATE OR REPLACE FUNCTION master_data_changes_append_only() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'master_data_changes is append-only: % is rejected', TG_OP;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_master_data_changes_append_only ON master_data_changes;

CREATE TRIGGER trg_master_data_changes_append_only
    BEFORE UPDATE OR DELETE ON master_data_changes
    FOR EACH ROW EXECUTE FUNCTION master_data_changes_append_only();

COMMENT ON TABLE master_data_changes IS '主数据变更记录（issue #19；append-only 字段级审计）';
COMMENT ON COLUMN master_data_changes.entity_type IS '实体类型白名单：product / product_variant / inventory_source';
COMMENT ON COLUMN master_data_changes.action IS '动作：create / update / delete';
COMMENT ON COLUMN master_data_changes.field IS '字段名（各模块自己的字段白名单键）';
COMMENT ON COLUMN master_data_changes.origin IS '写入路径：product / variant / variant_generate / pricing / receipt / source';
COMMENT ON COLUMN master_data_changes.operator_id IS '操作人（会话登录名），写入后不可改写';
