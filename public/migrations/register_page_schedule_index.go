package migrations

import "sync"

// register_page_schedule_index.go — page_schedules 全状态 page_id 复合索引（结构 506，审计 DB-06）。
//
// 506：`(page_id, create_time DESC, id DESC)`。460 建的三条二级索引里唯一含 page_id 的是
// PARTIAL（`WHERE status IN ('pending','running')`），对终态行不可用，而读路径
// `ListSchedulesByPage` / `ListSchedulesByPages` **覆盖全部状态**。
//
// **刻意不带 TableName**：migrator.apply 的默认 CheckSQL 是
// `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = ?`，
// 且只在 CheckSQL 里出现 `?` 时才把 TableName 传进去 —— TableName 为空即查「表名等于空串」，
// 恒为 0，于是整条 SQL 每次启动都会执行。
//
// 为什么这里**必须**这样：page_schedules 由 460 建好且**早已存在**，一旦带上 TableName，
// CheckSQL 立刻命中 count > 0 → 整条迁移被「迁移对象已存在，跳过」，
// `CREATE INDEX IF NOT EXISTS` 永不执行 —— 存量库永远没有这条索引，而全新库有：
// 两边静默分叉（491 与 484–486 同为此刻意不填 TableName）。
// 代价是文件里**只能放幂等语句**，506 的 `CREATE INDEX IF NOT EXISTS` 满足。
//
// 注册方式：由 register.go 的 init() 显式调用（与 491 同）。
func registerPageScheduleIndex() {
	registerPageScheduleIndexOnce.Do(func() {
		register(Migration{
			Version: "506-page-schedules-page-index",
			SQL:     mustSQL("506_page_schedules_page_index.sql"),
		})
	})
}

// registerPageScheduleIndexOnce 让重复调用成为空操作（不会重复 register）。
var registerPageScheduleIndexOnce sync.Once
