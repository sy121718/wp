-- 526 · 联系人 CRUD / 标签管理的 i18n 词条（22 个新键 × 中英）+ 空态描述改写
--
-- 键族沿用既有 `admin.mail.marketing.*`（190 / 284 / 441 / 522 都在这一族下），
-- 不另起第二套前缀。三处必须逐字一致：模板兜底、本迁移的 SQL 值、enums 的 LabelPair。
--
-- 本迁移动两类词条：
--   1) UPDATE：无筛选空态描述（admin.mail.marketing.contacts.empty.initial）。
--      带 old_value 守卫 —— 运营在后台改过的词条不覆盖（441 的同款做法）。
--      旧文案只讲「在下方导入联系人」，页面新增「新建联系人」入口后不再完整。
--   2) INSERT：22 个 admin.mail.marketing.* 新键（新建 / 编辑抽屉、删除确认、批量打标签、
--      标签筛选），ON CONFLICT DO NOTHING，不覆盖既有值。
--   3) INSERT：3 个 mail.err.* 新键（重复邮箱 / 空标签 / 缺同意来源），页面上要给运营看的是文案而不是 key。
--
-- 幂等：注册见 register_mail_contact_crud_i18n.go。判据用「本批 22 个键在两种语言下都在」
--   的计数 + 空态两条终值，枚举闭区间（item_key IN (本批全部键)），不用 LIKE 前缀
--   —— 前缀会把别批次的行算进来导致静默跳过（058），全库总量会永远追不平（076）。

-- 1) 无筛选空态描述：只在仍是历史精确值时改写
UPDATE sys_i18n AS i
SET item_value = v.new_value, update_time = now()
FROM (VALUES
    ('admin.mail.marketing.contacts.empty.initial', 'zh-CN',
     '在下方导入联系人，开始建立发送名单。',
     '新建一位联系人手工加一条，或用「导入联系人」批量导入 CSV —— 名单建立起来才能按标签圈人群群发。'),
    ('admin.mail.marketing.contacts.empty.initial', 'en-US',
     'Import contacts below to start building your mailing list.',
     'Add one contact by hand, or import a CSV in bulk — the list has to exist before you can target people by tag.')
) AS v(item_key, lang, old_value, new_value)
WHERE i.item_key = v.item_key AND i.lang = v.lang AND i.item_value = v.old_value;

-- 2) 22 个新键
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.mail.marketing.contact_form.new', 'zh-CN', '新建联系人', 200, 'admin', 'admin/mail/mail_contacts.html: 新建联系人抽屉按钮', 1, now(), now()),
('admin.mail.marketing.contact_form.new', 'en-US', 'New contact', 200, 'admin', 'admin/mail/mail_contacts.html: new contact drawer button', 1, now(), now()),

('admin.mail.marketing.contact_form.edit', 'zh-CN', '编辑', 200, 'admin', 'admin/mail/mail_contacts.html: 行内编辑按钮', 1, now(), now()),
('admin.mail.marketing.contact_form.edit', 'en-US', 'Edit', 200, 'admin', 'admin/mail/mail_contacts.html: row edit button', 1, now(), now()),

('admin.mail.marketing.contact_form.edit_title', 'zh-CN', '编辑联系人', 200, 'admin', 'admin/mail/mail_contacts.html: 编辑抽屉标题', 1, now(), now()),
('admin.mail.marketing.contact_form.edit_title', 'en-US', 'Edit contact', 200, 'admin', 'admin/mail/mail_contacts.html: edit drawer title', 1, now(), now()),

('admin.mail.marketing.contact_form.email', 'zh-CN', '邮箱', 200, 'admin', 'admin/mail/mail_contacts.html: 邮箱字段标签', 1, now(), now()),
('admin.mail.marketing.contact_form.email', 'en-US', 'Email', 200, 'admin', 'admin/mail/mail_contacts.html: email field label', 1, now(), now()),

('admin.mail.marketing.contact_form.email.ph', 'zh-CN', 'name@example.com', 200, 'admin', 'admin/mail/mail_contacts.html: 邮箱占位符', 1, now(), now()),
('admin.mail.marketing.contact_form.email.ph', 'en-US', 'name@example.com', 200, 'admin', 'admin/mail/mail_contacts.html: email placeholder', 1, now(), now()),

('admin.mail.marketing.contact_form.name', 'zh-CN', '姓名', 200, 'admin', 'admin/mail/mail_contacts.html: 姓名字段标签', 1, now(), now()),
('admin.mail.marketing.contact_form.name', 'en-US', 'Name', 200, 'admin', 'admin/mail/mail_contacts.html: name field label', 1, now(), now()),

('admin.mail.marketing.contact_form.tags', 'zh-CN', '标签（逗号分隔）', 200, 'admin', 'admin/mail/mail_contacts.html: 标签字段标签', 1, now(), now()),
('admin.mail.marketing.contact_form.tags', 'en-US', 'Tags (comma separated)', 200, 'admin', 'admin/mail/mail_contacts.html: tags field label', 1, now(), now()),

