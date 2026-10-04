-- 523 · 活动报表页空态动作的旧页名收口
--
-- 522 只改了「活动报表页页头」的返回按钮，漏了同一页的空态动作：
--   admin.mail.campaign.empty.action 的 DB 值仍是 415 写的「回营销页启动群发」，
--   渲染在 mail_campaign.html:87 与 :118。于是同一屏里页头说「返回群发活动」、
--   空态说「回营销页」—— 而「营销页」已被 521 拆成四个页面，两个名字指同一次跳转。
--
-- 模板 fallback 已随拆页改成「回群发活动页启动」，但键存在时 fallback 永不生效
--   （pkg/i18n/snapshot.go:39-57 取值链：当前语言 → 默认语言 → fallback → key），
--   所以必须改 DB 值 —— 与 522 修 contacts.empty / campaign.back 是同一类。
--
-- 为什么另开 523 而不是改 522：seed 通道按 Version 记录执行历史，
--   522-mail-split-i18n-fixes 一旦在某个库跑过就不会重跑，改 522 的 SQL 对已安装库无效。
--
-- 幂等：沿用 437/522 的「只改历史精确值」条件（item_value = old_value），不覆盖运营手改。
-- 注册见 register_mail_campaign_action_i18n_fix.go。

UPDATE sys_i18n AS i
SET item_value = v.new_value, update_time = now()
FROM (VALUES
    ('admin.mail.campaign.empty.action', 'zh-CN',
     '回营销页启动群发',
     '回群发活动页启动'),
    ('admin.mail.campaign.empty.action', 'en-US',
     'Back to marketing to start the campaign',
     'Back to campaigns to start sending')
) AS v(item_key, lang, old_value, new_value)
WHERE i.item_key = v.item_key AND i.lang = v.lang AND i.item_value = v.old_value;

-- 回滚（手工，无自动回滚）：
--   UPDATE sys_i18n SET item_value = '回营销页启动群发'
--     WHERE item_key = 'admin.mail.campaign.empty.action' AND lang = 'zh-CN' AND item_value = '回群发活动页启动';
--   UPDATE sys_i18n SET item_value = 'Back to marketing to start the campaign'
--     WHERE item_key = 'admin.mail.campaign.empty.action' AND lang = 'en-US' AND item_value = 'Back to campaigns to start sending';
