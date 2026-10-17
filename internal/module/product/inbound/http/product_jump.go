package producthttp

import (
	"errors"
	"go_wp/internal/adminui"
	"go_wp/internal/middleware/builtin"
	inventoryenums "go_wp/internal/module/inventory/enums"
	presentationenums "go_wp/internal/module/presentation/enums"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/internal/shell"
	"go_wp/pkg/backpath"
	bulkids "go_wp/pkg/bulkids"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// product_jump.go — product 后台页的**共享出口原语**：整页提示（shell.RenderJump）、回跳地址、
// 回执文案事实，以及**错误事实与文案出口**（原先的 product_err.go 并进来的那一半）。
//
// 两半是同一类东西：各页写动作的结论都要经这里落成响应（提示页或 JSON），
// 所以它们是「共享出口」而不是某个页面的功能 —— 单独一个文件，与业务功能文件分开。

const (
	productListPath         = "/admin/products"
	productEditPath         = "/admin/products/edit"
	productNewPath          = "/admin/products/new"
	productAttrPath         = "/admin/product-attributes"
	productBundlePath       = "/admin/products/bundle"
	productTagsPath         = "/admin/product-tags"
	productCategoriesPath   = "/admin/product-categories"
	productBrandsPath       = "/admin/product-brands"
	productPricingPath      = "/admin/product-pricing"
	productTranslationsPath = "/admin/products/translations"
)

// 各页回跳筛选键。
//
// 同一份键表服务两条路径：**渲染时**拼进表单 action 的 query、**POST 回来时**由
// shell.BackPath 读回。两处分叉的表现是「写完跳回去筛选静默丢了」—— 页面不报错、
// 日志也干净，所以键表必须是同一份（不要在两处各写一遍字面量）。
var (
	productListBackKeys         = []string{"project", "keyword", "status", "page", "limit"}
	productAttrBackKeys         = []string{"project", "keyword", "variation", "page", "limit"}
	productTagsBackKeys         = []string{"project", "keyword", "page", "limit"}
	productCategoriesBackKeys   = []string{"project", "keyword", "page", "limit"}
	productBrandsBackKeys       = []string{"project", "keyword", "page", "limit"}
	productBundleBackKeys       = []string{"project", "product"}
	productPricingBackKeys      = []string{"project"}
	productTranslationsBackKeys = []string{"project", "product", "lang"}
)

// productQueryFromRequest 本次请求 query 里的页面上下文（表单 action 上带回来的那一段）。
//
// 同一份实现同时服务「渲染时拼进表单 action」与「失败重渲片段时再拼一次」：
// 两者产出的形状必须一致，否则会出现「首屏表单 action 带 A、失败重渲后带 B」。
// 空值丢弃、编码走 url.Values（键有序、产物稳定）。
func productQueryFromRequest(c *gin.Context, keys ...string) string {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return ""
	}
	in := c.Request.URL.Query()
	out := url.Values{}
	for _, k := range keys {
		if v := strings.TrimSpace(in.Get(k)); v != "" {
			out.Set(k, v)
		}
	}
	return out.Encode()
}

// withListQuery 把页面上下文拼到写动作地址后面（空上下文则原样返回）。
func withListQuery(action, listQuery string) string {
	if strings.TrimSpace(listQuery) == "" {
		return action
	}
	if strings.Contains(action, "?") {
		return action + "&" + listQuery
	}
	return action + "?" + listQuery
}

// —— 回跳地址（每页一个，键表与上面同一份）——

func productListBack(c *gin.Context) string {
	back := shell.BackPath(c, productListPath, productListBackKeys...)
	if strings.Contains(back, "?") || c == nil {
		return back
	}
	// 列表写表单的工程在隐藏域 projectId 里。action 的 query 没带筛选时
	// （测试直打端点、或表单没拼上 ListQuery）仍要回到这个工程，
	// 否则提示页的返回链接落到不带工程的列表。
	if project := strings.TrimSpace(c.PostForm("projectId")); project != "" {
		return backpath.WithParams(productListPath, map[string]string{"project": project})
	}
	return back
}

func productAttrBack(c *gin.Context) string {
	return shell.BackPath(c, productAttrPath, productAttrBackKeys...)
}

func productTagsBack(c *gin.Context) string {
	return shell.BackPath(c, productTagsPath, productTagsBackKeys...)
}

func productCategoriesBack(c *gin.Context) string {
	return shell.BackPath(c, productCategoriesPath, productCategoriesBackKeys...)
}

func productBrandsBack(c *gin.Context) string {
	return shell.BackPath(c, productBrandsPath, productBrandsBackKeys...)
}

