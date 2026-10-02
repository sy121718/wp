package migrations

import "sync"

// register_page_excluded_langs.go — 页面级语言排除列（结构 491）。
//
// 491 pages.excluded_langs：text[]，默认 '{}'，语义 = 本页不产出 / 不参与切换器、
// hreflang、sitemap 的语言（COMMENT 与列一起写进 SQL 文件）。
//
// **刻意不带 TableName**：migrator.apply 的默认 CheckSQL 是
// SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = ?
// 且只在 CheckSQL 里出现 `?` 时才把 TableName 传进去 —— TableName 为空即查「table_name 等于空串」，
// 恒为 0，于是整条 SQL 每次启动都会执行。
//
// 为什么这里**必须**这样：pages 表**早已存在**，一旦带上 TableName，CheckSQL 立刻命中
// count > 0 → 整条迁移被「迁移对象已存在，跳过」，`ALTER TABLE … ADD COLUMN IF NOT EXISTS`
// 永不执行 —— 存量库永远没有这一列，而全新库有：两边静默分叉（484–486 就是为此刻意不填
// TableName，见 register_sys_reference.go）。代价是文件里**只能放幂等语句**。
//
// 注册方式：由 register.go 的 init() 显式调用（与 484–486 同）。
func registerPageExcludedLangs() {
	registerPageExcludedLangsOnce.Do(func() {
		register(Migration{
			Version: "491-page-excluded-langs",
			SQL:     mustSQL("491_page_excluded_langs.sql"),
		})
	})
}

// registerPageExcludedLangsOnce 让重复调用成为空操作（不会重复 register）。
var registerPageExcludedLangsOnce sync.Once
