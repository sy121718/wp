package producthttp

import (
	productservice "go_wp/internal/module/product/service"
	"go_wp/internal/shell"

	"github.com/gin-gonic/gin"
)

// product_page_util.go — 商品各页面**自己**才有的两件小事。
//
// 这一批把能共享的都搬走了（判据：别的模块会不会要同样的行为）：
//   - 分页基地址、筛选查询串、页码收敛、切片分页 → pkg/pagination
//     （BaseURL / Query / ClampPage / Slice）；
//   - 操作人取值 → internal/middleware/builtin.OperatorName；
//   - 金额展示 → pkg/money.FormatYuan / FormatYuanOrDash；
//   - 切片包含 → slices.Contains。
// 留在本文件的只有「商品后台表单的解析上限」与「本模块 service 的取词注入」——
// 两者的键名与注入点都属于商品模块。
//
// 写表单的失败回填（formEcho 一族 / 各表单的 XxxFormEcho 结构体 / 片段重渲染）已随商品页
// 最后一批迁到 JSON 写基座而删除：基座在原页发 fetch、失败只写结果区，用户的输入从头到尾
// 没离开过 DOM。字段归一化口径在 static/js/ui/json_write.js，由
// public/test/product/json_write.test.cjs 覆盖。

// formParseMemory 解析提交表单时的内存上限（与 gin 的 MaxMultipartMemory 默认值一致）。
//
// 手工调 ParseMultipartForm 的地方共用它：gin 的 PostForm 缓存是惰性填充的，
// 直接读 c.Request.PostForm 在未触发解析时是空 map。
const formParseMemory = 32 << 20

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