func productBundleBack(c *gin.Context) string {
	return shell.BackPath(c, productBundlePath, productBundleBackKeys...)
}

// productDetailTemplateBack 回详情页模板页：project / product 随表单提交（隐藏域），
// 由服务端用 backpath.WithParams 拼 —— 与 BackPath 共用同一份参数编码。
func productDetailTemplateBack(projectID, productID string) string {
	return backpath.WithParams(productDetailTemplatePath, map[string]string{
		"project": strings.TrimSpace(projectID),
		"product": strings.TrimSpace(productID),
	})
}

func productPricingBack(c *gin.Context) string {
	return shell.BackPath(c, productPricingPath, productPricingBackKeys...)
}

func productTranslationsBack(c *gin.Context) string {
	return shell.BackPath(c, productTranslationsPath, productTranslationsBackKeys...)
}

// productEditBack 回商品编辑页：project / product 本来就随表单提交（隐藏域），
// 由服务端用 backpath.WithParams 拼 —— 与 BackPath 共用同一份参数编码。
func productEditBack(projectID, productID string) string {
	return backpath.WithParams(productEditPath, map[string]string{
		"project": strings.TrimSpace(projectID),
		"product": strings.TrimSpace(productID),
	})
}

// productNewBack 回商品新建页：只有工程上下文。
func productNewBack(projectID string) string {
	return backpath.WithParams(productNewPath, map[string]string{"project": strings.TrimSpace(projectID)})
}

// —— 回跳链接文字（复用各页标题词条，不新增全站词条）——
//
// 全是静态的 key + 中文原文，所以是包级变量：**不需要 gin.Context** ——
// 「这段文案属于哪个语言」由提示页模板取词时决定，与控制器无关（这正是本批的形状要求）。
var (
	productListBackMsg           = productMsg{Key: MsgProductsTitle, Fallback: "商品"}
	productAttrBackMsg           = productMsg{Key: productenums.ProductAttributesTitle, Fallback: "商品属性"}
	productTagsBackMsg           = productMsg{Key: productenums.ProductTagsTitle, Fallback: "商品标签"}
	productCategoriesBackMsg     = productMsg{Key: productenums.ProductCategoriesTitle, Fallback: "商品分类"}
	productBrandsBackMsg         = productMsg{Key: productenums.ProductBrandsTitle, Fallback: "商品品牌"}
	productBundleBackMsg         = productMsg{Key: productenums.ProductBundleTitle, Fallback: "捆绑配置"}
	productDetailTemplateBackMsg = productMsg{Key: productenums.ProductDetailTemplateTitle, Fallback: "商品详情页模板"}
	productPricingBackMsg        = productMsg{Key: productenums.ProductPricingTitle, Fallback: "定价工具"}
	productTranslationsBackMsg   = productMsg{Key: productenums.ProductTranslationsTitle, Fallback: "商品多语言"}
	productEditBackMsg           = productMsg{Key: productenums.ProductsEditTitle, Fallback: "编辑商品"}
	productNewBackMsg            = productMsg{Key: productenums.ProductsCreateSubmit, Fallback: "新建商品"}
)

// —— 提示页的一句结论 ——

// productMsg 提示页的一句结论 —— 就是共享形状 adminui.Msg（key + 中文兜底 + `%s` 参数）。
//
// 这里用别名而不是新造 struct：各模块各造一份同形类型就是多份契约，字段名与参数编码
// 迟早分叉，而症状是「某个页面的提示偶尔少一个数字」，既不报错也不记日志。
type productMsg = adminui.Msg

// productMsgText 把一句结论取成当前语言文本（片段槽位 / JSON 出口没有模板取词时用）。
func productMsgText(c *gin.Context, m productMsg) string {
	return adminui.MsgText(c, m)
}

// —— 提示页出口 ——

// productPageJump 页面写动作的统一出口：整页提示（结论是**事实**，取词在模板里）。
//
// 成功 1 秒后自动跳（Seconds=1）；失败不自动跳（Seconds=0，运营必须看清原因）。
func productPageJump(c *gin.Context, ok bool, msg productMsg, back string, backText productMsg) {
	seconds := 0
	if ok {
		seconds = 1
	}
	shell.RenderJump(c, shell.Jump{
		OK:          ok,
		MsgKey:      msg.Key,
		Msg:         msg.Fallback,
		MsgArgs:     msg.Args,
		Back:        back,
		BackTextKey: backText.Key,
		BackText:    backText.Fallback,
		Seconds:     seconds,
	})
}

