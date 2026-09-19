package migrations

// registerShowcaseBlueprints 注册「展示页 Blueprint」种子（287）。
//
// 为什么要独立文件：register_core.go 由集成方在合并期统一改动，本票新增的迁移注册
// 先放在这里；合入时把 registerShowcaseBlueprints() 整体搬进 register_core.go
// 并删掉本文件，或直接保留本文件都可以 —— init() 的注册顺序不影响执行顺序：
// 迁移按 compareVersion 排序、种子按版本号排序，RunSeeds 在所有结构迁移之后执行。
//
// 幂等判定：本批 6 个蓝图行 + 6 条 version=1 版本行都在时结果才为 12（跳过）。
// **不能**只按单个 id 或 name LIKE '展示页 %' 计数 —— 编辑者改过其中一条的名字时，
// 判定会恒为真而静默跳过整批，缺失的那条永远不会被补回。
func registerShowcaseBlueprints() {
	const ids = "'3f1b6a52-7c4d-4a01-9e10-11f0a6b2c301', '3f1b6a52-7c4d-4a01-9e10-11f0a6b2c302', " +
		"'3f1b6a52-7c4d-4a01-9e10-11f0a6b2c303', '3f1b6a52-7c4d-4a01-9e10-11f0a6b2c304', " +
		"'3f1b6a52-7c4d-4a01-9e10-11f0a6b2c305', '3f1b6a52-7c4d-4a01-9e10-11f0a6b2c306'"
	registerSeed(Seed{
		Version:   "287-showcase-blueprints",
		TableName: "blueprints",
		ConditionSQL: "SELECT CASE WHEN " +
			"(SELECT COUNT(*) FROM blueprints WHERE id IN (" + ids + ")) + " +
			"(SELECT COUNT(*) FROM blueprint_versions WHERE version = 1 AND blueprint_id IN (" + ids + ")) " +
			"= 12 THEN 1 ELSE 0 END",
		SQL: mustSQL("287_showcase_blueprints.sql"),
	})
}

func init() { registerShowcaseBlueprints() }
