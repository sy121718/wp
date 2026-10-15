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

// product_page_util.go — 商品各页面共用的表单回显、分页与取值小工具。
//
// formEcho 一族是「写失败时原地留住输入」的底座：值从**本次提交**里读回，
// 归一化口径（去首尾空白、丢空项、多值保序）就收在这里。
// **怎么落进视图**不在这个文件：每个表单自己有 dto 的 `XxxFormEcho` 结构体（字段即清单），
// 由 adminui.FillForm 按 `form:` 标签填 —— 回填键仍必须叫 `FormEcho` 并带前缀，
// 片段与页面共用同一份渲染 data，通用键名（`Form`）会撞上页面已有键。

// formProductID 取表单里的商品 id：优先 productId，再回落 id。
//
// 两个名字都是**既有字段名**，不下令重命名：详情页的商品级表单（属性引用 / 分类与品牌 /
// 手工标签）用的隐藏域是 id（其值就是商品 id），子资源表单（变体 / 评分）用的是 productId。
// 只适用于这两者都指向商品本身的地方 —— 变体删除 / 评分删除的 id 是**子资源 id**，
// 那两个 handler 必须读 productId，绝不能走这里的回落。
// 「提交失败原地留住输入」出口（路径 A 渐进增强）。
//
// 形状与口径**照批 1 的商品建表单**（product_core.go 的 productCreateFormView + admin/product/product_create_form.html），
// 分档判据只有一份（product_page_util.go 的 isHXRequest / hxFragment / formEchoFrom）：
//
//	· htmx 请求：失败返 200 + 片段（错误槽 + 回填后的表单）—— 抽屉原地留住已填内容；
//	· 原生请求：整页提示（shell.RenderJump）回列表页（取代原先的 302 + ?err=）；
//	· 成功出口：一律整页提示 —— htmx 由 shell.RenderJump 输出 HX-Redirect（整页跳转），
//	  原生渲染提示页。
//
// 为什么单独一个文件：属性页有三个抽屉表单（新建属性组 / 编辑属性组 / 保存属性值），
// 每个抽屉的数据既要在**页面首屏**渲染（初值 = 库里当前值），又要在**提交失败**时由
// handler 单独重渲染（值 = 用户刚打的字）。两处各拼一份 data 必然分叉，而分叉不会编译
// 失败 —— 只会让失败重渲染缺一个键，于是 Jet 在那一行渲染失败、**整块响应被丢弃（500）**，
// 而 htmx 把 5xx 判成不 swap：用户点「保存」后页面上毫无反应（比白页更难查）。
// 因此组装只有 attrGroupFormData / attrValuesFormData 两个入口，首屏与失败共用。

// redirectWhere 已提升到 product_page_util.go（同包共享，本文件仍可直接调用）。
// 提升理由：它是「HTMX 写表单分档」的出口之一，写表单的失败片段再走同一个口径，
// 两处各写一份头判断迟早会分叉。

// —— HTMX 写表单的失败分档（各写表单共用）——
//
// 表单保留原生 `method="post" action=…`，另加 `hx-post`（htmx 优先拦住 submit）；
// 服务端按 `HX-Request` **分档**：
//
//	· htmx 请求：失败返 **200 + 片段**（错误槽 + 回填后的表单）—— 抽屉 / 页面原地留住输入；
//	· 原生请求：整页提示（shell.RenderJump）回来源页（取代原先的 302 + ?err=）。
//
// 分档判据只有 isHXRequest 一处实现：两个出口必须认同同一个信号，否则会出现
// 「片段按 htmx 走、跳转按原生走」这种自相矛盾的档位。
//
// 原 redirectWhere / hxFragment 两个 helper 已删除：写动作的结论统一由 shell.RenderJump
// 渲染（它对 htmx 自己发 HX-Redirect），回填片段分支由各 handler 就地 `c.HTML` 渲染。

// product_view.go - 商品页的视图构造（变体行、规格标签、金额/评分格式化、仓库下拉
// 与捆绑构成面板）。

// ---------------------------------------------------------------------------
// 商品域后台子列表（属性 / 分类 / 品牌 / 标签）共用的分页与取址助手。
//
// 为什么落在这个文件里：本批的独占文件清单只含三个 handler 与四个模板，共享助手所在的
// product_page_util.go 不在其中（有并行任务在同包改别的页面，动它必然冲突）；同包内位置
// 不影响可用性，四页都能直接调用。
// ---------------------------------------------------------------------------
//
// 并进来的还有 product_handle.go 的 operatorFromContext（会话里的操作人）。

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

// formEchoMemory 解析提交表单时的内存上限（与 gin 的 MaxMultipartMemory 默认值一致）。
const formEchoMemory = 32 << 20

// formEcho 这次请求的表单快照（失败片段回填用）。
//
// 值只来自**这次提交**，不回查数据库：回填要的是「用户刚打的字」，而不是「库里的旧值」
// （新建路径上后者根本不存在）。
type formEcho struct {
	// values 归一化后的提交值：去首尾空白、丢空项（与 reqparams.FieldValue 同口径）。
	// 多值字段保留**提交顺序** —— 顺序在本域有语义（「按本表顺序第一个勾中的仓是认领仓」），
	// 排序或去重都会改掉它。
	values url.Values
}

// formEchoFrom 取这次请求的表单快照。
func formEchoFrom(c *gin.Context) formEcho {
	vals := url.Values{}
	if c == nil || c.Request == nil {
		return formEcho{values: vals}
	}
	// 走标准库的解析入口（gin 的 PostForm 也走这里）：urlencoded 与 multipart 两条路径
	// 都会把字段填进 req.PostForm。解析错误一律忽略 —— 解析失败就当「没提交」，
	// 回填退化成空表单；提交本身合法与否由各 handler 的业务校验回答，不在这里变成 500。
	_ = c.Request.ParseMultipartForm(formEchoMemory)
	for key, list := range c.Request.PostForm {
		for _, raw := range list {
			if v := strings.TrimSpace(raw); v != "" {
				vals.Add(key, v)
			}
		}
	}
	return formEcho{values: vals}
}

// value 取单值字段的回填值（缺失返回空串）：文本 / 下拉 / 隐藏域用。
func (e formEcho) value(name string) string {
	if list := e.values[name]; len(list) > 0 {
		return list[0]
	}
	return ""
}

// list 取多值字段的完整回填值（复选框组 / 同名多次提交），保留提交顺序。
func (e formEcho) list(name string) []string {
	return e.values[name]
}

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

// parseFloat 解析表单里的金额字段（非法值返回错误，由调用方忽略）。
func parseFloat(s string) (float64, error) {
	return strconv.ParseFloat(s, 64)
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

// firstNonEmptyString 取第一个非空值。
func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// slashSlug URL 段 → 路径形态（空 slug 返回空串）。
//
// 商品 / 分类 / 品牌详情页的路径由各自的 slug 构成，编辑期没有发布实例时用它做
// URL 干净度判定的近似值：slug 本身就是要检查的对象（长度、大小写、参数），
// 不是编出来的路径。
func slashSlug(slug string) string {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return ""
	}
	return "/" + slug
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
