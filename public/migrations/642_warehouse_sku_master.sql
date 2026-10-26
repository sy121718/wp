-- 642 · 仓库商品 SKU 主档化：把「这个仓里这条货叫什么、长什么样、按多少运费」
--       从商品侧下放到仓库侧（B3，对齐 dy_erp 的 prd_warehouse_sku）。
--
-- 为什么主档落在库存行上而不是商品上：
--   同一款货在不同仓可以有不同的叫法（客户码 / 供应商码）、不同的图片（对方要求的图）、
--   不同的运费口径（海外仓与国内仓不同）。这些事实**随仓而变**，挂在商品上必然要再造一张
--   「商品 × 仓」的覆盖表；而 641 之后 inventory_stocks 的身份本来就是 (仓库, 仓库 SKU)，
--   主档属性直接落在它身上就是最贴事实的位置。
--
-- 为什么 name / barcode / image_url 都是 NOT NULL DEFAULT ''：
--   它们全部**可以先空着**（收货时只知道一个编码是常态）。用 NULL 表示「没填」会让
--   「没填」与「空串」成为两个事实，所有读取点都要各自决定要不要再看空串一次
--   —— 与 624 给 variant_id 用 *string 的取舍相反，这里空串就是唯一表示。
--
-- 为什么 freight_price 有默认值 70：
--   照 dy_erp 的口径（DefaultWarehouseSKUFreight = 70.0）。运费不是成本的一部分，
--   它是「这条货从仓到买家」的固定开销；给一个显式默认值让报表在没填时也有数，
--   而 0 会被误读成「免运费」。
--
-- 为什么 cost_fx_rate 可空：
--   与 orders.fx_rate（迁移 633）同一口径 —— NULL = 汇率未固化，**不推算**。
--   回填一个编造的汇率会让成本折算在事后无法与对账结果对齐。

ALTER TABLE inventory_stocks
    ADD COLUMN IF NOT EXISTS name               text          NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS image_url          text          NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS barcode            text          NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS freight_price      numeric(12,2) NOT NULL DEFAULT 70,
    ADD COLUMN IF NOT EXISTS cost_currency      text          NOT NULL DEFAULT 'CNY',
    ADD COLUMN IF NOT EXISTS cost_base_currency text          NOT NULL DEFAULT 'CNY',
    ADD COLUMN IF NOT EXISTS cost_fx_rate       numeric(18,8),
    ADD COLUMN IF NOT EXISTS status             text          NOT NULL DEFAULT 'active',
    ADD COLUMN IF NOT EXISTS remark             text          NOT NULL DEFAULT '';

-- 状态只有两个值：停用的货行不再出现在「从仓库选」的候选里，但历史流水与已建立的
-- 变体引用照旧可读（停用不等于删除，删除会让引用的变体变成悬空）。
ALTER TABLE inventory_stocks
    DROP CONSTRAINT IF EXISTS inventory_stocks_status_check;
ALTER TABLE inventory_stocks
    ADD CONSTRAINT inventory_stocks_status_check CHECK (status IN ('active', 'disabled'));

-- 运费非负：它是开销，不是可以「负回来」的调整项。
ALTER TABLE inventory_stocks
    DROP CONSTRAINT IF EXISTS ck_inventory_stocks_freight_nonneg;
ALTER TABLE inventory_stocks
    ADD CONSTRAINT ck_inventory_stocks_freight_nonneg CHECK (freight_price >= 0);

-- 币种两位以上、大写三字母；空串是**不允许**的（没填就落默认 CNY，与汇率未固化是两件事）。
ALTER TABLE inventory_stocks
    DROP CONSTRAINT IF EXISTS ck_inventory_stocks_cost_currency;
ALTER TABLE inventory_stocks
    ADD CONSTRAINT ck_inventory_stocks_cost_currency
    CHECK (btrim(cost_currency) <> '' AND btrim(cost_base_currency) <> '');

-- 汇率 > 0（NULL 表示未固化，不受本约束影响）。
ALTER TABLE inventory_stocks
    DROP CONSTRAINT IF EXISTS ck_inventory_stocks_cost_fx_rate;
ALTER TABLE inventory_stocks
    ADD CONSTRAINT ck_inventory_stocks_cost_fx_rate
    CHECK (cost_fx_rate IS NULL OR cost_fx_rate > 0);

-- 按名称检索是常态（运营手上的往往是商品名，不是编码）。
CREATE INDEX IF NOT EXISTS idx_inventory_stocks_name
    ON inventory_stocks (project_id, warehouse_id, name);

COMMENT ON COLUMN inventory_stocks.name IS
    '仓库侧的商品名（642，对齐 dy_erp prd_warehouse_sku.name）：这条货在这个仓叫什么；可以先于商品存在';
COMMENT ON COLUMN inventory_stocks.image_url IS
    '仓库侧的货品图（642）：同一款货在不同仓可以有不同的图（对方仓要求的图）';
COMMENT ON COLUMN inventory_stocks.barcode IS
    '条码（642）：扫码枪读到的原始串，不参与任何唯一约束（同一条码可以贴多行）';
COMMENT ON COLUMN inventory_stocks.freight_price IS
    '运费（642）：这条货从仓到买家的固定开销，默认 70（照 dy_erp）；不计入 cost_price';
COMMENT ON COLUMN inventory_stocks.cost_currency IS
    '成本的计价币种（642）：记录 cost_price 用什么币记的';
COMMENT ON COLUMN inventory_stocks.cost_base_currency IS
    '换算的基准币种（642）：与 cost_fx_rate 一起表达「1 计价币 = fx 基准币」';
COMMENT ON COLUMN inventory_stocks.cost_fx_rate IS
    '成本换算汇率（642）：NULL = 汇率未固化，不推算（与 orders.fx_rate 同口径）';
COMMENT ON COLUMN inventory_stocks.status IS
    '货行状态（642）：disabled 不再出现在「从仓库选」候选里，但历史流水与已建立的变体引用照旧可读';
COMMENT ON COLUMN inventory_stocks.remark IS '备注（642）';
