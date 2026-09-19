package migrations

import "sync"

// register_block_usage_i18n.go — 块删除保护的引用类别词条（迁移 297，审计 ARCH-02）。
//
// 为什么单独成一个主题文件：这批词条只服务一件事 —— 块删除被拒绝时说明「是哪一类
// 引用在挡路」。混进 register_admin_i18n.go（名字与注释写着「admin 后台词条」）会让
// 「这批词条是谁的」在 review 时看不出来。
//
// 六个 key 与 blockcontract.BlockUsageKind 一一对应（真源在
// internal/module/block/contract/block_usage.go），由
// internal/module/block/inbound/http 的提示渲染按当前语言取词。
func registerBlockUsageI18n() {
	registerBlockUsageI18nOnce.Do(registerBlockUsageI18nSeed)
}

// registerBlockUsageI18nOnce 让「自注册的 init()」与「register.go 里的显式一行」同时存在也安全
// （与 register_bulk_notice_i18n.go 同一处理）：重复调用只是空操作。
var registerBlockUsageI18nOnce sync.Once

// registerBlockUsageI18nSeed 注册 297 的 seed（真正干活的那一半，被 Once 包一层）。
func registerBlockUsageI18nSeed() {
	// 判定枚举本批**全部 7 个 key**，不用全库行数：用全库行数会被同期其它批次的行满足
	// 而静默跳过（本仓库踩过，理由见 226/277）。
	// ConditionSQL 里没有占位符 —— 迁移器传进来的 ? 是表名，拿它当 key 会让判定恒为 0、
	// 每次启动重跑（178 踩过）。
	// 判据与 SQL 的 key 列表必须同批改：只补 SQL 不改判据时，老库上条件已满足（>=6）
	// 会直接跳过，新词条永远灌不进去（正是 237 注释里那种漏词的成因）。
	registerSeed(Seed{
		Version:   "297-block-usage-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 7 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' AND item_key IN (" +
			"'MsgBlockUsagePageDocument', 'MsgBlockUsagePageStructure', 'MsgBlockUsagePageRevision', " +
			"'MsgBlockUsageThemeSlot', 'MsgBlockUsageBlockDocument', 'MsgBlockUsageContentTemplate', " +
			"'MsgBlockUsagePresentationInstance')",
		SQL: mustSQL("297_i18n_seed_block_usage_i18n.sql"),
	})
}
