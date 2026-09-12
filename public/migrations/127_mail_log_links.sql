-- 127 · 邮件日志补活动与联系人关联（issue #37 群发与报表）。
--
-- 群发时要知道「这封信属于哪个活动、发给了哪个联系人」：
--   · 活动维度的送达 / 失败计数回写要按 campaign_id 定位活动；
--   · 后续追踪（打开 / 点击）要把事件挂回活动与联系人；
--   · 「这个人收到过哪些信」也需要 contact_id。
-- 两列都可空：事务邮件（注册验证等）不属于任何活动。

ALTER TABLE mail_logs ADD COLUMN IF NOT EXISTS campaign_id BIGINT;
ALTER TABLE mail_logs ADD COLUMN IF NOT EXISTS contact_id  BIGINT;

-- 活动维度的统计与排障只查非空行，故用部分索引。
CREATE INDEX IF NOT EXISTS idx_mail_logs_campaign ON mail_logs (campaign_id) WHERE campaign_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_mail_logs_contact  ON mail_logs (contact_id)  WHERE contact_id  IS NOT NULL;
