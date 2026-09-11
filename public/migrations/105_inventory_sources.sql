-- 105 · 货源（供应商 / 集团内关联公司 / 自家工厂，issue #17）。
--
-- 一张表承载**所有进货来源**（spec §仓库与进货）：外部供应商、集团内关联公司、自家工厂
-- 登记在同一处、走同一套单据（采购单在 issue #18）。三条不可动摇的语义：
--
--   1. 类型区分内外部 —— type ∈ {external, internal}。external 是第三方外部供应商；
--      internal 是集团内（关联公司 / 自家工厂，可设内部结算价，自家工厂走生产入库）。
--      类型不是装饰性标签：它决定这条货源是否按内部交易口径出报表。
--   2. 关联方标志 related_party 独立于类型 —— 它只回答「这笔交易是不是关联交易」，
--      是**报表区分**的判据（spec §仓库与进货 #46c）。内部货源恒为关联方（CHECK 兜住）；
--      外部供应商也可以被标成关联方（同一实控人下的另一家公司）。
--   3. config 是**异构对接扩展信息**（spec #46）—— 不同来源的字段形状天然不同，故只有它
--      是 JSONB；类型 / 关联方 / 结算价 / 状态必须结构化，因为报表要按它们分组、
--      采购单要按它们校验。metadata 是模块级灵活字段（默认查询不取）。
--
-- 不建「货源 ↔ SKU」映射表（spec 明文）：货源货号记在采购单行上，历史采购价从库存流水查。
--
-- 与 inventory_warehouses 同一约定：工程内 code 唯一（upper 归一），行删除是硬删除
-- （被采购单引用的货源不允许删除由 issue #18 在服务层补守卫）。

CREATE TABLE IF NOT EXISTS inventory_sources (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id    uuid NOT NULL REFERENCES projects(id),
    code          text NOT NULL,
    name          text NOT NULL,
    type          text NOT NULL DEFAULT 'external',
    related_party boolean NOT NULL DEFAULT false,
    settle_price  numeric(12,2) NULL,
    status        text NOT NULL DEFAULT 'active',
    config        jsonb NOT NULL DEFAULT '{}'::jsonb,
    sort          integer NOT NULL DEFAULT 0,
    metadata      jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT inventory_sources_type_check CHECK (type IN ('external', 'internal')),
    CONSTRAINT inventory_sources_status_check CHECK (status IN ('active', 'disabled')),
    -- 内部货源即关联方：内部交易必须能被关联方报表捕获，不允许出现「内部但非关联」的行。
    CONSTRAINT inventory_sources_internal_related_check CHECK (type = 'external' OR related_party),
    -- 结算价只属于内部货源，且非负（它是自产商品的成本口径，不是售价）。
    CONSTRAINT inventory_sources_settle_price_check
        CHECK (settle_price IS NULL OR (type = 'internal' AND settle_price >= 0)),
    -- config 是异构对接配置，必须是一个 JSON **对象**（数组 / 标量没有键可供插件读取）。
    CONSTRAINT inventory_sources_config_object_check CHECK (jsonb_typeof(config) = 'object')
);

-- 货源编码在工程内唯一（按 upper(code)，服务层归一成大写，索引兜住绕过服务层的写入）。
CREATE UNIQUE INDEX IF NOT EXISTS uq_inventory_sources_project_code
    ON inventory_sources(project_id, upper(code));
-- 关联方报表区分：按工程 + 关联方标志 / 类型取数（报表维度就是这两列的组合）。
CREATE INDEX IF NOT EXISTS idx_inventory_sources_project_party
    ON inventory_sources(project_id, related_party);
CREATE INDEX IF NOT EXISTS idx_inventory_sources_project_type
    ON inventory_sources(project_id, type);

COMMENT ON TABLE inventory_sources IS '货源（供应商 / 集团内关联公司 / 自家工厂，issue #17）';
COMMENT ON COLUMN inventory_sources.type IS 'external 外部供应商 / internal 集团内（关联公司、自家工厂）';
COMMENT ON COLUMN inventory_sources.related_party IS '关联方标志：关联交易报表的判据；内部货源恒为真';
COMMENT ON COLUMN inventory_sources.settle_price IS '内部结算价：自产商品的成本口径（外部供应商不得有值）';
COMMENT ON COLUMN inventory_sources.config IS '异构对接扩展信息（JSON 对象）：不同来源的字段形状各不相同';
