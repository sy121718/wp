package migrations

// register_block_version_conflict_i18n.go — 677：并发版本冲突的对外文案（B2）。
//
// 判据逐条枚举本批的 1 个 key、要求中英两行都在（共 2 行）：只数一种语言时另一种
// 缺行仍判已完成，那条缺的词永远补不上（AGENTS.md「seed 门槛必须逐条枚举本批对象」）。
// 不用 LIKE：将来任何新增的 block 域错误 key 都会让门槛提前满足。

import "sync"

func init() { registerBlockVersionConflictI18n() }

var blockVersionConflictI18nOnce sync.Once

func registerBlockVersionConflictI18n() {
	blockVersionConflictI18nOnce.Do(func() {
		registerSeed(Seed{
			Version:   "677-block-version-conflict-i18n",
			TableName: "sys_i18n",
			ConditionSQL: `SELECT CASE WHEN COUNT(*) >= 2 THEN 1 ELSE 0 END FROM sys_i18n
				WHERE lang IN ('zh-CN', 'en-US') AND item_key IN ('ErrBlockVersionConflict')`,
			SQL: mustSQL("677_i18n_block_version_conflict.sql"),
		})
	})
}
