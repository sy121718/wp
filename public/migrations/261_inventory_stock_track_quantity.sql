-- 261 · 库存域：无限库存（不跟踪数量）开关 —— inventory_stocks.track_quantity。
--
-- 口径（用户 2026-09-19 拍板，照此实现，不自行改）：
--
--   1. 「无限」用**显式开关**表达，不用可空数量：track_quantity = false 就是「不跟踪」
--      （= 无限），quantity 保持 NOT NULL DEFAULT 0。可空数量会把「没填」与「0」混成
--      同一个值 —— 卖光（0）与无限（NULL / 不填）在界面上完全一样，运营分不出来。
--   2. CHECK (track_quantity OR quantity = 0)：不跟踪的行**不允许**带数字。
--      数量与开关是同一件事的两种表达，同时存在就是自相矛盾的数据。
--   3. 存量行一律 track_quantity = true（**保守**）。
--      理由：存量那些 0 无法区分为「建行占位」还是「卖光了」。把卖光的行判成无限会
--      直接导致超卖 —— 无限行的扣减不校验可用量、不扣减，订单照卖。宁可让运营手动
--      把确实无限的行改成无限，也不能替他们猜。**新建行**才默认无限。
--
-- 幂等：
--   · 加列放在 DO 块里，并且只在「本次真的新增了这一列」时才执行存量 UPDATE ——
--     重跑（列已存在）不会把运营手工改成无限的存量行重新掰回跟踪。
--   · CHECK 约束用 DO 块吞 duplicate_object，重跑不失败。
--   · 约束建在 UPDATE **之后**：ADD CONSTRAINT 会校验既有行，先生效会拒绝存量非 0 行。
--
-- CheckSQL（register_catalog.go）判列是否存在：已加则跳过整条迁移（约定见 236）。

DO $$
DECLARE
    added boolean := false;
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
         WHERE table_schema = current_schema()
           AND table_name = 'inventory_stocks'
           AND column_name = 'track_quantity'
    ) THEN
        ALTER TABLE inventory_stocks ADD COLUMN track_quantity boolean NOT NULL DEFAULT false;
        added := true;
    END IF;

    -- 存量行保守置 true（见文件头第 3 条）。只在这一列**刚被加上**的那一次执行。
    IF added THEN
        UPDATE inventory_stocks SET track_quantity = true;
    END IF;
END $$;

COMMENT ON COLUMN inventory_stocks.track_quantity IS '是否跟踪数量：false = 无限（不跟踪，扣减不校验可用量也不扣减，quantity 恒为 0）；true = 按 quantity 跟踪。存量行一律 true（保守，见迁移 261 注释），新建行默认 false。';

DO $$
BEGIN
    ALTER TABLE inventory_stocks
        ADD CONSTRAINT inventory_stocks_track_quantity_check
        CHECK (track_quantity OR quantity = 0);
EXCEPTION
    -- 约束已存在（重复执行 / 迁移判定漏判）：什么都不做。
    WHEN duplicate_object THEN NULL;
END $$;