// productListJump 回商品列表页的提示页出口（删除 / 批量删除 / 批量改价共用）。
func productListJump(c *gin.Context, ok bool, msg productMsg) {
	productPageJump(c, ok, msg, productListBack(c), productListBackMsg)
}

// productAttrJump 回属性页的提示页出口。
func productAttrJump(c *gin.Context, ok bool, msg productMsg) {
	productPageJump(c, ok, msg, productAttrBack(c), productAttrBackMsg)
}

// productTagsJump 回标签页的提示页出口。
func productTagsJump(c *gin.Context, ok bool, msg productMsg) {
	productPageJump(c, ok, msg, productTagsBack(c), productTagsBackMsg)
}

// productBrandsJump 回品牌页的提示页出口。
func productBrandsJump(c *gin.Context, ok bool, msg productMsg) {
	productPageJump(c, ok, msg, productBrandsBack(c), productBrandsBackMsg)
}

// productBundleJump 回捆绑配置页的提示页出口。
//
// 商品 id 随隐藏域 productId 提交，不依赖 action query：htmx 成功要 HX-Redirect 回
// 「这一个商品」的捆绑页，query 上没有 product 时不能落到空骨架。
func productBundleJump(c *gin.Context, ok bool, msg productMsg) {
	back := productBundleBack(c)
	if !strings.Contains(back, "product=") {
		project := ""
		product := ""
		if c != nil {
			project = strings.TrimSpace(c.PostForm("projectId"))
			product = strings.TrimSpace(c.PostForm("productId"))
		}
		back = backpath.WithParams(productBundlePath, map[string]string{
			"project": project,
			"product": product,
		})
	}
	productPageJump(c, ok, msg, back, productBundleBackMsg)
}

// productDetailTemplateJump 回详情页模板页的提示页出口（project / product 来自表单）。
func productDetailTemplateJump(c *gin.Context, ok bool, projectID, productID string, msg productMsg) {
	productPageJump(c, ok, msg, productDetailTemplateBack(projectID, productID), productDetailTemplateBackMsg)
}

// productPricingJump 回定价工具页的提示页出口。
func productPricingJump(c *gin.Context, ok bool, msg productMsg) {
	productPageJump(c, ok, msg, productPricingBack(c), productPricingBackMsg)
}

// productTranslationsJump 回翻译工作台的提示页出口。
func productTranslationsJump(c *gin.Context, ok bool, msg productMsg) {
	productPageJump(c, ok, msg, productTranslationsBack(c), productTranslationsBackMsg)
}

// productEditJump 回商品编辑页的提示页出口（商品级写动作的**唯一**落点）。
func productEditJump(c *gin.Context, ok bool, projectID, productID string, msg productMsg) {
	productPageJump(c, ok, msg, productEditBack(projectID, productID), productEditBackMsg)
}

// productNewJump 回商品新建页的提示页出口（新建表单失败时的唯一落点）。
func productNewJump(c *gin.Context, ok bool, projectID string, msg productMsg) {
	productPageJump(c, ok, msg, productNewBack(projectID), productNewBackMsg)
}

// —— 成功回执文案（key + 中文兜底；词条缺失时页面上仍是中文）——

// productActionDoneFallback 单条写动作的通用成功回执的中文兜底（key 在
// productenums.ProductsActionDone）。
//
// 不复用 enums 的 MsgCreateSuccess / MsgUpdateSuccess：那几个 key 的值在词条表里是
// 「Blueprint 创建成功」这类**蓝图域**的句子（058/430 早先 seed），商品页用它会显示成
// 「Blueprint 创建成功」。这里另起一条商品域 key（登记前走中文兜底）。
const productActionDoneFallback = "操作已完成"

// productActionDoneMsg 单条写动作（新建 / 修改 / 保存）的成功回执事实。
var productActionDoneMsg = productMsg{Key: productenums.ProductsActionDone, Fallback: productActionDoneFallback}

// productOutcomeMsg 把 enums 的结论（productenums.Outcome）转成提示页要的**事实**。
//
// 控制器只做这一次转换：选句与参数装配都在 enums（internal/module/CLAUDE.md §enums）。
func productOutcomeMsg(o productenums.Outcome) productMsg {
	m := productMsg{Key: o.Key, Fallback: o.Fallback}
	if len(o.Args) > 0 {
		m = m.WithArgs(o.Args...)
	}
	return m
}

