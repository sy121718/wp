package producthttp

import (
	"go_wp/internal/middleware/builtin"
	productservice "go_wp/internal/module/product/service"
	"go_wp/internal/shell"
	"go_wp/pkg/money"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// product_page_util.go — 商品各页面共用的分页、取值与分档小工具。
//
// 写表单的失败回填（formEcho 一族 / 各表单的 XxxFormEcho 结构体 / 片段重渲染）已随商品页
// 最后一批迁到 JSON 写基座而删除：基座在原页发 fetch、失败只写结果区，用户的输入从头到尾
// 没离开过 DOM。字段归一化口径现在在 static/js/ui/json_write.js，由
// public/test/product/json_write.test.cjs 覆盖。
//
// 剩下的分档判据只有 isHXRequest 一处实现：仍在 htmx 路径上的写动作（捆绑页等）靠它决定
// 「回片段还是整页」，两个出口必须认同同一个信号，否则会出现「片段按 htmx 走、跳转按原生走」
// 这种自相矛盾的档位。

// formProductID 取表单里的商品 id：优先 productId，再回落 id。
//
// 两个名字都是**既有字段名**，不下令重命名：详情页的商品级表单（属性引用 / 分类与品牌 /
// 手工标签）用的隐藏域是 id（其值就是商品 id），子资源表单（变体 / 评分）用的是 productId。
// 只适用于这两者都指向商品本身的地方 —— 变体删除 / 评分删除的 id 是**子资源 id**，
// 那两个 handler 必须读 productId，绝不能走这里的回落。
//
// 本文件另有商品域后台子列表（属性 / 分类 / 品牌 / 标签）共用的分页与取址助手，
// 以及 product_handle.go 并进来的 operatorFromContext（会话里的操作人）。

func formProductID(c *gin.Context) string {
	if id := strings.TrimSpace(c.PostForm("productId")); id != "" {
		return id
	}
	return strings.TrimSpace(c.PostForm("id"))
}

// isHXRequest 这次请求是否由 htmx 发起（HX-Request: true）。
//
// 大小写不敏感、去首尾空白：与 pkg/reqparams 的请求头判据同口径。
func isHXRequest(c *gin.Context) bool {
	return strings.EqualFold(strings.TrimSpace(c.GetHeader("HX-Request")), "true")
}

// formParseMemory 解析提交表单时的内存上限（与 gin 的 MaxMultipartMemory 默认值一致）。
//
// 手工调 ParseMultipartForm 的地方共用它：gin 的 PostForm 缓存是惰性填充的，
// 直接读 c.Request.PostForm 在未触发解析时是空 map。
const formParseMemory = 32 << 20

// formatAmount 数值 → 后台展示文本（整数不带小数尾巴）。
//
// 唯一实现在 pkg/money.FormatYuan（审计 CQ-013：此前与商品构建期的 formatPrice
// 逐字节重复）。保留本名字是因为同包多个后台页面文件共用它。
func formatAmount(v float64) string { return money.FormatYuan(v) }

// formatNullableAmount 可空数值 → 展示文本。
//
// 唯一实现在 pkg/money.FormatYuanOrDash（与 formatAmount 同一处理：本名是同包多个后台页共用）。
func formatNullableAmount(v *float64) string { return money.FormatYuanOrDash(v) }

// containsString 判断切片是否含目标值。
func containsString(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// firstNonEmpty 取第一个非空字符串。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

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

// productSubListPageSize 商品域后台子列表的每页条数。
//
// 唯一实现在 service 的 ProductSubListPageSize：属性页的分页已随取数下沉进读模型，
// 两处各写一个 20 时改一处会让同一批子列表页出现两种每页条数。
const productSubListPageSize = productservice.ProductSubListPageSize

// clampPageToTotal 把页码收敛到实际总页数以内（total=0 时收敛到第 1 页）。
//
// 唯一实现在 service 的 ClampPageToTotal（那条注释写明了「为什么在取数之前收敛」）：
// 属性页已随取数下沉用上服务侧那一份，这里留着本名字是因为同包还有几页（标签 / 分类 / 品牌）
// 在控制器里做取数 —— 两份实现会让同一条判据在其中一页上悄悄失效。
func clampPageToTotal(page, size int, total int64) int {
	return productservice.ClampPageToTotal(page, size, total)
}

// listFilterQuery 列表页筛选条件的查询串（筛选表单与分页基地址共用同一份口径）。
func listFilterQuery(projectID, keyword string) url.Values {
	q := url.Values{}
	if v := strings.TrimSpace(projectID); v != "" {
		q.Set("project", v)
	}
	if v := strings.TrimSpace(keyword); v != "" {
		q.Set("keyword", v)
	}
	return q
}

// productListBaseURL 列表页的分页基地址（**不含** page/limit：分页组件自己拼）。
//
// 把筛选条件拼进基地址，翻页时关键词才不会丢；反过来说，基地址里塞了 page 就会出现两个
// page 参数（浏览器取第一个），翻页看起来「点了没反应」—— 与 productListFilterURL
// （商品列表专用，参数固定为 keyword + status）同一条理由。
func productListBaseURL(path string, q url.Values) string {
	if enc := q.Encode(); enc != "" {
		return path + "?" + enc
	}
	return path
}

// listPageSlice 切出「第 page 页」，并返回**收敛后**的页码。
//
// 保留旧分页助手供同包既有测试使用；分类页现走受限分类读。
//
// 页码收敛与 BuildPagination 同一条规则：page=999 时若不先收敛，会出现「表格为空、
// 分页条却显示第 999 页」这种自相矛盾的组合（BuildPagination 拿到的 total 与 page
// 不同源时就会这样）。
func listPageSlice[T any](all []T, page, size int) (rows []T, current int) {
	if size < 1 {
		size = productSubListPageSize
	}
	if page < 1 {
		page = 1
	}
	pages := (len(all) + size - 1) / size
	if pages < 1 {
		pages = 1
	}
	if page > pages {
		page = pages
	}
	from := (page - 1) * size
	if from > len(all) {
		from = len(all)
	}
	to := from + size
	if to > len(all) {
		to = len(all)
	}
	return all[from:to], page
}

// ==========================================================================
// 会话里的操作人（接口面与页面面共用）
// ==========================================================================
// operatorFromContext 从会话取操作人（issue #13 定价留痕 / issue #19 变更记录共用）。
//
// 优先取登录名：留痕与变更记录都要能直接读懂「谁改的」（与库存流水的 operator_id 同口径，
// 后台页面路径也一直是用登录名）。登录名缺失（脚本 / 测试路径）时退回数值 id ——
// 会话中间件写入的 user_id 是 int64，这里只做展示用的文本化，不参与任何鉴权判断。
// 两者都没有时返回空串：留痕字段允许为空。
//
// 不走 builtin.CurrentUserIDText：要保留「先登录名、后 id 文本」的组合语义，且非 int64
// 原始值分支要照旧（脚本 / 测试路径可能写入别的形状），builtin.CurrentUserID 入口只认 int64。
func operatorFromContext(c *gin.Context) (id string) {
	if name := strings.TrimSpace(builtin.GetUsername(c)); name != "" {
		return name
	}
	value, exists := c.Get("user_id")
	if !exists {
		return ""
	}
	switch v := value.(type) {
	case int64:
		return strconv.FormatInt(v, 10)
	case int:
		return strconv.Itoa(v)
	case string:
		return v
	case uint64:
		return strconv.FormatUint(v, 10)
	}
	return ""
}
