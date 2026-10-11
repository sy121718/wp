package migrations

import "sync"

// register_mail_contact_write_ui_i18n.go — 联系人页「JSON 写表单」的界面文案（迁移 598）。
//
// 单独成一个主题文件而不是挤进相邻的 mail i18n 批次（526 / 434 那两批是页面字段与确认框）：
// 本批的 10 个 key 是**页面写动作从前端直调 JSON API**带出来的（就地结果区的摘要句 +
// 无 JS 提示），混进别的主题会让「这批词条为什么存在」在 review 时看不出来 ——
// 与 593（区块页）/ 595（词条页）单独成文件的理由同源。
//
// 目前 init() 里的注册函数按主题手工列出（见 register.go），新增主题要在那里加一行调用。
func registerMailContactWriteUII18n() {
	registerMailContactWriteUII18nOnce.Do(registerMailContactWriteUII18nSeed)
}

var registerMailContactWriteUII18nOnce sync.Once

func registerMailContactWriteUII18nSeed() {
	// 598：10 个 key × 中英 = 20 行。
	//
	// 判定逐条枚举本批**全部 10 个 key 的两种语言**（= 20 个 (item_key, lang) 组合），
	// 不用全库行数、也不用前缀 LIKE：
	//   · 全库/前缀判据会被同期其它批次的行满足而静默跳过（本仓库踩过，见 226/277/283/592）；
	//   · **只看中文那 10 行**同样会静默跳过 —— 只要运营库里任意一条 en-US 缺失
	//     （历史库缺行、有人手工删过英文、或本批 SQL 曾半途失败），中文计数就已经满足，
	//     于是 RunSeeds 跳过 SQL，缺的英文**永远补不上**，英文界面回退到组件包内的中文兜底
	//     （错得安静：没有异常、没有日志）。review-sol 的 task-33 复验抓到这条 P2。
	// 定则来自 AGENTS.md §数据库：「判据的偏差方向要刻意选**宁可重跑，不可静默跳过**」。
	// 20 行齐全时判据为 1（跳过）；缺任意一 (key, lang) 即为 0（重跑）——
	// 而 SQL 是 ON CONFLICT (item_key, lang) DO NOTHING，重跑只补缺行、不覆盖运营改过的值，
	// 所以这个偏差方向没有副作用。
	//
	// 语言标识写死这两种：它们就是本批 SQL 里出现的全部语言（用例
	// register_mail_contact_write_ui_i18n_test.go 会核对判据与 SQL 的组合集合一致）。
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