// productBulkSentence 把控制器侧的结论文案（productBulkText）转成 enums 的句子载体。
//
// 各实体自己的那两句（「已删除 %s 个」/「…%s 个未能删除」）仍住在调用方附近 —— 它们是
// 该实体的措辞；enums 只负责从这两句里挑一句并填计数。
func productBulkSentence(t productBulkText) productenums.Outcome {
	return productenums.Outcome{Key: t.key, Fallback: t.fallback}
}

// productBulkDoneMsg 单条删除的成功回执：复用该实体的批量结论文案（计数 1）。
//
// 「删一个」与「批量删一个」说的是同一句话 —— 两处各写一句的下场是同一个动作在
// 两种入口下措辞不同（与 block 模块 allDeleted 的取舍一致）。
func productBulkDoneMsg(tpl productBulkText) productMsg {
	return tpl.fact("1")
}

// productBulkDeleteResult 批量删除的结论（成功 N / 跳过 M）→ 提示页的 ok 与**事实**。
//
// 逐条失败不中断整批的语义由调用方保证（已算好 deleted / skipped）；选句与计数装配在
// productenums.BulkDeleteOutcome（控制器不再按计数挑文案）。
func productBulkDeleteResult(deleted, skipped int, partial, done productBulkText) (ok bool, msg productMsg) {
	ok, out := productenums.BulkDeleteOutcome(deleted, skipped, productBulkSentence(partial), productBulkSentence(done))
	return ok, productOutcomeMsg(out)
}

// ==========================================================================
// 商品后台页**写侧**回执文案与**整页 GET 失败出口**的收口。
//
// 传输通道已改为 shell.RenderJump 渲染整页提示（见 product_jump.go）：写动作的结论走
// 响应体，**不再**经 302/303 + `?err=` / `?done=` / `?applied=` / `?saved=` 回带列表页。
//
// **读侧（?err= / ?done= 的受控文案集合与判定）已整批删除**：那条通道的代价是本模块要维护
// 一份「受控文案 + 数字归一模板」来证明提示出自本仓（productPageErr / productPageDone /
// productNoticeTexts / productFacingNotice / productVariantNoticeMatches / productSkipListMatches /
// productReasonTexts / productOwnPageTexts / productAppliedToken / productBulkResultTemplates
// 就是那套），而查询参数不是可信边界（任何人手拼一个 /admin/products?err=任意文案 就能往
// 页面上塞一条伪造消息）。文案走响应体之后，那套判定随之不需要了。
//
// 本文件剩下的都是**写侧**与**整页 GET 的失败出口**：把 service 的错误 / 计数结论渲染成
// 「一句结论的事实」（key + 中文兜底 + `%s` 参数）或可展示的成品文案。
//
// **错误判定只有一条**：pkg/response.IsBusinessError（`a.b.c` key 或 `Err*` / `Msg*` 常量名），
// 与 JSON 出口（response.ErrorAuto）同一份。原先本模块自带的第二份真相 ——
// `productErrSentinels` 白名单 + `productErrKey` 逐条比对常量 —— 已整个删除：
// service 换了措辞、或某个 key 改由别的模块提供（如 inventory / presentation / contenttemplate
// 的 enums），白名单就静默失配，症状是把业务文案吞成归口、或把内部错误当业务文案透出，
// 两种都不报错。判定收口到 productErrFact 一处（见 product_page.go）。
//
// 模块**保留**「key → 中文兜底」的表（productenums.ProductErrFallbacks 是本模块那半张、
// 不是可透出性判据（与 page 模块的 pageenums.ErrDetailFallbacks 同一性质）。
// ==========================================================================
type productBulkText struct{ key, fallback string }

// fact 把一条结论文案变成**事实**（key + 中文兜底 + `%s` 参数），提示页在渲染期取词。
//
// 参数一律 strconv 成字符串（模板侧的占位符是 `%s`，传 int 会渲染成 `%!s(int=3)`）；
// 没有占位符的词条（如 productSaved）**不要**传参数 —— TranslateArgs 对多余参数会 Sprintf 出
// `%!(EXTRA string=…)`。
func (t productBulkText) fact(args ...string) productMsg {
	m := productMsg{Key: t.key, Fallback: t.fallback}
	if len(args) > 0 {
		m = m.WithArgs(args...)
	}
	return m
}

// productBulkTextOf 取一条批量结论文案的当前语言模板（**文本**形态，给「多段拼一句」的合成句用）。
//
// 词条混进 %d 之类协议外占位符时回落中文原文（模板一律只允许 %s，数字先经 strconv.Itoa）——
// 否则 Sprintf 会把参数渲染成 int，而这条路直接给运营看（与 shell.BulkIDsFacingText 同理）。
func productBulkTextOf(c *gin.Context, t productBulkText) string {
	text := shell.TranslateFor(c)(t.key, t.fallback)
	if !i18n.HasStringPlaceholdersOnly(text) {
		return t.fallback
	}
	return text
}

