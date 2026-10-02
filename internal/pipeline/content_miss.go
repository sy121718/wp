package pipeline

// content_miss.go — 构建期内容译文缺失告警（page / presentation 共用）。

import "go_wp/pkg/logger"

// LogContentTranslationMisses L3：缺译文已回退原文，只记日志不阻断构建。
func LogContentTranslationMisses(lang string, candidates int, misses int64) {
	if misses <= 0 {
		return
	}
	// 字段名刻意**把量纲写进名字**（V2）：这一行原先叫 candidates / misses，
	// 并排打出来读着像分子分母，而两者量纲不同、不能相除（见 ManifestTranslationMisses）——
	// 排查的人照着算就会得出「>100% 缺失率」。
	// 值本身没变，只是让日志一眼能分清哪个是字段数、哪个是取词次数。
	logger.Scene("build").
		With("lang", lang).
		With("fallbackFieldCount", candidates). // 去重后的可翻译字段数
		With("lookupMissCount", misses).        // 渲染期取词未命中的调用次数（不去重）
		Warn("构建期内容译文缺失，已回退原文（fallbackFieldCount 是字段数、lookupMissCount 是取词次数，两者量纲不同不可相除；补齐译文后需重建，docs/06-D §9）")
}
