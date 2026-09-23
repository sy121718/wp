package migrations

import "sync"

// register_mail_admin_page_err_i18n.go — 邮件公开链接（点击追踪 / 一键退订）访客面词条（410）。
//
// 见 410_i18n_mail_admin_page_err.sql 的头部：mail 的三个访客端点（/_t/o、/_t/c、/_t/u）
// 服务的是**收件人**而不是后台运营 —— 无登录态、无后台页面壳，失败给一句受控短句、
// 成功给一张自带样式的整页 HTML。形态本来就对，缺的是文案来源：此前硬编码中文，
// 英文收件人（Accept-Language: en）打开只能看到中文，而且运营改不了措辞（改的是代码）。
//
// 本批新增 5 个 key × 2 语言：
//
//	mail.err.trackLinkInvalid        —— 点击追踪链接验签失败 / 已过期
//	mail.err.unsubscribeLinkInvalid  —— 退订链接失败（token 无效 / 联系人查不到 / 写库失败，原文只进日志）
//	mail.msg.unsubscribeDoneTitle    —— 退订成功页标题
//	mail.msg.unsubscribeDoneBody     —— 退订成功页正文（%s = 收件人邮箱）
//	mail.msg.unsubscribeDoneNote     —— 退订成功页补充说明
//
// 注册方式：本文件自带 init()（与 register_project_page_err_i18n.go 同形），不往 register.go
// 里加调用 —— 那是与本批并行任务共用的文件。
// init 的注册顺序不影响执行顺序：迁移按 compareVersion 排序、种子按版本号排序，
// RunSeeds 在所有结构迁移之后执行。
func registerMailAdminPageErrI18n() {
	registerMailAdminPageErrI18nOnce.Do(registerMailAdminPageErrI18nSeed)
}

// registerMailAdminPageErrI18nOnce 让重复调用成为空操作。
var registerMailAdminPageErrI18nOnce sync.Once

// registerMailAdminPageErrI18nSeed 注册 410（真正干活的那一半）。
func registerMailAdminPageErrI18nSeed() {
	// 门槛 = 本批 5 个 key 的 **en-US** 行都在。
	//
	// 挑 en-US 而不是 zh-CN：zh-CN 一侧的值与 Go 里的中文兜底常量逐字相同，
	// 未来别的批次若顺手补过其中一句（zh-CN 行先存在），按 zh-CN 计数会让门槛在
	//「本批还没跑」时就成立，整批词条被静默跳过（403 记过同一条教训）。
	//
	// ConditionSQL 由迁移器 db.Raw 直接执行，**没有任何参数替换** —— 判定要用的 key 只能写成
	// SQL 字面量；写成 item_key = ? 会永远查不到行，让这条迁移每次启动重跑（178 踩过）。
	registerSeed(Seed{
		Version:   "410-i18n-mail-admin-page-err",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 5 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'en-US' AND item_key IN (" +
			"'mail.err.trackLinkInvalid','mail.err.unsubscribeLinkInvalid'," +
			"'mail.msg.unsubscribeDoneTitle','mail.msg.unsubscribeDoneBody'," +
			"'mail.msg.unsubscribeDoneNote')",
		SQL: mustSQL("410_i18n_mail_admin_page_err.sql"),
	})
}

func init() { registerMailAdminPageErrI18n() }