// 批量删除：五个实体各一对（部分成功 / 全部成功）。%s 是计数（Go 侧 strconv.Itoa 后填入）。
var (
	productTagBulkPartial   = productBulkText{productenums.BulkTagPartial, "已删除 %s 个，%s 个未能删除（标签不存在或已被删除）"}
	productTagBulkDone      = productBulkText{productenums.BulkTagDone, "已删除 %s 个标签"}
	productAttrBulkPartial  = productBulkText{productenums.BulkAttrPartial, "已删除 %s 个，%s 个未能删除（属性组不存在或被商品引用）"}
	productAttrBulkDone     = productBulkText{productenums.BulkAttrDone, "已删除 %s 个属性组（连同其全部属性值）"}
	productBrandBulkPartial = productBulkText{productenums.BulkBrandPartial, "已删除 %s 个，%s 个未能删除（品牌不存在或被商品引用）"}
	productBrandBulkDone    = productBulkText{productenums.BulkBrandDone, "已删除 %s 个品牌"}
	productBulkPartial      = productBulkText{productenums.BulkProductPartial, "已删除 %s 个，%s 个未能删除（商品不存在或被其它数据引用）"}
	productBulkDone         = productBulkText{productenums.BulkProductDone, "已删除 %s 个商品（连同其全部变体）"}
)

// —— 列表页 / 详情页的自造回执文案 ——

// productQuantityInvalidMsg 新建商品时数量字段的校验文案（**事实**：key + 中文兜底）。
//
// key + 中文兜底：取词发生在提示页模板里；词条缺失时页面上仍是中文。
var productQuantityInvalidMsg = productMsg{
	Key:      productenums.ProductsCreateQuantityInvalid,
	Fallback: "数量必须是非负整数",
}

// 批量改价的结论文案模板（%s 是计数或跳过原因）。
var (
	productPricingNoChange = productBulkText{productenums.BulkPricingNoChange, "按该规则算下来没有价格变化：%s 个商品已是目标价，未写入调价记录。"}
	productPricingApplied  = productBulkText{productenums.BulkPricingApplied, "已按规则改价：共改 %s 个变体（%s 个商品已是目标价）。"}
	productPricingAllSkip  = productBulkText{productenums.BulkPricingAllSkip, "没有可改价的变体：%s 个商品被跳过（%s）。"}
	productPricingPartial  = productBulkText{productenums.BulkPricingPartial, "已改 %s 个变体，另有 %s 个商品被跳过（%s）。"}
)

// 变体清单保存的结论文案（详情页提示页）。
var (
	productVariantSaveNoChange = productBulkText{productenums.BulkVariantNoChange, "变体清单与库里一致，没有需要保存的变化。"}
	productVariantSaveSaved    = productBulkText{productenums.BulkVariantSaved, "已保存变体清单：新增 %s 个、修改 SKU %s 个、删除 %s 个。"}
	productVariantSaveSkipped  = productBulkText{productenums.BulkVariantSkipped, "跳过 %s 个：%s"}
)

// productSaved 商品编辑页保存成功的回执（无占位符）。
//
// 保存后页面刷新，字段值与保存前看起来一模一样 —— 没有这条提示，用户无法区分
// 「保存成功了」与「按钮点了没反应」。
var productSaved = productBulkText{productenums.MsgProductSaved, "商品已保存"}

// —— 批量 id 超限的受控事实 ——

// productBulkIDsMsg 批量 id 超限的受控**事实**（**类型**判定，不比对文案）。
//
// 此前 product 侧按当前语言拼前缀再拿 err.Error() 比对（productErrControlledMessages）：
// 上游改措辞或换语言即静默失配，超限提示退化成归口文案，运营看不到「请分批进行」。
// 判据改为类型事实（*bulkids.IDsError 携 Max / Count），文案走共享词条。
func productBulkIDsMsg(c *gin.Context, err error) productMsg {
	var tooMany *bulkids.IDsError
	if !errors.As(err, &tooMany) {
		return adminui.InternalMsg()
	}
	return productMsg{Key: bulkids.MsgTooMany, Fallback: bulkids.TooManyTemplate}.
		WithArgs(strconv.Itoa(tooMany.Max), strconv.Itoa(tooMany.Count))
}

