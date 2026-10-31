package migrations

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
