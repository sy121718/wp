package navigationservice

// navigation_facing.go — 业务错误文案出口（契约 FacingTexter）。
// 与 inbound/http 同源：同一份白名单、同一个「key：定位」拆法，差别只在取词入口
//（这里走 pkg/i18n.Translate，拿不到 *gin.Context）。

import (
	"strings"

	"go_wp/pkg/i18n"

	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationenums "go_wp/internal/module/navigation/enums"
)

// 编译期断言：业务错误文案出口（工作台检查器这类跨模块消费者用）。
var _ navigationcontract.FacingTexter = (*Service)(nil)

// FacingText 把本模块业务错误转成指定语言下可直接展示的一句话（契约 FacingTexter）。
//
// 与 inbound/http 的两个出口同源：同一份白名单、同一个「key：定位」拆法（判据都在 enums），
// 差别只在取词入口 —— 这里走 pkg/i18n.Translate（消费者拿不到 *gin.Context），
// 那里走 pkg/response 的取词。两处说法因此不会漂。
func (s *Service) FacingText(lang string, err error) string {
	tr := func(key, fallback string) string { return i18n.Translate(key, fallback, lang) }
	if err != nil {
		if msg, ok := navigationenums.HitFacingMessage(err.Error()); ok {
			if key, detail, hasDetail := navigationenums.SplitFacingDetail(msg); hasDetail {
				return tr(key, key) + facingDetailSepForLang(lang) + detail
			}
			if key, param, hasParam := strings.Cut(msg, "|"); hasParam {
				// 带参形态：%s 由这里填（pkg/i18n 只做取词，不做占位符替换）。
				return strings.Replace(tr(key, key), "%s", param, 1)
			}
			return tr(msg, msg)
		}
	}
	return tr(navigationenums.ErrInternal, "操作失败，请稍后重试（细节只进日志）")
}

// facingDetailSepForLang 定位信息的分隔符按语言取（中文全角，其余「: 」）。
// 与 inbound/http 的 facingDetailSep 同一取值口径：提示条最终是给人看的一句话。
func facingDetailSepForLang(lang string) string {
	if strings.HasPrefix(strings.TrimSpace(lang), "zh") {
		return navigationenums.FacingDetailSep
	}
	return ": "
}
