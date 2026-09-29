package webhookhttp

// webhook_delivery_text.go — 投递日志出口的文案还原（see enums/webhook_delivery_err.go）。

import (
	webhookdto "go_wp/internal/module/webhook/dto"
	webhookenums "go_wp/internal/module/webhook/enums"
	"go_wp/pkg/i18n"
)

// webhookDeliveryTexts 把投递日志里的 last_error 编码按请求语言还原（原地改写）。
//
// 历史行不受影响：FormatDeliveryErr 对中文原文 / 出站客户端原文原样返回。
func webhookDeliveryTexts(lang string, items []*webhookdto.DeliveryItem) {
	tr := i18n.TranslateFunc(lang)
	for _, it := range items {
		if it == nil {
			continue
		}
		it.LastError = webhookenums.FormatDeliveryErr(tr, it.LastError)
	}
}
