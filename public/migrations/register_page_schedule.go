package migrations

import "sync"

// register_page_schedule.go — 页面定时上下线待办表（迁移 460，PIPE-7）。
//
// 与 062（page_publications）/ 063（page_stagings）/ 308（page_publication_plans）同族：
// 四张台账都按 (page_id, lang) 建键，各自回答一个发布问题 —— 暂存了什么、激活了什么、
// 依据哪份语言输入、该在何时做什么。
//
// 本表是新表（此前没有任何形态），因此用默认的「表存在即跳过」检查即可：
// 表不存在就整条执行，执行完必然存在。
//
// 注册方式：由 register.go 的 init() 显式调用（不在文件尾自注册）——
// 与 308 registerPagePublicationPlans 同形，避免「谁负责注册」出现两个真源。
func registerPageSchedules() {
	registerPageSchedulesOnce.Do(registerPageSchedulesSQL)
}

// registerPageSchedulesOnce 让重复调用成为空操作（不会重复 register）。
var registerPageSchedulesOnce sync.Once

// registerPageSchedulesSQL 注册 460（真正干活的那一半，被 Once 包一层）。
func registerPageSchedulesSQL() {
	register(Migration{
		Version:   "460-page-schedules",
		TableName: "page_schedules",
		SQL:       mustSQL("460_page_schedules.sql"),
	})
}
