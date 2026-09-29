package migrations

import "sync"

// register_retire_seo_grade_keys.go — 退役孤儿词条 admin.seo.grade.*（458）。
//
// 见 458_retire_seo_grade_keys.sql 的头部：product 侧的等级文案映射收编到
// seoscore.ScoreGradeText（internal/seo/score_grade.go）之后，这 5 个 key 在代码里
// 失去全部引用者 —— 它们与 content / project 用的 admin.seo.score.grade.* 逐字相同。
//
// 为什么挂在 **seed 台账**而不是 Migration 台账：
//
//	· 449 的 SQL 里仍有这 10 行 INSERT（历史迁移 SQL 不改），它一旦重跑就会把它们插回来；
//	· Migrations 全部先跑、Seeds 后跑（migrator.go），删除若停在 register 台账里，
//	  那一轮的净结果就是「库里有孤儿」，要等下一轮启动才清；
//	· 挂在 seed 台账、版本 458 > 449，同一轮内「插入在前、删除在后」，净结果恒为
//	  「这些 key 不在库里」—— 与 418 / 419 同一形态。
//
// 注册方式：本文件自带 init()（与 register_orphan_i18n_retire.go 同形）。
func registerRetireSeoGradeKeys() {
	registerRetireSeoGradeKeysOnce.Do(registerRetireSeoGradeKeysSeed)
}

// registerRetireSeoGradeKeysOnce 让重复调用成为空操作。
var registerRetireSeoGradeKeysOnce sync.Once

// registerRetireSeoGradeKeysSeed 注册 458（真正干活的那一半）。
func registerRetireSeoGradeKeysSeed() {
	// 判定方向：applySeed 的语义是「ConditionSQL 返回 > 0 → 跳过」（migrator.go）。
	// 所以「还有这些行才需要跑」要写成 **COUNT(*) = 0 → 1**（一行都不剩 = 已完成 = 跳过），
	// 而**不能**写成「10 行齐全才跑」：那样「10 行都在」会返回 1 被跳过，删除永远不执行；
	// 更隐蔽的是「执行中途失败、只删掉一部分」的库会被永久跳过（残留再不清）。
	// 偏差方向按 AGENTS.md 取「宁可重跑，不可静默跳过」：本条件只在删除完成后成立，
	// 重复执行只是多跑一次幂等 DELETE。
	//
	// ConditionSQL 由迁移器 db.Raw 直接执行、没有参数替换：key 只能写在 SQL 字面量里（178 踩过）。
	registerSeed(Seed{
		Version:   "458-i18n-retire-seo-grade-keys",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 0 THEN 1 ELSE 0 END FROM sys_i18n WHERE item_key IN (" +
			"'admin.seo.grade.green','admin.seo.grade.lightgreen','admin.seo.grade.yellow'," +
			"'admin.seo.grade.red','admin.seo.grade.redBlocking')",
		SQL: mustSQL("458_retire_seo_grade_keys.sql"),
	})
}

func init() { registerRetireSeoGradeKeys() }
