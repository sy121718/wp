package migrations

import "sync"

// register_mail_page_notice_i18n.go — mail 页面级受控提示的 9 条词条（迁移 614）。
//
// 单独成一个主题文件：这 9 条是「控制器不取词」这一批**新引入**的 key（原先只有 Go 侧的中文兜底），
// 而 enums 对账判据只认 `a.msg.*` / `a.err.*` / `a.test.*` 三种形态 ——
// `mail.bulk.*` 与 `mail.page.*` 那 7 条连红都不会红（只有 mail.msg.automationTick 被它抓到）。
// 换句话说：**门禁绿不等于没漏配**，这一批的漏配要靠人读一遍 enums 的新增块才发现。
//
// 目前 init() 里的注册函数按主题手工列出（见 register.go），新增主题要在那里加一行调用。
func registerMailPageNoticeI18n() {
	registerMailPageNoticeI18nOnce.Do(registerMailPageNoticeI18nSeed)
}

var registerMailPageNoticeI18nOnce sync.Once

func registerMailPageNoticeI18nSeed() {
	// 614：9 个 key × 中英 = 18 行，判据逐条枚举这 18 个 (item_key, lang) 组合。
	//
	// 不用全库行数、也不用前缀 LIKE：前者会让本批每次启动重跑，后者会被同期其它批次的行满足
	// 而静默跳过（AGENTS.md §数据库「判据必须枚举本批自己的对象（上界封闭）」）。
	// 少枚举任意一个组合 → 那条词条永远补不上（判据已满足、整段 SQL 被跳过），
	// 而偏差方向刻意选「宁可重跑」：缺任意一项即为 0，SQL 重跑，ON CONFLICT 只补缺行。
	//
	// 语言标识写死这两种：它们就是本批 SQL 里出现的全部语言
	//（用例 register_mail_page_notice_i18n_test.go 会核对判据与 SQL 的组合集合一致）。
	registerSeed(Seed{
		Version:   "614-mail-page-notice-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 18 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE (item_key, lang) IN (" +
			"('mail.bulk.allDone', 'zh-CN'), ('mail.bulk.allDone', 'en-US'), " +
			"('mail.bulk.allSkipped', 'zh-CN'), ('mail.bulk.allSkipped', 'en-US'), " +
			"('mail.bulk.partial', 'zh-CN'), ('mail.bulk.partial', 'en-US'), " +
			"('mail.msg.automationTick', 'zh-CN'), ('mail.msg.automationTick', 'en-US'), " +
			"('mail.page.templateListFailed', 'zh-CN'), ('mail.page.templateListFailed', 'en-US'), " +
			"('mail.page.contactStatusBad', 'zh-CN'), ('mail.page.contactStatusBad', 'en-US'), " +
			"('mail.page.campaignIDRequired', 'zh-CN'), ('mail.page.campaignIDRequired', 'en-US'), " +
			"('mail.page.runIDRequired', 'zh-CN'), ('mail.page.runIDRequired', 'en-US'), " +
			"('mail.page.automationIDRequired', 'zh-CN'), ('mail.page.automationIDRequired', 'en-US'))",
		SQL: mustSQL("614_mail_page_notice_i18n.sql"),
	})
}
