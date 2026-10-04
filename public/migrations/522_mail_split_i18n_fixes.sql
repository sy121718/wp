-- 522 · 邮件拆页后的 i18n 收口（定位文案修正 + 新键补种）
--
-- 背景：521 把「邮件营销」拆成 联系人 / 群发活动 / 邮件模板 / 自动化任务 四页，
--   但词条不会跟着页面走 —— 键一旦存在，`.["t"](key, fallback)` 的取值链
--   （当前语言 → 默认语言 → fallback → key，见 pkg/i18n/snapshot.go:39-57）
--   就永远落不到 fallback：模板里改过的兜底文案在已安装库上不生效。
--   所以拆页后的文案修正必须走迁移，不能只改模板。
--
-- 本迁移做三件事：
--   1) 联系人「筛选无结果」空态描述：437 写的「在下方折叠区批量导入联系人」随拆页失效
--      （导入已改成页头的「导入联系人」抽屉），改为与 mail_contacts.html 的 fallback 一致；
--   2) 活动报表页页头的「← 返回营销页」：「营销页」已被 521 拆成四个页面，
--      改为「返回群发活动」—— href 一直是 /admin/mail/campaigns，落点本来就对，
--      只是文案指向了一个不再存在的页名；
--   3) 补种拆页后新增的键 admin.mail.automation.bulk_delete_confirm（流程页批量删除确认）。
--
-- 幂等：两条改值沿用 437 的「只改历史精确值」条件（item_value = old_value）——
--   运营在后台改过的文案不会被覆盖；补种一条用 ON CONFLICT DO NOTHING。
--   注册见 register_mail_split_i18n_fixes.go：ConditionSQL 以「三处都已收口」为门槛，
--   单条判定会让中途失败被误判成已完成（224 / 521 的教训）。

-- 1) 联系人空态：指向页头「导入联系人」抽屉，而不是已经不存在的下方折叠区
UPDATE sys_i18n AS i
SET item_value = v.new_value, update_time = now()
FROM (VALUES
    ('admin.mail.marketing.contacts.empty', 'zh-CN',
     '可调整筛选条件，或在下方折叠区批量导入联系人。',
     '可调整筛选条件，或点右上角「导入联系人」批量导入。'),
    ('admin.mail.marketing.contacts.empty', 'en-US',
     'Adjust the filters, or import contacts in the section below.',
     'Adjust the filters, or use "Import contacts" at the top right to bulk import.')
) AS v(item_key, lang, old_value, new_value)
WHERE i.item_key = v.item_key AND i.lang = v.lang AND i.item_value = v.old_value;

-- 2) 活动报表页返回按钮：营销页已拆散，落点仍是群发活动列表
UPDATE sys_i18n AS i
SET item_value = v.new_value, update_time = now()
FROM (VALUES
    ('admin.mail.campaign.back', 'zh-CN', '返回营销页', '返回群发活动'),
    ('admin.mail.campaign.back', 'en-US', 'Back to marketing', 'Back to campaigns')
) AS v(item_key, lang, old_value, new_value)
WHERE i.item_key = v.item_key AND i.lang = v.lang AND i.item_value = v.old_value;

-- 3) 流程页批量删除确认（521 之后新增的键）
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
    ('admin.mail.automation.bulk_delete_confirm', 'zh-CN', '删除选中的流程？进行中的实例会先停止。', 200, 'admin', 'admin/mail/mail_automation.html: 批量删除流程的确认文案', 1, now(), now()),
    ('admin.mail.automation.bulk_delete_confirm', 'en-US', 'Delete the selected flows? Running instances are stopped first.', 200, 'admin', 'admin/mail/mail_automation.html: 批量删除流程的确认文案', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;

-- 回滚（手工，无自动回滚）：
--   UPDATE sys_i18n SET item_value = '可调整筛选条件，或在下方折叠区批量导入联系人。'
--     WHERE item_key = 'admin.mail.marketing.contacts.empty' AND lang = 'zh-CN'
--       AND item_value = '可调整筛选条件，或点右上角「导入联系人」批量导入。';
--   UPDATE sys_i18n SET item_value = 'Adjust the filters, or import contacts in the section below.'
--     WHERE item_key = 'admin.mail.marketing.contacts.empty' AND lang = 'en-US'
--       AND item_value = 'Adjust the filters, or use "Import contacts" at the top right to bulk import.';
--   UPDATE sys_i18n SET item_value = '返回营销页' WHERE item_key = 'admin.mail.campaign.back' AND lang = 'zh-CN' AND item_value = '返回群发活动';
--   UPDATE sys_i18n SET item_value = 'Back to marketing' WHERE item_key = 'admin.mail.campaign.back' AND lang = 'en-US' AND item_value = 'Back to campaigns';
--   DELETE FROM sys_i18n WHERE item_key = 'admin.mail.automation.bulk_delete_confirm';