// —— 整页 GET 的失败出口：文案归口 ——
//
// 契约：docs/rules/template-boundary.md §整页 GET 的失败出口 —— 取数失败的整页 GET
// **也必须是「这一页」**（同一份模板 + 页壳 + 侧边菜单 + `Cache-Control: no-store`），
// 状态码按档位说实话（主数据没读到 = 500；请求参数不合法 = 400；资源不存在 = 404）。
//
// 此前本模块三个整页 GET（/admin/products、/admin/products/new、/admin/products/detail）
// 的取数失败一律走 shell.PageError(c, "product"|"products_new", err)：浏览器收到的是
// `{"code":500,…}` —— DOM 里没有标题栏与左侧菜单，地址栏停在原路径，用户只能手改地址栏回去。
//
// **本文件只做取词，不做出口**：渲染留在各 handler 末尾那唯一一次 c.HTML 里
//（失败页与成功页共用同一份模板与同一份页数据，差别只有状态码 / no-store / 失败槽）。
// 这里若再放一个「渲染失败页」的函数，模块就又多了一条出口形态 —— 那正是要收敛的东西。

// productErrUnregisteredFallback 「有业务形态、但本仓没登记展示原文」时的兜底（task-158 §7.3 ⑥）。
//
// 两个「不能」：页面不能显示空串（错误槽空着，用户以为没出错），也不能显示裸 key
// （ErrSomethingNew 这种串运营只会以为后台坏了）。也不能借共享归口文案说「系统内部错误」
// —— 那是**非业务**错误的口径，把一次业务拒绝说成系统故障同样误导。
//
// 它本该永不出现：productenums.ProductErrFallbacks 的覆盖面由 AST 守卫钉住，
// 未登记的 key 只可能来自「新加常量又没跑测试」。出现时记一条 warn（带 err_key），
// 所以「多了一句稳定文案、少了一句该有的话」在日志里看得见。
const productErrUnregisteredFallback = "操作失败，请稍后重试（细节只进日志）"

// productErrPageText 页面出口的成品文案（片段槽位 / 整页失败槽用）。
//
// 判定与日志都在 productErrMsg（见 product_page.go 的 productErrFact）：命中业务错误取
// 当前语言（缺词条走 productErrFallbackText 查到的中文兜底）；未命中记一条日志并给归口文案。
func productErrPageText(c *gin.Context, err error) string {
	return productMsgText(c, productErrMsg(c, err))
}

// productPageScene 页面装载失败的日志场景名（与 shell.PageError 的 page 场景同源，便于检索）。
const productPageScene = "page"

// productErrKeyOf err → 业务错误 key（非业务错误 / nil 一律空串）。
//
// 页面出口用它分流「不存在」(404) 与「不合法」(400)：两者都是**业务事实**，
// 由 service 的 enums 常量判定，不靠匹配驱动报错原文（那会随 PG 版本改字面量）。
func productErrKeyOf(err error) string {
	key, _, _ := productErrFactOf(err)
	return key
}

// productErrForeignFallbacks 别模块业务 key 的展示兜底（key 的归属不在本模块，表暂由本层带着）。
//
// 为什么留在这层而不是搬进各自的 enums：那两个模块**还没有**通用兜底表
// （internal/module/inventory/enums/inventory_enums.go 只有 ErrDetailFallbacks，
// presentation 同理）。写进 product 的 enums 更糟 —— key 的归属一旦跨模块，
// 对方改名就没有编译错误兜着。所以本表只装「展示原文」，且**只减不增**：
// 对方模块补上 ErrFallbacks 后就把对应条目删掉。
var productErrForeignFallbacks = map[string]string{
	inventoryenums.ErrSKUSourceInvalid:             "SKU 来源取值不合法：只支持「自己创建」与「从仓库选」",
	inventoryenums.ErrWarehouseSKURequired:         "从仓库选 SKU 时必须先选仓库并指明该仓的那条仓库 SKU",
	inventoryenums.ErrWarehouseSKUNotFound:         "该仓库里没有这条仓库 SKU：请确认仓库选对了，或先在该仓建好这条货的库存记录",
	inventoryenums.ErrWarehouseSKUBundleNotAllowed: "捆绑商品不存在于仓库，不能用「从仓库选」建主体 SKU，请改用自己的编码",
	inventoryenums.ErrWarehouseSKUCodeTaken:        "该仓已有同一条 SKU 编码的库存行（我们自己的 SKU 在仓内唯一），请改用另一个仓库或另一个仓库 SKU",
	inventoryenums.ErrExternalSKUInvalid:           "外部编码不合法：长度需在 128 个字符以内且不含控制字符",
	inventoryenums.ErrExternalSKUProductConflict:   "该外部编码在本仓已挂在另一个商品上：同一个外部编码在同一仓库内只能属于同一个商品（同一商品的多个口味可以共用一个外部编码）",
	inventoryenums.ErrWarehouseNotFound:            "仓库不存在",
	inventoryenums.ErrWarehouseDisabled:            "已停用的仓库不能作为归属仓",
	inventoryenums.ErrWarehouseProjectMismatch:     "仓库不属于该工程",
	inventoryenums.ErrWarehouseDefaultMissing:      "该工程没有默认仓可兜底，请先建一个仓库并设为默认仓",
	presentationenums.ErrDetachConfirmRequired:     "该操作会放弃模板同步（商品页转为独立文档），需要先确认",
	presentationenums.ErrRollbackTargetMiss:        "回滚目标不存在：该版本不属于这个商品，或产物文件已缺失",
	presentationenums.ErrRollbackFailed:            "回滚失败：线上版本保持不变",
	presentationenums.ErrSnapshotMismatch:          "该版本不属于这个商品，不能用来回滚",
}

