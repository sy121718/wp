package migrations

// 526 · 联系人 CRUD / 标签管理的 i18n 词条。
//
// 判据 = 「本批 22 个 admin.mail.* 键 + 3 个 mail.err.* 键在 zh-CN / en-US 两种语言下都在」
// +「空态描述两条终值都到位」。
// 计数与 item_key IN (…本批全部键…) 一起用：枚举闭区间保证只数本批自己的行，
// 计数保证不是「只落了一半」。偏差方向刻意选「宁可重跑，不可静默跳过」。
// ConditionSQL 由 db.Raw 直接执行、没有参数替换，所以整段写成 SQL 字面量。
func init() {
	registerSeed(Seed{
		Version:   "526-mail-contact-crud-i18n",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN
    (SELECT count(*) FROM sys_i18n WHERE lang = 'zh-CN' AND item_key IN (
        'admin.mail.marketing.contact_form.new', 'admin.mail.marketing.contact_form.edit',
        'admin.mail.marketing.contact_form.edit_title', 'admin.mail.marketing.contact_form.email',
        'admin.mail.marketing.contact_form.email.ph', 'admin.mail.marketing.contact_form.name',
        'admin.mail.marketing.contact_form.tags', 'admin.mail.marketing.contact_form.consent_source',
        'admin.mail.marketing.contact_form.consent_source.ph', 'admin.mail.marketing.contact_form.status',
        'admin.mail.marketing.contact_form.status_hint', 'admin.mail.marketing.contact_form.save',
        'admin.mail.marketing.contact_delete_confirm', 'admin.mail.marketing.bulk_delete_confirm',
        'admin.mail.marketing.bulk_tag_submit', 'admin.mail.marketing.bulk_tag_confirm',
        'admin.mail.marketing.bulk_tag.add', 'admin.mail.marketing.bulk_tag.add.ph',
        'admin.mail.marketing.bulk_tag.remove', 'admin.mail.marketing.bulk_tag.remove.ph',
        'admin.mail.marketing.filter.tags', 'admin.mail.marketing.ph.tags')) = 22
    AND (SELECT count(*) FROM sys_i18n WHERE lang = 'en-US' AND item_key IN (
        'admin.mail.marketing.contact_form.new', 'admin.mail.marketing.contact_form.edit',
        'admin.mail.marketing.contact_form.edit_title', 'admin.mail.marketing.contact_form.email',
        'admin.mail.marketing.contact_form.email.ph', 'admin.mail.marketing.contact_form.name',
        'admin.mail.marketing.contact_form.tags', 'admin.mail.marketing.contact_form.consent_source',
        'admin.mail.marketing.contact_form.consent_source.ph', 'admin.mail.marketing.contact_form.status',
        'admin.mail.marketing.contact_form.status_hint', 'admin.mail.marketing.contact_form.save',
        'admin.mail.marketing.contact_delete_confirm', 'admin.mail.marketing.bulk_delete_confirm',
        'admin.mail.marketing.bulk_tag_submit', 'admin.mail.marketing.bulk_tag_confirm',
        'admin.mail.marketing.bulk_tag.add', 'admin.mail.marketing.bulk_tag.add.ph',
        'admin.mail.marketing.bulk_tag.remove', 'admin.mail.marketing.bulk_tag.remove.ph',
        'admin.mail.marketing.filter.tags', 'admin.mail.marketing.ph.tags')) = 22
    AND (SELECT count(*) FROM sys_i18n WHERE lang = 'zh-CN'
        AND item_key IN ('mail.err.contactEmailExists', 'mail.err.contactTagEmpty', 'mail.err.consentSourceRequired')) = 3
    AND (SELECT count(*) FROM sys_i18n WHERE lang = 'en-US'
        AND item_key IN ('mail.err.contactEmailExists', 'mail.err.contactTagEmpty', 'mail.err.consentSourceRequired')) = 3
    AND EXISTS (SELECT 1 FROM sys_i18n WHERE item_key = 'admin.mail.marketing.contacts.empty.initial'
        AND lang = 'zh-CN' AND item_value = '新建一位联系人手工加一条，或用「导入联系人」批量导入 CSV —— 名单建立起来才能按标签圈人群群发。')
    AND EXISTS (SELECT 1 FROM sys_i18n WHERE item_key = 'admin.mail.marketing.contacts.empty.initial'
        AND lang = 'en-US' AND item_value = 'Add one contact by hand, or import a CSV in bulk — the list has to exist before you can target people by tag.')
THEN 1 ELSE 0 END`,
		SQL: mustSQL("526_mail_contact_crud_i18n.sql"),
	})
}
