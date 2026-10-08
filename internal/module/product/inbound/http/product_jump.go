package producthttp

// product_jump.go — product 后台页写动作的**出口**：整页提示（shell.RenderJump，
// 对应 ThinkPHP 的 success() / error()）。
//
// 取代原先的 302/303 + `?err=` / `?done=` / `?applied=` / `?saved=` 回列表页 / 编辑页：
// 那条通道要求读侧再判一次「这条提示是不是本仓给的」（productPageErr / productPageDone /
// productNoticeTexts / productVariantNoticeMatches / productOwnPageTexts / productAppliedToken
// 就是那套），而查询参数不是可信边界。文案改走响应体之后，那套判定整批删除
//（见 product_err.go 的文件头）。
//
// 三条边界（同 shell.RenderJump 的注释）：
//   · 文案必须**已过本模块白名单 / 已归口**（productErrText / detailTemplateFacingError /
//     detailTemplateTemplateErrText / shell.BulkIDsFacingText / productBulkTextOf 的产物）——
//     原文只进日志，换个页面呈现不等于可以把 err.Error() 铺上去；
//   · 回跳地址由 shell.BackPath 从**表单 action 的 query** 按白名单读回（服务端自己拼，
//     不读隐藏域里的整串 URL）；编辑页 / 新建页的 id 本来就随表单提交，用 shell.WithParams
//     拼（与 content 模块 articleJumpEditBack 同一取舍）；
//   · 结论不进 URL —— 成功 / 失败只体现在提示页的响应体里。

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	productenums "go_wp/internal/module/product/enums"
	"go_wp/internal/shell"
)