// productErrFallbackText key → 已登记的中文兜底（未登记返回空串）。
//
// 两段查：先本模块 enums 的表（key 的归属与文案真源都在那儿，见 productenums.ProductErrFallbacks），
// 再跨模块那张。查表顺序就是优先级，两张表不允许有交集
// （product_err_fallback_tables_test.go 守着）。
func productErrFallbackText(key string) string {
	if key == "" {
		return ""
	}
	if fb := productenums.ProductErrFallbacks[key]; fb != "" {
		return fb
	}
	return productErrForeignFallbacks[key]
}

// productErrFactOf 业务错误的判定与拆分（**纯事实**，不记日志）。
//
// 判据只有一条：pkg/response.IsBusinessError（与 JSON 出口 response.ErrorAuto 同一份）。
// 两种入参形态：
//   - 单段：`ErrBundlePriceRequired` —— 整体就是 key；
//   - 复合：service 用 fmt.Errorf("%s：%s", key, detail) 把「枚举 + 明细」拼成一整句，
//     整体过不了锚定的形态判据，所以取**前段**判定。注意本模块用的是**全角冒号**：
//     adminui.BusinessErrFact 只按 ASCII 冒号切，这里补一刀（判据仍是共享那一份）。
//
// 明细段（tail）原样带出，由 productErrDetailText 解码成当前语言。
func productErrFactOf(err error) (key, tail string, business bool) {
	if err == nil {
		return "", "", false
	}
	if f, ok := adminui.BusinessErrFact(err); ok {
		return f.Key, f.Tail, true
	}
	raw := strings.TrimSpace(err.Error())
	if idx := strings.Index(raw, "："); idx > 0 {
		k := strings.TrimSpace(raw[:idx])
		t := strings.TrimSpace(raw[idx+len("："):])
		if k != "" && response.IsBusinessError(errors.New(k)) {
			return k, t, true
		}
	}
	return "", "", false
}

// productErrFact 判定 + 归口（**记日志**）：给「调用点没有更具体的日志」的地方用。
func productErrFact(c *gin.Context, err error) adminui.ErrFact {
	return productErrFactWith(c, err, true)
}

// productErrFactQuiet 同 productErrFact，但**不记日志** —— 给「调用点已经记过一条更具体的日志
// （带 productId / project 等）」的地方用，免得同一个错误在日志里出现两遍。
func productErrFactQuiet(c *gin.Context, err error) adminui.ErrFact {
	return productErrFactWith(c, err, false)
}

func productErrFactWith(c *gin.Context, err error, logIt bool) adminui.ErrFact {
	if err == nil {
		return adminui.ErrFact{}
	}
	if key, tail, business := productErrFactOf(err); business {
		fb := productErrFallbackText(key)
		if fb == "" {
			// 「有业务形态、但没有展示原文」的 key：对外给稳定的一句兜底（绝不显示裸 key），
			// 对内记一条 warn 带上 key —— 否则「页面上少了一句该有的话」没有任何信号。
			if logIt {
				logger.Scene(productErrScene).
					With("path", c.Request.URL.Path).
					With("user_id", builtin.CurrentUserID(c)).
					With("err_key", key).
					Warn("业务错误 key 未登记中文兜底（补进 productenums.ProductErrFallbacks）")
			}
			fb = productErrUnregisteredFallback
		}
		return adminui.ErrFact{Key: key, Fallback: fb, Tail: tail, Business: true}
	}
	if logIt {
		logger.Scene(productErrScene).
			With("path", c.Request.URL.Path).
			With("user_id", builtin.CurrentUserID(c)).
			Error(err, "商品后台页操作失败（非业务错误，只对外给归口文案）")
	}
	return adminui.InternalErrorFact()
}

