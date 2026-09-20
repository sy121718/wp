package migrations

import "sync"

// register_publication_plan.go — 发布计划冻结表（迁移 308，审计 I18N-01）。
//
// 与 062（page_publications）/ 063（page_stagings）同族：三个台账都按 (page_id, lang)
// 建键，各自回答一个发布问题 —— 暂存了什么、激活了什么、依据哪份语言输入。
//
// 本表是新表（此前没有任何形态），因此用默认的「表存在即跳过」检查即可：
// 表不存在就整条执行，执行完必然存在。
//
// 注册方式：由 register.go 的 init() 显式调用（不在文件尾自注册）。
func registerPagePublicationPlans() {
	registerPagePublicationPlansOnce.Do(registerPagePublicationPlansSQL)
}

// registerPagePublicationPlansOnce 让重复调用成为空操作（不会重复 register）。
var registerPagePublicationPlansOnce sync.Once

// registerPagePublicationPlansSQL 注册 308（真正干活的那一半，被 Once 包一层）。
func registerPagePublicationPlansSQL() {
	register(Migration{
		Version:   "308-page-publication-plans",
		TableName: "page_publication_plans",
		SQL:       mustSQL("308_page_publication_plans.sql"),
	})
}
