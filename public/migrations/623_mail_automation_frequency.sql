-- 623 · 自动化流程的触发频率与「回信即退出」。
--
-- trigger_frequency：
--   every_time 每次触发都建实例（今天的默认行为）
--   once       同一个人只跑一次（有过任何实例就不再建）
--
-- 为什么需要它：`idx_mail_automation_runs_active` 只挡「同时有一个进行中的实例」
-- （status IN running/waiting）—— 一个跑完的实例（completed/failed/stopped）不在索引里，
-- 于是「打开邮件」这类可重复事件会不断给人重跑同一条欢迎流程。
--
-- exit_on_reply：
--   收到回信时把该联系人**进行中的实例**置 stopped。true 才生效。
--
-- 幂等：ADD COLUMN IF NOT EXISTS + 显式默认值。
ALTER TABLE mail_automations ADD COLUMN IF NOT EXISTS trigger_frequency VARCHAR(16) NOT NULL DEFAULT 'every_time';
ALTER TABLE mail_automations ADD COLUMN IF NOT EXISTS exit_on_reply BOOLEAN NOT NULL DEFAULT false;

-- 频率闸门要按 (automation_id, contact_id) 判断「有没有过任何实例」：
-- 索引 131 的 idx_mail_automation_runs_contact 只按 contact_id，用不上。
CREATE INDEX IF NOT EXISTS idx_mail_automation_runs_automation_contact
    ON mail_automation_runs (automation_id, contact_id);
