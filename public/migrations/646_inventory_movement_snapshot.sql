-- 646 · 流水行带上货品名与图片快照（B4：库存流水独立页）。
--
-- 为什么是**快照列**而不是 join inventory_stocks 取当前值：
-- 流水是审计。货行改名、换图、甚至被删（stock_id 会置 NULL，迁移 640 的 FK 是
-- ON DELETE SET NULL）之后，历史流水仍然要能回答「当时动的是哪件货、长什么样」。
-- 同一手法已用在 unit_cost（迁移 256）：变动时刻的事实固定在流水行上，
-- 之后改主档不改写历史。
--
-- 与 sku_code 的分工：sku_code 是仓库侧的编码身份（货行的 key 之一），
-- 这两列是**展示**用的描述信息 —— 「SKU 对得上但看不出是什么东西」是台账页最常被
-- 抱怨的一件事（运营要拿着编码去别处反查）。
--
-- 分区表（迁移 173）：ALTER 主表会自动把列加到全部分区，不需要逐分区改。
ALTER TABLE inventory_stock_movements ADD COLUMN IF NOT EXISTS stock_name       text NOT NULL DEFAULT '';
ALTER TABLE inventory_stock_movements ADD COLUMN IF NOT EXISTS stock_image_url  text NOT NULL DEFAULT '';

COMMENT ON COLUMN inventory_stock_movements.stock_name IS '流水写入时刻的货品名称快照（货行主档 name，迁移 642）';
COMMENT ON COLUMN inventory_stock_movements.stock_image_url IS '流水写入时刻的货品图片快照（货行主档 image_url，迁移 642）；空串 = 没图';
