-- 168_mail_campaign_event_totals.sql
--
-- 群发活动的事件汇总列（IDX-012）。
--
-- 背景：打开/点击是一次一行（mail_campaign_events），大群发会立刻把表撑起来；而报表
-- 需要的只是「这个活动被打开了多少次、点击了多少次」。此前汇总只能现算 —— 于是明细
-- 既不能清理（清了报表就没数），又不能不清理（表会一直涨）。
-- 加两列把汇总固化下来，明细才可以按保留期清理：报表读「固化值 + 保留期内明细」。
--
-- 默认 0 表示「尚未固化」（而不是「零次打开」）：固化在明细**即将被清理**时执行一次，
-- 用 open_count = 0 AND click_count = 0 作未固化标记，避免反复统计导致数值被新事件覆盖。
ALTER TABLE mail_campaigns ADD COLUMN IF NOT EXISTS open_count integer NOT NULL DEFAULT 0;
ALTER TABLE mail_campaigns ADD COLUMN IF NOT EXISTS click_count integer NOT NULL DEFAULT 0;
