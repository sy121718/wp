package migrations

import "sync"

func registerFacingMissingKeysI18n() {
	registerFacingMissingKeysI18nOnce.Do(registerFacingMissingKeysI18nSeed)
}

var registerFacingMissingKeysI18nOnce sync.Once

func registerFacingMissingKeysI18nSeed() {

	registerSeed(Seed{
		Version:   "429-i18n-facing-missing-keys",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 4 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'en-US' AND item_key IN (" +
			"'ErrInvalidRange','ErrInvalidParent','ErrInvalidSlot','ErrSlotPageMiss')",
		SQL: mustSQL("429_i18n_facing_missing_keys.sql"),
	})
}

func init() { registerFacingMissingKeysI18n() }
