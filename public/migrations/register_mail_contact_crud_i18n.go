package migrations

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
