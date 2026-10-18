package runtimefragment

// facing.go — 片段层的错误文案：用共享判据（pkg/facing）自己取词。
//
// 不再向业务模块要「一句成品文案」（契约 FacingTexter 已删）：那会让模块的 service
// 承担取词，而取词需要请求语言、属出口的事。片段层拿得到 r.Lang，判据又只有一份，
// 所以这里自己翻；模板化之后这段会随片段数据槽一起消失。

import (
	"strings"

	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
	"go_wp/pkg/response"
)

// fragmentFacingText 业务错误 → 当前语言一句文案；非业务错误 → 归口文案；err 为 nil → fallback。
func fragmentFacingText(r *Request, err error, fallback string) string {
	if err == nil {
		return fallback
	}
	lang := ""
	if r != nil {
		lang = r.Lang
	}
	if fact, ok := facing.FactOf(err); ok && fact.Business {
		if text := strings.TrimSpace(i18n.Translate(fact.Key, fact.Fallback, lang)); text != "" {
			return text
		}
		return fallback
	}
	if text := strings.TrimSpace(i18n.Translate(response.InternalKey, response.InternalFallback, lang)); text != "" {
		return text
	}
	return fallback
}
