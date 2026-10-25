-- 630 · 汇率快照（工程维度）：把「下单那一刻的汇率」固化成事实（W5）。
--
-- 为什么快照而不是实时换算：
--   实时换算会让同一张订单在不同时刻显示不同金额，利润报表随之漂移 ——
--   财务口径必须可重放。落地即固化，之后汇率怎么变都不影响历史订单。
--
-- 为什么缺失时留 NULL 而不是回填：
--   「汇率未固化」是真实状态（历史订单 / 外部导入）。回填会伪造历史，
--   显示「汇率未固化」比显示一个编造的数字诚实。
--
-- 为什么表不带 project_id：
--   汇率是**币种对**的事实，不是某个工程的事实 —— 同一个 USD/CNY 在哪个工程下都该是同一个数。
--   带 project_id 会让「同一天的汇率在两个工程下不一致」成为可能，而那不是业务上的差异。
--
-- 幂等：CREATE TABLE IF NOT EXISTS / ADD COLUMN IF NOT EXISTS / 索引 IF NOT EXISTS。

CREATE TABLE IF NOT EXISTS currency_rates (
    id          bigserial PRIMARY KEY,
    base_code   text NOT NULL,
    quote_code  text NOT NULL,
    -- numeric(18,8) 而不是 float：汇率是财务口径，浮点累加会让对账对不平
    -- （AGENTS.md「金额一律 BIGINT 分 / numeric」）。
    rate        numeric(18, 8) NOT NULL,
    rate_at     timestamptz NOT NULL DEFAULT now(),
    -- source 标注来源（manual / 定时任务 / 导入）：将来自动同步时靠它区分。
    source      text NOT NULL DEFAULT 'manual',
    update_time timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ck_currency_rates_rate CHECK (rate > 0),
    CONSTRAINT ck_currency_rates_code CHECK (btrim(base_code) <> '' AND btrim(quote_code) <> '')
);

-- 覆盖式：同一币对只留最新一条（与 inventory_stocks.cost_price 的覆盖式同口径）。
-- 按 upper(代码) 唯一：USD/usd 是同一个币种，不该允许两行。
CREATE UNIQUE INDEX IF NOT EXISTS uq_currency_rates_pair
    ON currency_rates (upper(base_code), upper(quote_code));

ALTER TABLE orders ADD COLUMN IF NOT EXISTS fx_rate numeric(18, 8);
ALTER TABLE orders ADD COLUMN IF NOT EXISTS fx_base_code text NOT NULL DEFAULT '';

COMMENT ON COLUMN orders.fx_rate IS '下单时刻汇率（1 订单币 = fx_rate 基准币）；NULL = 汇率未固化，不推算';
COMMENT ON COLUMN orders.fx_base_code IS '汇率快照的基准币种（fx_rate 的分母）；空串 = 未固化';
COMMENT ON TABLE currency_rates IS '汇率（币种对，覆盖式：同一币对只留最新一条）';
