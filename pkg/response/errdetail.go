package response

import (
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/i18n"
)

// detailText 把 Tail 里的编码明细（i18n.ErrorDetail / JoinErrorDetails 产物）逐段取词后拼成品句。
// 明细 key 一律不带出门：缺词或参数不足按共享归口文案，控制串直接丢弃。
//
// 这里不猜「缺词」：拿回与归口文案相同的文本也可能是参数不足，或译文恰好等于归口句，
// 那种猜测会把正常链路记成缺词。缺词只在真正查不到 key 的位置记（见 msg.go 的 FillText）。
func detailText(c *gin.Context, raw string) string {
	parts, ok := i18n.ParseErrorDetails(raw)
	if !ok {
		if i18n.EncodedDetailLike(raw) {
			return InternalText(c)
		}
		return raw
	}
	seen := make([]string, 0, len(parts))
	for _, part := range parts {
		text := FillText(c, part.Key, "", part.Args)
		if text != "" {
			seen = append(seen, text)
		}
	}
	if len(seen) == 0 {
		return InternalText(c)
	}
	return strings.Join(seen, "；")
}
