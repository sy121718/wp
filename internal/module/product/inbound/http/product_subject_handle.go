package producthttp

// product_subject_handle.go — 商品主体：接口面 CRUD / 变体 / 评分、后台列表与编辑页、定价。
//
// 为什么这两块在同一个文件：定价是商品价格字段上的操作，不是独立域。按子功能切文件的结果是
// 「改一个商品的写入要在两个文件里找落点」，而路由表本身只按业务域分节（见 product_router.go）。

import (
	"go_wp/pkg/binding"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	presentationcontract "go_wp/internal/module/presentation/contract"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
)

// productErrScene 日志场景名（与 product 模块其它 logger.Scene("product") 一致）。
const productErrScene = "product"

// product_subject_handle.go — 商品本体的控制器：接口面 CRUD / 变体 + 列表 / 新建 / 编辑 / 详情页 / 批量。
//
// 一个文件 = 一个业务功能（internal/module/CLAUDE.md §inbound/http）：同一功能的
// 「JSON 接口面 handler」与「后台页面 handler」住在一起 —— 它们改的常常是同一件事
//（新增一个字段要同时进接口入参、表单回填与列表列），按 API / 页面拆开就会各改一处。
//
// 路由不在这里：唯一声明点是 product_router.go。

// ==========================================================================
// 与商品 / 变体接口同形：JSON 入参、pkg/response 出参、消息取 enums。
// 本层只做绑定与转发，key 派生、值的归一与排序一律在 service。
// ==========================================================================
// Create 新建商品（自动生成首个变体）。
func (h *ProductHandle) Create(c *gin.Context) {
	req, ok := binding.JSON[productdto.CreateReq](c)
	if !ok {
		return
	}
	// issue #19：变更记录的操作人从会话取（客户端传入被忽略）。
	req.OperatorID = builtin.OperatorName(c)
	res, err := h.svc.Create(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgCreateSuccess, res)
}

// Update 修改商品。
func (h *ProductHandle) Update(c *gin.Context) {
	req, ok := binding.JSON[productdto.UpdateReq](c)
	if !ok {
		return
	}
	// issue #19：变更记录的操作人从会话取（客户端传入被忽略）。
	req.OperatorID = builtin.OperatorName(c)
	res, err := h.svc.Update(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgUpdateSuccess, res)
}

// Get 商品详情（含变体与价格区间）。
func (h *ProductHandle) Get(c *gin.Context) {
	req, ok := binding.Query[productdto.GetReq](c)
	if !ok {
		return
	}
	res, err := h.svc.Get(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDetailSuccess, res)
}

// List 商品列表。
func (h *ProductHandle) List(c *gin.Context) {
	req, ok := binding.Query[productdto.ListReq](c)
	if !ok {
		return
	}
	list, err := h.svc.QueryProducts(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgListSuccess, list)
}

// Delete 删除商品。
func (h *ProductHandle) Delete(c *gin.Context) {
	req, ok := binding.JSON[productdto.DeleteReq](c)
	if !ok {
		return
	}
	// issue #19：变更记录的操作人从会话取（客户端传入被忽略）。
	req.OperatorID = builtin.OperatorName(c)
	if err := h.svc.Delete(c.Request.Context(), req); err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDeleteSuccess, nil)
}

// BulkDelete 批量删除商品（POST /api/product/bulk-delete）。
//
// 权限点与单条删除分开（product:bulk_delete / product:delete）：sys_permission 上
// permission_code 唯一、超管策略由它逐行生成，一个权限点只能绑一条 API 路由 —— 复用会让
// 其中一条路径拿不到策略而 403。批量删与单删本来也是两次授权决定。
//
// 整批冲突明细由 service 交事实，统一 JSON 出口按请求语言取词。
func (h *ProductHandle) BulkDelete(c *gin.Context) {
	req, ok := binding.JSON[productdto.BulkDeleteReq](c)
	if !ok {
		return
	}
	// 归一（去重 / 上限）、空勾选判定、整批拒绝的明细全在 service 与 pkg/response ——
	// 入口只绑请求、调用例、写响应，不做计算。
	res, err := h.svc.BulkDeleteProducts(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, productErrScene, err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDeleteSuccess, res)
}

// CreateVariant 新增变体（未填字段由商品级默认值补齐）。
func (h *ProductHandle) CreateVariant(c *gin.Context) {
	req, ok := binding.JSON[productdto.CreateVariantReq](c)
	if !ok {
		return
	}
	// issue #19：变更记录的操作人从会话取（客户端传入被忽略）。
	req.OperatorID = builtin.OperatorName(c)
	res, err := h.svc.CreateVariant(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgCreateSuccess, res)
}

