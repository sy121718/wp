package pipeline

// content_miss.go — 构建期内容译文缺失告警（page / presentation 共用）。

import "go_wp/pkg/logger"

// LogContentTranslationMisses L3：缺译文已回退原文，只记日志不阻断构建。
func LogContentTranslationMisses(lang string, candidates int, misses int64) {
	if misses <= 0 {
		return
	}
	logger.Scene("build").
		With("lang", lang).
		With("candidates", candidates).
		With("misses", misses).
		Warn("构建期内容译文缺失，已回退原文（补齐译文后需重建，docs/06-D §9）")
}