('admin.mail.marketing.contact_form.consent_source', 'zh-CN', '同意来源', 200, 'admin', 'admin/mail/mail_contacts.html: 同意来源字段标签', 1, now(), now()),
('admin.mail.marketing.contact_form.consent_source', 'en-US', 'Consent source', 200, 'admin', 'admin/mail/mail_contacts.html: consent source field label', 1, now(), now()),

('admin.mail.marketing.contact_form.consent_source.ph', 'zh-CN', '例如：本人在官网表单勾选同意', 200, 'admin', 'admin/mail/mail_contacts.html: 同意来源占位符', 1, now(), now()),
('admin.mail.marketing.contact_form.consent_source.ph', 'en-US', 'e.g. opted in on the website form', 200, 'admin', 'admin/mail/mail_contacts.html: consent source placeholder', 1, now(), now()),

('admin.mail.marketing.contact_form.status', 'zh-CN', '同意状态', 200, 'admin', 'admin/mail/mail_contacts.html: 状态字段标签', 1, now(), now()),
('admin.mail.marketing.contact_form.status', 'en-US', 'Consent status', 200, 'admin', 'admin/mail/mail_contacts.html: status field label', 1, now(), now()),

('admin.mail.marketing.contact_form.status_hint', 'zh-CN', '待确认不会被群发；置为「已订阅」必须同时填写同意来源（谁在什么时候声明过同意）。', 200, 'admin', 'admin/mail/mail_contacts.html: 状态字段说明（与 service 的强制校验同口径）', 1, now(), now()),
('admin.mail.marketing.contact_form.status_hint', 'en-US', 'Pending contacts are never mailed; setting a contact to "Subscribed" requires a consent source.', 200, 'admin', 'admin/mail/mail_contacts.html: status field hint (matches the server-side rule)', 1, now(), now()),

('admin.mail.marketing.contact_form.save', 'zh-CN', '保存', 200, 'admin', 'admin/mail/mail_contacts.html: 抽屉保存按钮', 1, now(), now()),
('admin.mail.marketing.contact_form.save', 'en-US', 'Save', 200, 'admin', 'admin/mail/mail_contacts.html: drawer save button', 1, now(), now()),

('admin.mail.marketing.contact_delete_confirm', 'zh-CN', '删除该联系人？退订 / 投诉记录仍留在抑制名单里 —— 删掉联系人不会让他重新收到邮件。', 200, 'admin', 'admin/mail/mail_contacts.html: 单条删除确认', 1, now(), now()),
('admin.mail.marketing.contact_delete_confirm', 'en-US', 'Delete this contact? Unsubscribe / complaint records stay in the suppression list — deleting the contact does not let them receive mail again.', 200, 'admin', 'admin/mail/mail_contacts.html: single delete confirm', 1, now(), now()),

('admin.mail.marketing.bulk_delete_confirm', 'zh-CN', '删除选中的联系人？退订 / 投诉记录仍留在抑制名单里 —— 删掉联系人不会让他重新收到邮件。', 200, 'admin', 'admin/mail/mail_contacts.html: 批量删除确认', 1, now(), now()),
('admin.mail.marketing.bulk_delete_confirm', 'en-US', 'Delete the selected contacts? Unsubscribe / complaint records stay in the suppression list — deleting them does not let them receive mail again.', 200, 'admin', 'admin/mail/mail_contacts.html: bulk delete confirm', 1, now(), now()),

('admin.mail.marketing.bulk_tag_submit', 'zh-CN', '批量打标签', 200, 'admin', 'admin/mail/mail_contacts.html: 批量打标签按钮', 1, now(), now()),
('admin.mail.marketing.bulk_tag_submit', 'en-US', 'Add / remove tags', 200, 'admin', 'admin/mail/mail_contacts.html: bulk tag button', 1, now(), now()),

('admin.mail.marketing.bulk_tag_confirm', 'zh-CN', '给选中的联系人打标签？填在「去掉」里的标签会从他们身上移除。', 200, 'admin', 'admin/mail/mail_contacts.html: 批量打标签确认', 1, now(), now()),
('admin.mail.marketing.bulk_tag_confirm', 'en-US', 'Tag the selected contacts? Tags listed under "remove" will be taken off them.', 200, 'admin', 'admin/mail/mail_contacts.html: bulk tag confirm', 1, now(), now()),

('admin.mail.marketing.bulk_tag.add', 'zh-CN', '加上标签', 200, 'admin', 'admin/mail/mail_contacts.html: 批量条「加上标签」输入', 1, now(), now()),
('admin.mail.marketing.bulk_tag.add', 'en-US', 'Add tags', 200, 'admin', 'admin/mail/mail_contacts.html: bulk bar add-tags input', 1, now(), now()),

