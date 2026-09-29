package producthttp

// product_translate.go — 把取词函数注入请求 ctx（service 层展示文案的取词通道）。

import (
	"github.com/gin-gonic/gin"

	productservice "go_wp/internal/module/product/service"
	"go_wp/internal/web/shell"
)

// productTranslateMiddleware 让本模块的 service 能按请求语言取词。
//
// service 层没有语言上下文（语言来自后台 Cookie / Accept-Language，只有 gin.Context
// 知道），而它要产出**展示文案**：内置定价 / 标签规则的展示名与描述、筛选条件的可读
// 标签、调价行的状态与原因。这里把 shell.TranslateFor(c) 放进 Request 的 Context，
// handler 里 `ctx := c.Request.Context()` 因此自带取词函数 ——
// 既不必给每个 contract 方法加 tr 参数，也不会漏掉任何一个调用点（漏一个的表现是
// 那句话在英文界面上还是中文，且没有任何测试会红）。
//
// 未挂本中间件的路径（单测直调 service）行为不变：取词函数缺失时 service 回落中文兜底
// （见 service/product_translate.go 的 translateFrom）。
func productTranslateMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		tr := productservice.TranslateFunc(shell.TranslateFor(c))
		c.Request = c.Request.WithContext(productservice.WithTranslate(c.Request.Context(), tr))
		c.Next()
	}
}
