package migrations

import "sync"

func registerMailContactWriteUII18n() {
	registerMailContactWriteUII18nOnce.Do(registerMailContactWriteUII18nSeed)
}

var registerMailContactWriteUII18nOnce sync.Once

func registerMailContactWriteUII18nSeed() {

	registerSeed(Seed{
		Version:   "598-mail-contact-write-ui-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 20 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang IN ('zh-CN', 'en-US') AND item_key IN (" +
			"'admin.mail.contacts.bulk.noneSelected', " +
			"'admin.mail.contacts.bulkDelete.allDeleted', " +
			"'admin.mail.contacts.bulkDelete.allSkipped', " +
			"'admin.mail.contacts.bulkDelete.partial', " +
			"'admin.mail.contacts.bulkDelete.refresh', " +
			"'admin.mail.contacts.bulkTag.allUpdated', " +
			"'admin.mail.contacts.bulkTag.allSkipped', " +
			"'admin.mail.contacts.bulkTag.partial', " +
			"'admin.mail.contacts.bulkTag.refresh', " +
			"'admin.mail.contacts.write.noJs')",
		SQL: mustSQL("598_mail_contact_write_ui_i18n.sql"),
	})
}