('admin.mail.marketing.bulk_tag.add.ph', 'zh-CN', '加上标签：vip,华南', 200, 'admin', 'admin/mail/mail_contacts.html: 批量条「加上标签」占位符', 1, now(), now()),
('admin.mail.marketing.bulk_tag.add.ph', 'en-US', 'Add tags: vip,APAC', 200, 'admin', 'admin/mail/mail_contacts.html: bulk bar add-tags placeholder', 1, now(), now()),

('admin.mail.marketing.bulk_tag.remove', 'zh-CN', '去掉标签', 200, 'admin', 'admin/mail/mail_contacts.html: 批量条「去掉标签」输入', 1, now(), now()),
('admin.mail.marketing.bulk_tag.remove', 'en-US', 'Remove tags', 200, 'admin', 'admin/mail/mail_contacts.html: bulk bar remove-tags input', 1, now(), now()),

('admin.mail.marketing.bulk_tag.remove.ph', 'zh-CN', '去掉标签：vip', 200, 'admin', 'admin/mail/mail_contacts.html: 批量条「去掉标签」占位符', 1, now(), now()),
('admin.mail.marketing.bulk_tag.remove.ph', 'en-US', 'Remove tags: vip', 200, 'admin', 'admin/mail/mail_contacts.html: bulk bar remove-tags placeholder', 1, now(), now()),

('admin.mail.marketing.filter.tags', 'zh-CN', '标签', 200, 'admin', 'admin/mail/mail_contacts.html: 标签筛选标签', 1, now(), now()),
('admin.mail.marketing.filter.tags', 'en-US', 'Tag', 200, 'admin', 'admin/mail/mail_contacts.html: tag filter label', 1, now(), now()),

('admin.mail.marketing.ph.tags', 'zh-CN', '例如 vip（多个用逗号分隔）', 200, 'admin', 'admin/mail/mail_contacts.html: 标签筛选占位符', 1, now(), now()),
('admin.mail.marketing.ph.tags', 'en-US', 'e.g. vip (comma separated)', 200, 'admin', 'admin/mail/mail_contacts.html: tag filter placeholder', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;

-- 3) 新增业务错误文案（mail.err.contactEmailExists / mail.err.contactTagEmpty）
--
-- 这一组与上面 22 条 admin.mail.* 分开：它们是**模块错误出口**的 key（service 上抛、
-- 由 inbound/http/mail_err.go 的 translateMailFacing 取词）。缺词条时取词会回落 key 本身，
-- 运营看到的会是 mail.err.contactEmailExists 这样的原始 key —— 可读性直接归零。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('mail.err.contactEmailExists', 'zh-CN', '该邮箱已存在：同一个人不用建两条；若只是换了地址，请直接编辑已有联系人。', 200, 'mail', 'service/mail_contact_save.go: 新建 / 改邮箱撞 lower(email) 唯一索引', 1, now(), now()),
('mail.err.contactEmailExists', 'en-US', 'This email already exists. Edit the existing contact instead of creating a duplicate.', 200, 'mail', 'service/mail_contact_save.go: duplicate email on create or rename', 1, now(), now()),
('mail.err.contactTagEmpty', 'zh-CN', '请至少填一个要加上或去掉的标签。', 200, 'mail', 'service/mail_contact_tag.go: 批量打标签时加与减都为空', 1, now(), now()),
('mail.err.contactTagEmpty', 'en-US', 'Fill in at least one tag to add or remove.', 200, 'mail', 'service/mail_contact_tag.go: bulk tag with empty add and remove', 1, now(), now()),
('mail.err.consentSourceRequired', 'zh-CN', '置为「已订阅」必须填写同意来源：谁在什么时候声明过同意，是这条名单的合规依据。', 200, 'mail', 'service/mail_contact_save.go: 新建 / 编辑置为已订阅但没写同意来源', 1, now(), now()),
('mail.err.consentSourceRequired', 'en-US', 'A consent source is required to mark a contact as subscribed: record who consented and when.', 200, 'mail', 'service/mail_contact_save.go: subscribing without a consent source', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;

-- 回滚（手工，无自动回滚）：
--   DELETE FROM sys_i18n WHERE lang IN ('zh-CN', 'en-US') AND (
--     item_key = 'admin.mail.marketing.contacts.empty.initial'
--     OR item_key LIKE 'admin.mail.marketing.contact\_form.%' ESCAPE '\'
--     OR item_key LIKE 'admin.mail.marketing.bulk\_tag%' ESCAPE '\'
--     OR item_key IN ('admin.mail.marketing.contact_delete_confirm',
--                     'admin.mail.marketing.bulk_delete_confirm',
--                     'admin.mail.marketing.filter.tags',
--                     'admin.mail.marketing.ph.tags',
--                     'mail.err.contactEmailExists', 'mail.err.contactTagEmpty',
--                     'mail.err.consentSourceRequired'));
