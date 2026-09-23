package migrations

import "sync"

// register_plugin_patrol_fold_i18n.go — 插件巡检折叠卡的两个新词条（迁移 426，审计 02-L P1-17）。
//
// 背景：/admin/plugins 的四类产物对账结果此前是四张独立卡片，一旦出现就把页尾的「安装插件」
// 推到第 5 屏。本批收进一张 <details class="section-fold card">，summary 常显并带不一致**类数**
// 与告警色 —— 折叠省版面，但孤儿 schema 里可能有真实数据，安全信号不能折起来就看不见。
//
// 本批只新增 2 个 key（折叠卡标题 + 不一致计数量词）× 中英 = 4 行：
// 279 已 seed 的 12 个 admin.plugins.patrol.* 词条一个都没改（四个小节的标题与说明原样保留）。
//
// 为什么单独一个文件而不是并进 register_plugin_patrol_i18n.go：那份文件的注释与判定都写着
// 「279 这一批 12 个 key」，把新批次的行塞进它的 ConditionSQL 会让「这批词条是谁的」在 review
// 时看不出来，也会让 279 的门槛随本批变化。新批次自带 init()、自带判定，互不牵连。
func registerPluginPatrolFoldI18n() {
	registerPluginPatrolFoldI18nOnce.Do(registerPluginPatrolFoldI18nSeed)
}

// registerPluginPatrolFoldI18nOnce 让重复调用成为空操作。
var registerPluginPatrolFoldI18nOnce sync.Once

// registerPluginPatrolFoldI18nSeed 注册 426（真正干活的那一半）。
func registerPluginPatrolFoldI18nSeed() {
	// 门槛 = 本批 2 个 key 的 **en-US** 行都在（= 2 行）。挑 en-US 而不是 zh-CN 是有意的：
	// 中文行与 Go 侧的 fallback 同形，容易在别处被顺手加上，按 zh-CN 计数会让门槛在
	// 「本批还没跑」时就成立，整批词条被静默跳过（404 / 其他 i18n 批次同口径）。
	//
	// ConditionSQL 由迁移器 db.Raw 直接执行，**没有任何参数替换** —— 判定要用的 key 只能写成
	// SQL 字面量；写成 item_key = ? 会永远查不到行，让这条迁移每次启动重跑（178 踩过）。
	registerSeed(Seed{
		Version:   "426-plugin-patrol-fold-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'en-US' AND item_key IN (" +
			"'admin.plugins.patrol.title','admin.plugins.patrol.badge_kinds')",
		SQL: mustSQL("426_plugin_patrol_fold_i18n.sql"),
	})
}

func init() { registerPluginPatrolFoldI18n() }