// UpdateVariant 修改变体（编辑路径不做默认值填充）。
func (h *ProductHandle) UpdateVariant(c *gin.Context) {
	req, ok := binding.JSON[productdto.UpdateVariantReq](c)
	if !ok {
		return
	}
	// issue #19：变更记录的操作人从会话取（客户端传入被忽略）。
	req.OperatorID = builtin.OperatorName(c)
	res, err := h.svc.UpdateVariant(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgUpdateSuccess, res)
}

// DeleteVariant 删除变体。
func (h *ProductHandle) DeleteVariant(c *gin.Context) {
	req, ok := binding.JSON[productdto.DeleteVariantReq](c)
	if !ok {
		return
	}
	// issue #19：变更记录的操作人从会话取（客户端传入被忽略）。
	req.OperatorID = builtin.OperatorName(c)
	if err := h.svc.DeleteVariant(c.Request.Context(), req); err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDeleteSuccess, nil)
}

// GenerateVariants 按勾选的属性值生成全部变体组合（issue #8）。
//
// 未勾选任何属性值（或 selections 缺省）= 无表单路径：按商品全部参与变体的
// 属性组与其启用值生成；新变体逐字段继承商品级默认值。
func (h *ProductHandle) GenerateVariants(c *gin.Context) {
	req, ok := binding.JSON[productdto.GenerateVariantsReq](c)
	if !ok {
		return
	}
	// issue #19：变更记录的操作人从会话取（客户端传入被忽略）。
	req.OperatorID = builtin.OperatorName(c)
	res, err := h.svc.GenerateVariants(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgVariantGenerateSuccess, res)
}

