package migrations

import "sync"

var registerBlocksVersionPresetOnce sync.Once

func init() { registerBlocksVersionPreset() }

func registerBlocksVersionPreset() {
	registerBlocksVersionPresetOnce.Do(func() {
		register(Migration{
			Version:   "676-blocks-version-preset",
			TableName: "blocks",

			CheckSQL: `SELECT CASE WHEN (
				SELECT COUNT(*) FROM pg_attribute
				WHERE attrelid = to_regclass('blocks')
				  AND attname IN ('version', 'preset_key', 'preset_version')
				  AND NOT attisdropped
			) = 3 AND (
				SELECT COUNT(*) FROM pg_constraint
				WHERE conrelid = to_regclass('blocks')
				  AND conname IN ('ck_blocks_version_positive', 'ck_blocks_preset_pair')
			) = 2 THEN 1 ELSE 0 END`,
			SQL: mustSQL("676_blocks_version_preset.sql"),
		})
	})
}