// 后台页面路径（回跳目标）。productDetailTemplatePath 在 product_page.go 里
// （与列表行内入口 / 菜单 path 三处一致），这里不重复。
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
		return shell.WithParams(productListPath, map[string]string{"project": project})
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
// 由服务端用 shell.WithParams 拼 —— 与 BackPath 共用同一份参数编码。
func productDetailTemplateBack(projectID, productID string) string {
	return shell.WithParams(productDetailTemplatePath, map[string]string{
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
// 由服务端用 shell.WithParams 拼 —— 与 BackPath 共用同一份参数编码。
func productEditBack(projectID, productID string) string {
	return shell.WithParams(productEditPath, map[string]string{
		"project": strings.TrimSpace(projectID),
		"product": strings.TrimSpace(productID),
	})
}

// productNewBack 回商品新建页：只有工程上下文。
func productNewBack(projectID string) string {
	return shell.WithParams(productNewPath, map[string]string{"project": strings.TrimSpace(projectID)})
}

// —— 回跳链接文字（复用各页标题词条，不新增全站词条）——

func productListBackText(c *gin.Context) string {
	return shell.TranslateFor(c)(MsgProductsTitle, "商品")
}

func productAttrBackText(c *gin.Context) string {
	return shell.TranslateFor(c)(productenums.ProductAttributesTitle, "商品属性")
}

func productTagsBackText(c *gin.Context) string {
	return shell.TranslateFor(c)(productenums.ProductTagsTitle, "商品标签")
}

func productCategoriesBackText(c *gin.Context) string {
	return shell.TranslateFor(c)(productenums.ProductCategoriesTitle, "商品分类")
}

func productBrandsBackText(c *gin.Context) string {
	return shell.TranslateFor(c)(productenums.ProductBrandsTitle, "商品品牌")
}

func productBundleBackText(c *gin.Context) string {
	return shell.TranslateFor(c)(productenums.ProductBundleTitle, "捆绑配置")
}

func productDetailTemplateBackText(c *gin.Context) string {
	return shell.TranslateFor(c)(productenums.ProductDetailTemplateTitle, "商品详情页模板")
}

func productPricingBackText(c *gin.Context) string {
	return shell.TranslateFor(c)(productenums.ProductPricingTitle, "定价工具")
}

func productTranslationsBackText(c *gin.Context) string {
	return shell.TranslateFor(c)(productenums.ProductTranslationsTitle, "商品多语言")
}

func productEditBackText(c *gin.Context) string {
	return shell.TranslateFor(c)("admin.products.edit.title", "编辑商品")
}

func productNewBackText(c *gin.Context) string {
	return shell.TranslateFor(c)(productenums.ProductsCreateSubmit, "新建商品")
}

// —— 提示页出口 ——

// productPageJump 页面写动作的统一出口：整页提示。
//
// 成功 1 秒后自动跳（Seconds=1）；失败不自动跳（Seconds=0，运营必须看清原因）。
func productPageJump(c *gin.Context, ok bool, msg, back, backText string) {
	if ok {
		shell.RenderJump(c, shell.Jump{OK: true, Msg: msg, Back: back, BackText: backText, Seconds: 1})
		return
	}
	shell.RenderJump(c, shell.Jump{Msg: msg, Back: back, BackText: backText})
}

// productListJump 回商品列表页的提示页出口（删除 / 批量删除 / 批量改价共用）。
func productListJump(c *gin.Context, ok bool, msg string) {
	productPageJump(c, ok, msg, productListBack(c), productListBackText(c))
}

// productAttrJump 回属性页的提示页出口。
func productAttrJump(c *gin.Context, ok bool, msg string) {
	productPageJump(c, ok, msg, productAttrBack(c), productAttrBackText(c))
}

// productTagsJump 回标签页的提示页出口。
func productTagsJump(c *gin.Context, ok bool, msg string) {
	productPageJump(c, ok, msg, productTagsBack(c), productTagsBackText(c))
}

// productCategoriesJump 回分类页的提示页出口。
func productCategoriesJump(c *gin.Context, ok bool, msg string) {
	productPageJump(c, ok, msg, productCategoriesBack(c), productCategoriesBackText(c))
}

// productBrandsJump 回品牌页的提示页出口。
func productBrandsJump(c *gin.Context, ok bool, msg string) {
	productPageJump(c, ok, msg, productBrandsBack(c), productBrandsBackText(c))
}

// productBundleJump 回捆绑配置页的提示页出口。
//
// 商品 id 随隐藏域 productId 提交，不依赖 action query：htmx 成功要 HX-Redirect 回
// 「这一个商品」的捆绑页，query 上没有 product 时不能落到空骨架。
func productBundleJump(c *gin.Context, ok bool, msg string) {
	back := productBundleBack(c)
	if !strings.Contains(back, "product=") {
		project := ""
		product := ""
		if c != nil {
			project = strings.TrimSpace(c.PostForm("projectId"))
			product = strings.TrimSpace(c.PostForm("productId"))
		}
		back = shell.WithParams(productBundlePath, map[string]string{
			"project": project,
			"product": product,
		})
	}
	productPageJump(c, ok, msg, back, productBundleBackText(c))
}

// productDetailTemplateJump 回详情页模板页的提示页出口（project / product 来自表单）。
func productDetailTemplateJump(c *gin.Context, ok bool, projectID, productID, msg string) {
	productPageJump(c, ok, msg, productDetailTemplateBack(projectID, productID), productDetailTemplateBackText(c))
}

// productPricingJump 回定价工具页的提示页出口。
func productPricingJump(c *gin.Context, ok bool, msg string) {
	productPageJump(c, ok, msg, productPricingBack(c), productPricingBackText(c))
}

// productTranslationsJump 回翻译工作台的提示页出口。
func productTranslationsJump(c *gin.Context, ok bool, msg string) {
	productPageJump(c, ok, msg, productTranslationsBack(c), productTranslationsBackText(c))
}

// productEditJump 回商品编辑页的提示页出口（商品级写动作的**唯一**落点）。
func productEditJump(c *gin.Context, ok bool, projectID, productID, msg string) {
	productPageJump(c, ok, msg, productEditBack(projectID, productID), productEditBackText(c))
}

// productNewJump 回商品新建页的提示页出口（新建表单失败时的唯一落点）。
func productNewJump(c *gin.Context, ok bool, projectID, msg string) {
	productPageJump(c, ok, msg, productNewBack(projectID), productNewBackText(c))
}

// —— 成功回执文案（key + 中文兜底；词条缺失时页面上仍是中文）——

// productActionDoneKey / productActionDoneFallback 单条写动作的通用成功回执。
//
// 不复用 enums 的 MsgCreateSuccess / MsgUpdateSuccess：那几个 key 的值在词条表里是
// 「Blueprint 创建成功」这类**蓝图域**的句子（058/430 早先 seed），商品页用它会显示成
// 「Blueprint 创建成功」。这里另起一条商品域 key（登记前走中文兜底）。
const (
	productActionDoneKey      = "admin.products.actionDone"
	productActionDoneFallback = "操作已完成"
)

// productActionDoneText 单条写动作（新建 / 修改 / 保存）的成功回执。
func productActionDoneText(c *gin.Context) string {
	return shell.TranslateFor(c)(productActionDoneKey, productActionDoneFallback)
}

// productBulkNoneSelectedKey / productBulkNoneSelectedFallback 批量动作一个都没勾选时的提示。
//
// 原先「没勾选」是静默跳回列表（不报错也不说明），用户会以为「点了没反应」。
// 与批量改价的 bulkPricingNothingSelected 同一口径（说清怎么继续）。
const (
	productBulkNoneSelectedKey      = "admin.products.bulk.noneSelected"
	productBulkNoneSelectedFallback = "没有选中任何项，列表未改动。"
)

// productBulkNoneSelectedText 上面那条提示的当前语言文本。
func productBulkNoneSelectedText(c *gin.Context) string {
	return shell.TranslateFor(c)(productBulkNoneSelectedKey, productBulkNoneSelectedFallback)
}

// productBulkDoneText 单条删除的成功回执：复用该实体的批量结论文案（计数 1）。
//
// 「删一个」与「批量删一个」说的是同一句话 —— 两处各写一句的下场是同一个动作在
// 两种入口下措辞不同（与 block 模块 allDeleted 的取舍一致）。
func productBulkDoneText(c *gin.Context, tpl productBulkText) string {
	return fmt.Sprintf(productBulkTextOf(c, tpl), "1")
}

// productBulkDeleteResult 批量删除的结论（成功 N / 跳过 M）→ 提示页的 ok 与正文。
//
// 逐条失败不中断整批的语义由调用方保证（已算好 deleted / skipped）；这里只把计数
// 翻成当前语言的整句。一个都没勾选时给「没有选中任何项」而不是「已删除 0 个」——
// 后者读起来像成功，实际什么都没做。
func productBulkDeleteResult(c *gin.Context, deleted, skipped int, partial, done productBulkText) (ok bool, msg string) {
	switch {
	case skipped > 0:
		return false, fmt.Sprintf(productBulkTextOf(c, partial), strconv.Itoa(deleted), strconv.Itoa(skipped))
	case deleted > 0:
		return true, fmt.Sprintf(productBulkTextOf(c, done), strconv.Itoa(deleted))
	default:
		return false, productBulkNoneSelectedText(c)
	}
}
