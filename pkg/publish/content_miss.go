package publish

import "go_wp/pkg/logger"

func LogContentTranslationMisses(lang string, candidates int, misses int64) {
	if misses <= 0 {
		return
	}

	logger.Scene("build").
		With("lang", lang).
		With("fallbackFieldCount", candidates).
		With("lookupMissCount", misses).
		Warn("构建期内容译文缺失，已回退原文（fallbackFieldCount 是字段数、lookupMissCount 是取词次数，两者量纲不同不可相除；补齐译文后需重建，docs/06-D §9）")
}
