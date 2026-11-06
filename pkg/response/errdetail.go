package response

import (
	"github.com/gin-gonic/gin"

	"go_wp/pkg/i18n"
)

// errorDetailsOf 把 Tail 里的受控编码明细（i18n.ErrorDetail / JoinErrorDetails 产物）
// 提取成 data.details 的结构化数组，交给数据消费者按需读取，不进入主 message。
//
// 只接受合法编码：明文 tail、损坏编码与内部原文一律丢弃，不把任意文本透出。
// 明细 key 与参数保持机器可读原样，不在出口二次取词，避免已译 message 变形。
func errorDetailsOf(raw string) []gin.H {
	parts, ok := i18n.ParseErrorDetails(raw)
	if !ok {
		return nil
	}
	details := make([]gin.H, 0, len(parts))
	for _, part := range parts {
		args := make(map[string]string, len(part.Args))
		for name, value := range part.Args {
			args[name] = value
		}
		details = append(details, gin.H{"key": part.Key, "args": args})
	}
	return details
}