// productErrMsg 业务错误 → 一句结论事实（判定见 productErrFact）。
func productErrMsg(c *gin.Context, err error) productMsg {
	return productErrMsgOf(c, productErrFact(c, err))
}

// productErrMsgQuiet 同 productErrMsg，但不记日志。
func productErrMsgQuiet(c *gin.Context, err error) productMsg {
	return productErrMsgOf(c, productErrFactQuiet(c, err))
}

// productErrMsgOf 事实 → 一句结论（key + 中文兜底；带明细时给成品串）。
func productErrMsgOf(c *gin.Context, f adminui.ErrFact) productMsg {
	if f.Key == "" {
		return productMsg{}
	}
	fb := productErrFallbackOf(f.Key, f.Fallback)
	if f.Tail != "" {
		if detail := productErrDetailText(shell.TranslateFor(c), f.Tail); detail != "" {
			// 明细是「词条 + 具名参数」的多段结构，一段 key 表达不了它 ——
			// 这一档给成品串（与迁入前逐字一致；明细本身仍随语言变化）。
			return productMsg{Fallback: adminui.TranslateArgs(c, f.Key, fb, nil) + "：" + detail}
		}
	}
	return productMsg{Key: f.Key, Fallback: fb}
}

// productErrFallbackOf key → 中文兜底（缺词条时的展示原文，不是可透出性判据）。
//
// def 是事实自带的兜底（adminui.ErrFact.Fallback）。都取不到时给**归口文案**（共享那一份），
// 不回落到裸 key：页面上出现 ErrBundlePriceRequired 这种串，运营只会以为后台坏了。
func productErrFallbackOf(key, def string) string {
	if fb := productErrFallbackText(key); fb != "" {
		return fb
	}
	if strings.TrimSpace(def) != "" {
		return def
	}
	return adminui.InternalErrorFact().Fallback
}

// productErrScene 日志场景名（与 product 模块其它 logger.Scene("product") 一致）。
const productErrScene = "product"

// productInternalText 未命中业务判据时的统一出口（错误文案三件套的第三件）：
// 原文只进日志（场景 + user_id + 原始错误），对外只给归口文案。
//
// 页面上出现 "pq: duplicate key value violates unique constraint" 既看不懂，
// 也把库表结构泄了出去 —— 响应（含提示页文案与模板数据）不是可信边界。
func productInternalText(c *gin.Context, err error) string {
	if err != nil {
		logger.Scene(productErrScene).
			With("user_id", builtin.CurrentUserID(c)).
			Error(err, "商品后台页操作失败（非业务错误，只对外给归口文案）")
	}
	return adminui.PageInternalText(c)
}

// productErrText 业务错误 → 当前语言文案（**片段槽位 / JSON 出口**用：那边模板不取词）。
//
// 写侧与整页失败出口不再用它 —— 那些地方传**事实**（productErrMsg），取词在渲染期发生。
// 与 block 模块的 blockErrText 同一形状：页面上的错误必须是能读的一句话，
// 而不是 ErrBundlePriceRequired 这样的裸 key，也不是 GORM / PG 的原始报错。
func productErrText(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	return productMsgText(c, productErrMsg(c, err))
}

// productErrDetailText 业务错误的**补充说明** → 当前语言文案。
//
// 补充说明有两种形态：
//
//  1. i18n.ErrorDetail 的产物（控制字符开头的「明细词条 key + 具名参数」，可多段）——
//     service 用它把「为什么被拒」的上下文（哪个属性组 / 超了多少 / 被哪个商品占用）
//     也词条化，于是整句按当前语言取词并填 {name} 占位符；
//  2. 其余（受控提示、历史形态的纯文本）—— 原样透出，行为与本通道引入前一致。
//
// 未登记的明细 key（拼错 / 新加漏登记）跳过该段并记一条日志：少一句补充说明，
// 好过把编码串或半截占位符摆到页面上。
func productErrDetailText(tr func(key, fallback string) string, tail string) string {
	parts, ok := i18n.ParseErrorDetails(tail)
	if !ok {
		return tail
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		fallback, registered := productenums.ErrDetailFallbacks[p.Key]
		if !registered || fallback == "" {
			logger.Scene(productErrScene).With("detail_key", p.Key).
				Warn("业务错误的补充说明词条未登记，已省略该段")
			continue
		}
		out = append(out, i18n.FillTranslate(tr, p.Key, fallback, p.Args))
	}
	return strings.Join(out, "；")
}