// ProductsPage 逐份读取业务对象；字段展示与分页链接由模板生成。
func (h *ProductPageHandle) ProductsPage(c *gin.Context) {
	req, ok := binding.Query[productdto.ProductListPageReq](c)
	if !ok {
		return
	}
	scope, err := h.products.ResolveCatalogScope(c.Request.Context(), req.ProjectID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	result, err := h.products.QueryProducts(c.Request.Context(), &productdto.ListReq{ProjectID: scope.ProjectID, Keyword: req.Keyword, Status: req.Status, Page: req.Page, Size: 20})
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	categories, err := h.products.CategoryNodes(c.Request.Context(), scope.ProjectID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	brands, err := h.products.CatalogBrands(c.Request.Context(), scope.ProjectID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	tags, err := h.products.CatalogTags(c.Request.Context(), scope.ProjectID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	warehouses, err := h.products.CatalogWarehouses(c.Request.Context(), scope.ProjectID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	pageview.Render(c, pageview.Objects(pageview.Object("Scope", scope), pageview.Object("Query", req),
		pageview.Object("Result", result), pageview.Object("Categories", categories), pageview.Object("Brands", brands),
		pageview.Object("Tags", tags), pageview.Object("WarehouseOptions", warehouses),
		pageview.Object("Rules", h.products.ListPricingRuleTypes(c.Request.Context())),
		pageview.Object("Roundings", h.products.ListPricingRoundingOptions(c.Request.Context()))))
}

// ProductNewPage 只交创建商品所需的原始候选。
func (h *ProductPageHandle) ProductNewPage(c *gin.Context) {
	req, ok := binding.Query[productdto.ProductNewPageReq](c)
	if !ok {
		return
	}
	scope, err := h.products.ResolveCatalogScope(c.Request.Context(), req.ProjectID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	warehouses, err := h.products.CatalogWarehouses(c.Request.Context(), scope.ProjectID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	attributes, err := h.products.CatalogAttributes(c.Request.Context(), scope.ProjectID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	skus, err := h.products.CatalogWarehouseSKUs(c.Request.Context(), scope.ProjectID, warehouses)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	templates, err := h.products.CatalogEntityTemplates(c.Request.Context(), scope.ProjectID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	pageview.Render(c, pageview.Objects(pageview.Object("Scope", scope), pageview.Object("WarehouseOptions", warehouses),
		pageview.Object("Attributes", attributes), pageview.Object("WarehouseSKUOptions", skus), pageview.Object("TemplateCatalog", templates)))
}

// ProductEditPage 选择器与主体均读取成功才渲染；读取故障不能被解释为主动解绑。
func (h *ProductPageHandle) ProductEditPage(c *gin.Context) {
	req, ok := binding.Query[productdto.ProductEditPageReq](c)
	if !ok {
		return
	}
	scope, err := h.products.ResolveCatalogScope(c.Request.Context(), req.ProjectID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	product, err := h.products.Get(c.Request.Context(), &productdto.GetReq{ProjectID: scope.ProjectID, ID: req.ProductID})
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	categories, err := h.products.CategoryNodes(c.Request.Context(), scope.ProjectID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	brands, err := h.products.CatalogBrands(c.Request.Context(), scope.ProjectID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	tags, err := h.products.CatalogTags(c.Request.Context(), scope.ProjectID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	attributes, err := h.products.CatalogAttributes(c.Request.Context(), scope.ProjectID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	warehouses, err := h.products.CatalogWarehouses(c.Request.Context(), scope.ProjectID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	skus, err := h.products.CatalogWarehouseSKUs(c.Request.Context(), scope.ProjectID, warehouses)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	panel, err := h.products.QueryDetailTemplatePanel(c.Request.Context(), scope.ProjectID, product.ID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	pageview.Render(c, pageview.Objects(pageview.Object("Scope", scope), pageview.Object("Product", product), pageview.Object("Categories", categories),
		pageview.Object("Brands", brands), pageview.Object("Tags", tags), pageview.Object("Attributes", attributes),
		pageview.Object("WarehouseOptions", warehouses), pageview.Object("WarehouseSKUOptions", skus), pageview.Object("TplPanel", panel)))
}

// ProductDetailPage 只读详情直接消费商品对象与模板绑定事实。
func (h *ProductPageHandle) ProductDetailPage(c *gin.Context) {
	req, ok := binding.Query[productdto.ProductDetailPageReq](c)
	if !ok {
		return
	}
	scope, err := h.products.ResolveCatalogScope(c.Request.Context(), req.ProjectID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	product, err := h.products.Get(c.Request.Context(), &productdto.GetReq{ProjectID: scope.ProjectID, ID: req.ProductID})
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	panel, err := h.products.QueryDetailTemplatePanel(c.Request.Context(), scope.ProjectID, product.ID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	pageview.Render(c, pageview.Objects(pageview.Object("Scope", scope), pageview.Object("Product", product), pageview.Object("TplPanel", panel)))
}

// ProductsVariantPreview 返回变体业务数据，规格显示由前端按属性定义格式化。
func (h *ProductPageHandle) ProductsVariantPreview(c *gin.Context) {
	form, ok := binding.Form[productdto.VariantPreviewFormReq](c)
	if !ok {
		return
	}
	res, err := h.products.PreviewVariantCombinations(c.Request.Context(), form.ServiceReq())
	if err != nil {
		response.ErrorStatusAuto(c, productErrScene, err)
		return
	}
	response.Success(c, res)
}

// SaveVariantList 以清单为准保存变体（POST /api/product/variant/save）——变体**唯一的落库动作**。
//
// rows 是整张清单（顺序即期望顺序）：行上只有 variantId（既有变体；新增行为空）、
// 可编辑的 skuCode 与 optionValues。服务端逐条重算（见 service.SaveVariantList），
// 客户端提交的形状一律不作数。
//
// 整批成功返回统一成功消息与计数；冲突由错误出口返回 data.items。
func (h *ProductHandle) SaveVariantList(c *gin.Context) {
	req, ok := binding.JSON[productdto.SaveVariantListReq](c)
	if !ok {
		return
	}
	// issue #19：变更记录的操作人从会话取（客户端传入被忽略）。
	req.OperatorID = builtin.OperatorName(c)
	res, err := h.svc.SaveVariantList(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessMsg(c, productActionDoneMsg, res)
}

// AddRating 补录一条商品评分（issue #33）。
//
// 评分是**独立明细表**（#30），所以这是「加一条记录」而不是「改商品字段」——
// 分值范围由 service 与数据库 CHECK 双重兜底（service 对越界给「评分必须是 0~5 的数字」）。
func (h *ProductHandle) AddRating(c *gin.Context) {
	req, ok := binding.JSON[productdto.AddRatingReq](c)
	if !ok {
		return
	}
	req.OperatorID = builtin.OperatorName(c)
	res, err := h.svc.AddRating(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessMsg(c, productActionDoneMsg, res)
}

// DeleteRating 删掉一条评分（issue #33）：录错了能撤掉。
//
// 请求里的 id 是**评分 id**；工程随请求显式回传（DB-009：product_ratings 有 FORCE 策略，
// 删一条评分要在工程作用域里）。
func (h *ProductHandle) DeleteRating(c *gin.Context) {
	req, ok := binding.JSON[productdto.DeleteRatingReq](c)
	if !ok {
		return
	}
	if err := h.svc.DeleteRating(c.Request.Context(), req); err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessMsg(c, productActionDoneMsg)
}

// ProductPreviewFrame 商品详情页的真实预览帧（GET /admin/products/preview-frame?productId=&project=）。
//
// 与文章编辑页的同类帧同构：用**详情页模板**渲染一份不写库、不激活的预览（PreviewInstance），
// 看到的就是访客看到的样子。拼字段 HTML 那种回显照不出模板的任何东西 —— 商品详情页有图集、
// 变体分组、关联分区，靠拼字符串还原不了。
//
// 代价与文章页一样：只反映**已保存**的数据。改完先保存，再点「刷新预览」。
//
// 为什么 iframe 直接 src：详情页是完整文档（自带样式与脚本），塞进 srcdoc 会与后台页面同源、
// 脚本可能互扰；指一个页面组路由（Session 鉴权、不需 CSRF）最省事也最隔离。
//
// 出口有两个形态：① 预览就绪 —— 直出构建器渲染好的整份 HTML（后端只搬运字节，见 AGENTS.md
// 「前后端分界」）；② 还没就绪 —— 交替代页视图 admin/preview_notice.html（一句人话 + 那份模板里的样式）。
// 两种都不拼 HTML：替代页曾经在这里拼字符串，结构与样式只存在于 Go 里，前端层改不到它。
func (h *ProductPageHandle) ProductPreviewFrame(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	req, ok := binding.Query[productdto.ProductPreviewFrameReq](c)
	if !ok {
		return
	}
	h.renderPreviewFrame(c, req)
}

// renderPreviewFrame 渲染预览帧：成功直出构建器产物字节，其余情况给一页替代提示。
//
// 替代页是模板渲染（不走页壳），取词在这里先做掉；每一档都 return —— 之前少了 return，
// 提示写完之后仍会继续去预览，响应里出现第二段内容。
func (h *ProductPageHandle) renderPreviewFrame(c *gin.Context, req *productdto.ProductPreviewFrameReq) {
	if req.ProductID == "" {
		renderPreviewPlaceholder(c, productenums.ProductsPreviewNeedSave, "保存这个商品后，这里会显示它的详情页真实形态。")
		return
	}
	if h.instances == nil {
		renderPreviewPlaceholder(c, productenums.ProductsPreviewUnavailable, "预览不可用：发布能力未装配。")
		return
	}
	res, err := h.instances.PreviewInstance(c.Request.Context(), &presentationcontract.PreviewInstanceReq{
		EntityType: productcontract.EntityTypeProduct, EntityID: req.ProductID, ProjectID: req.ProjectID,
	})
	if err != nil || res == nil || strings.TrimSpace(res.HTML) == "" {
		if err != nil {
			// 原文只进日志（后台页面不得直出内部错误）；页面上给一句能行动的话。
			logger.Scene("product").With("product_id", req.ProductID).Error(err, "商品详情页预览渲染失败")
			renderPreviewPlaceholder(c, productenums.ProductsPreviewFailed, "预览渲染失败（多半是这个商品还没有可用的详情页模板）。")
		} else {
			renderPreviewPlaceholder(c, productenums.ProductsPreviewEmpty, "还没有可渲染的详情页：先给 product 类型建一套内容模板。")
		}
		return
	}
	// 直出的是**构建器渲染出来的详情页 HTML**（预览产物）：后端只搬运字节，不拼结构。
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(res.HTML))
}

// renderPreviewPlaceholder 预览替代页（结构与样式在 admin/templates 的 preview_notice.html）。
// renderPreviewPlaceholder 预览替代页：只交 key + 兜底，取词由模板完成。
func renderPreviewPlaceholder(c *gin.Context, key, fallback string) {
	pageview.Render(c, productdto.PreviewNoticeResp{Key: key, Fallback: fallback, Lang: response.RequestLanguage(c)})
}

// previewPlaceholderResp 预览替代页的数据（结构与样式在 internal/templates/admin/preview_notice.html）。
//
// 商品与文章两个预览帧共用同一份模板：占位页长得一样，没有理由各写一份。

// ProductPricingPage 只读取工程、规则目录与调价记录；提交结果由 JSON API 返回。
func (h *ProductPageHandle) ProductPricingPage(c *gin.Context) {
	req, ok := binding.Query[productdto.ProductProjectReq](c)
	if !ok {
		return
	}
	scope, err := h.products.ResolveCatalogScope(c.Request.Context(), req.ProjectID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	var history []*productdto.PriceAdjustmentResp
	if scope.ProjectID != "" {
		history, err = h.products.ListPriceAdjustments(c.Request.Context(), &productdto.ListPriceAdjustmentReq{ProjectID: scope.ProjectID, Limit: 10, IncludeItems: true})
		if err != nil {
			pageview.Fail(c, err)
			return
		}
	}
	pageview.Render(c, pageview.Objects(
		pageview.Object("Scope", scope),
		pageview.Object("Rules", h.products.ListPricingRuleTypes(c.Request.Context())),
		pageview.Object("Roundings", h.products.ListPricingRoundingOptions(c.Request.Context())),
		pageview.Object("History", history),
	))
}

// ==========================================================================
// 定价接口：JSON 入参、pkg/response 出参、消息取 enums。
// 本层只做绑定、覆盖操作人与转发：规则校验、算价、落库与留痕一律在 service。
// ==========================================================================
// ListPricingRuleTypes 内置定价规则类型清单（后台下拉与参数说明的唯一来源）。
func (h *ProductHandle) ListPricingRuleTypes(c *gin.Context) {
	response.SuccessWithMessage(c, productenums.MsgListSuccess, h.svc.ListPricingRuleTypes(c.Request.Context()))
}

// ListPricingRoundingOptions 尾数处理清单。
func (h *ProductHandle) ListPricingRoundingOptions(c *gin.Context) {
	response.SuccessWithMessage(c, productenums.MsgListSuccess, h.svc.ListPricingRoundingOptions(c.Request.Context()))
}

// PreviewPricing 按规则试算（不落库、不留痕）。
func (h *ProductHandle) PreviewPricing(c *gin.Context) {
	req, ok := binding.JSON[productdto.PricingPreviewReq](c)
	if !ok {
		return
	}
	res, err := h.svc.PreviewPricing(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgPricingPreviewSuccess, res)
}

// ApplyPricing 应用调价：算价、写回与留痕由 service 保证事务，操作人取会话。
func (h *ProductHandle) ApplyPricing(c *gin.Context) {
	req, ok := binding.JSON[productdto.PricingApplyReq](c)
	if !ok {
		return
	}
	req.OperatorID = builtin.OperatorName(c)
	res, err := h.svc.ApplyPricing(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgPricingApplySuccess, res)
}

// BulkPricing 批量「按规则改价」（POST /api/product/bulk-pricing）。
//
// 权限点独立（product:bulk_pricing）：一个权限点只能绑一条 API 路由（sys_permission 上
// permission_code 唯一），复用 product:update 会让其中一条路径拿不到策略而 403。
//
// 规则参数**平铺在顶层**（ruleType / multiplier / amount / margin / rounding），不套一层 rule
// 对象：页面表单本来就是一个平铺表单（pricing_rule_fields.html），而「哪个规则用哪个参数」的
// 判定全在 dto.PricingParams —— 在这里另写一套装配，就会出现「抽屉填的 amount 被当成
// multiplier」这类只在一个入口发生的错。
func (h *ProductHandle) BulkPricing(c *gin.Context) {
	req, ok := binding.JSON[productdto.BulkPricingJSONReq](c)
	if !ok {
		return
	}
	// 入参形状（含规则字段）由 dto 转成 service 的入参；操作人从会话取（客户端传入被忽略）。
	res, err := h.svc.BulkApplyPricing(c.Request.Context(), req.ServiceReq(builtin.OperatorName(c)))
	if err != nil {
		response.ErrorStatusAuto(c, productErrScene, err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgPricingApplySuccess, res)
}

// ListPriceAdjustments 调价留痕列表（按工程）。
func (h *ProductHandle) ListPriceAdjustments(c *gin.Context) {
	req, ok := binding.Query[productdto.ListPriceAdjustmentReq](c)
	if !ok {
		return
	}
	list, err := h.svc.ListPriceAdjustments(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgListSuccess, list)
}

// GetPriceAdjustment 单批次留痕详情（含逐变体「原价 → 新价」明细）。
func (h *ProductHandle) GetPriceAdjustment(c *gin.Context) {
	req, ok := binding.Query[productdto.GetPriceAdjustmentReq](c)
	if !ok {
		return
	}
	res, err := h.svc.GetPriceAdjustment(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDetailSuccess, res)
}
