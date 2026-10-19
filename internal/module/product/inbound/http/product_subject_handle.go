package producthttp

// product_subject.go — 商品主体：接口面 CRUD / 变体 / 评分、后台列表与编辑页、捆绑配置、定价。
//
// 为什么这三块在同一个文件：捆绑配置是 type=bundle 商品的一种编辑能力，定价是商品价格字段上的
// 操作，两者都不是独立域。按子功能切文件的结果是「改一个商品的写入要在三个文件里找落点」，
// 而路由表本身只按业务域分节（见 product_router.go）。

import (
	"fmt"
	"go_wp/pkg/binding"
	"go_wp/pkg/facing"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	presentationcontract "go_wp/internal/module/presentation/contract"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/internal/shell"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
)

// 商品列表页的地址：详情页等整页 GET 在「资源已不在了」时 302 回列表。
const productListPath = "/admin/products"

// 本文件各页的筛选上下文键表（每页一份）。
//
// 同一份键表服务两条路径：**渲染时**拼进表单 action 的 query、**提交回来时**按同一份表读回。
// 两处分叉的表现是「改完筛选再操作，回来静默丢了筛选」—— 页面不报错、日志也干净，
// 所以键表只留这一份，不要在两处各写字面量。
var (
	productPricingBackKeys = []string{"project"}
)

// productErrScene 日志场景名（与 product 模块其它 logger.Scene("product") 一致）。
const productErrScene = "product"

// productPageInternalText 页面槽位（片段里的一行结论 / 整页失败槽）的归口文案。
//
// 与 JSON 出口的 response.MessageOf 只差一句话的措辞：页面上用的是带 i18n 词条的
// 「系统内部错误，请稍后重试」（MsgInternalError），接口用的是 pkg/response 的常量
// 「服务器内部错误，请稍后重试」。两者在 sys_i18n 里各有词条，所以这里不合并；
// 判据（什么算业务错误、要不要记日志）仍然只有一份，在 pkg/facing 与 pkg/response。
func productPageInternalText(c *gin.Context, err error) string {
	if err != nil {
		logger.Scene(productErrScene).
			With("user_id", builtin.CurrentUserID(c)).
			Error(err, "商品后台页取数失败（非业务错误，只对页面给归口文案）")
	}
	return response.InternalText(c)
}

// product_core.go — 商品本体的控制器：接口面 CRUD / 变体 + 列表 / 新建 / 编辑 / 详情页 / 批量。
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
	list, err := h.svc.List(c.Request.Context(), req)
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
// 逐条原因在这里取词：service 只回业务 key（ReasonKey），成品句由 productErrText 按请求
// 语言给。取词只在 inbound 有，service 拿不到也不该拿。
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

// 编辑路径的默认值语义由 service 保证：本层只做绑定与转发，不补字段。
var _ = http.StatusOK

// ==========================================================================
// 商品列表页：筛选、分页、行内状态与分类/标签单元格。
//
// 筛选条与分页是全后台通用形态（GET 表单 + select）；本页不拼筛选上下文 query ——
// 列表页的返回地址只需要工程一个条件（productListFilterURL）。

// ProductsPage 商品管理页：工程切换 + 商品列表 + 每个商品的变体面板 + 内联新建表单。
//
// 三动作：绑参数 → 调读模型 → 返回视图。取数失败直接返回错误，页面出口由 adminui.Page
// 统一写成 {code,message,data}（业务 400 / 其余 500），模板里没有失败分支。
func (h *ProductPageHandle) ProductsPage(c *gin.Context) {
	req, ok := binding.Query[productdto.ProductListPageReq](c)
	if !ok {
		return
	}
	view, err := h.reader.ProductListPage(c.Request.Context(), req)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	pageview.Render(c, view)
}

// productListPageSize 商品列表每页条数（服务端分页）。
//
// 唯一实现在 service 的 ProductListPageSize：列表页取数已下沉进读模型，两处各写一个 20
// 时改一处会让「共 N 条」的推算与实际取回的行数对不上。
//
// 20 与后台其它列表页同一量级：列表行要逐个取商品详情组装视图，
// 一页取太多等于把「翻页」换成「一屏卡顿」。
const productListPageSize = productservice.ProductListPageSize

// productPageNumber 解析页码：空值 / 非数字 / 越界一律退回第 1 页。
//
// 查询串不是可信边界：page=abc、page=-3、page=999999 都会走到这里，
// 服务端自行归一，绝不把非法页码透给取数层（负 offset 在 PG 上是错误）。
// ==========================================================================
// 商品新建页。
//
// 表单的提交已迁到 JSON 写基座（static/js/ui/json_write.js + /api/product/create），页面这边
// 只剩「读数据 → 渲染」：没有回填槽、没有失败重渲染、也不再有页面的写 handler。
// 字段名与 dto.CreateReq 的 json 标签的逐字对齐由 product_create_form_protocol_test.go 钉住
// —— 模板写错一个 name 的后果是字段静默丢失（商品建出来了，某个字段是空的）。

// 左侧基础信息表单（字段名与 dto.CreateReq 一致），
// 右侧模板卡片区（默认模板 + 候选列表 + 预览入口）；模板绑定与可视化自定义
// 在创建后的详情页继续（create 不落实例，绑定发生在首次发布）。

// —— 商品建表单的失败分档出口（渐进增强；模板见 admin/product/product_create_form.html）——
//
// 本节起是原先的 product_create.go（新建页与它的表单字段协议）。
// ==========================================================================

// ProductNewPage GET /admin/products/new：商品新建整页。
//
// 本批（整页 GET 取数下沉）之后控制器只剩三步：**读参数 → 调读模型 → 渲染**。
// 工程清单、工程存在性校验、仓库选项、属性组选项、每仓 SKU 候选、模板清单与默认模板、
// 15 个 data 键的装配、以及「哪个失败点算 400 / 哪个算 500」的编排，全部在
// service.ProductNewPage 里（取数口径与页面的档位是业务结论，不是 HTTP 结论）。
//
// 三个具体后果：
//
//	· 控制器里不再有 gin.H 装配与取数助手调用 —— 页面数据只有一个来源（dto 视图）；
//	· 状态码与 no-store 由视图带出来（view.Result），不再在这里判档，
//	  于是「失败页长什么样」不再有第二种写法；
//	· 失败槽文案（LoadError）仍在这里取词：取词是**请求级**的（按当前语言），
//	  交给 service 就等于把语言钉进业务层。
func (h *ProductPageHandle) ProductNewPage(c *gin.Context) {
	// 读模型在装配期注入（见 ProductPageHandle.reader）：漏接 = 编译错误，
	// 不再有「请求期断言失败 → 500 兜底」这条路径。
	reader := h.reader
	req, ok := binding.Query[productdto.ProductNewPageReq](c)
	if !ok {
		return
	}
	view, err := reader.ProductNewPage(c.Request.Context(), req)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	pageview.Render(c, view)
}

// ==========================================================================
// 商品编辑页（整页 + 回显）。
//
// 保存走 JSON 写基座（static/js/ui/json_write.js）：字段名与 /api/product/update 的
// UpdateReq json 标签逐字一致，由 product_edit_form_protocol_test.go 对账；
// 失败只在原页结果区出现，服务端不再有「把刚提交的值原样送回」那一层。

// ProductEditPage GET /admin/products/edit：商品基本字段编辑页。
// 为什么单独成页，而不是并进商品详情页：
//   - 详情页的职能是**子资源维护**（变体清单 / 评分 / 属性引用 / 分类与品牌 / 手工标签），
//     那里的每个表单都作用在「这个商品的某个子资源」上；
//   - 商品**自身**的字段（名称 / URL 段 / SKU / 状态 / 价格 / 单位 / 重量 / SEO / 图集）
//     自本页出现之前**只有创建时能填**，建完之后再也没有入口 —— 编辑入口缺位是真实缺口，
//     不是版式问题；
//   - 商品域翻译工作台（多语言）原先占着列表操作列的一格，本批收进编辑页：多语言改的是
//     这个商品的字段译文，与「改这个商品」是同一件事。
//
// 表单协议：字段名与 /api/product/update 的 UpdateReq 的 json 标签**逐字对齐**
// （name / slug / sku / status / defaultPrice / unit / weight / seoTitle / seoDescription /
// images / imageAlts / attributeIds / categoryIds / primaryCategoryId / brandId / tagIds），
// 改字段名等于改协议。
//
// 提交后回编辑页（PRG）而不是回列表：用户在这里改的是**这一个商品**，
// 弹回列表等于让他重新找一遍再点进来（与详情页的写操作同一口径）。
//
// 本节起是原先的 product_edit.go（编辑页，字段名与 UpdateReq 的 json 标签逐字对齐）。
// ==========================================================================

func (h *ProductPageHandle) ProductEditPage(c *gin.Context) {
	req, ok := binding.Query[productdto.ProductEditPageReq](c)
	if !ok {
		return
	}
	h.renderProductEditPage(c, req.ProjectID, req.ProductID, "", false)
}

func (h *ProductPageHandle) renderProductEditPage(c *gin.Context, selected, productID, submitErr string, _ bool) {
	// 取数编排、表单回显、返回地址全在读模型里（service.ProductEditPage 末尾调 FillProductEditFormEcho）。
	view, err := h.reader.ProductEditPage(c.Request.Context(), &productdto.ProductEditPageReq{
		ProjectID: selected, ProductID: productID,
	})
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	// 写动作的结论不在本页回显（走 JSON 回执 + flash 行）；只剩原地重渲染这一条路会给 Err。
	view.Err = submitErr
	// 属性组候选读失败：撤掉保存入口并按当前语言归口 ——「选择器缺失」与「用户主动取消了
	// 全部引用」在 POST 上同形，读故障时不能装作后者。
	if view.AttributeOptionsErr != nil {
		view.EditBlocked = true
		if view.Err == "" {
			view.Err = response.MessageOf(c, "product", view.AttributeOptionsErr)
		}
	}
	// 详情页模板面板：编辑页与详情页共用同一份组装（写动作都在编辑页这一屏）。
	view.TplPanel = h.detailTemplatePanel(c.Request.Context(), view.SelectedProject, view.ProductID)
	pageview.Render(c, view)
}

// ==========================================================================
// 商品详情页：变体生成/预览/保存、评分、属性挂载、删除与预览框。
//
// 变体从「规格属性组合」生成：预览先算再存（variantPreviewRow），保存按 fingerPrint 覆盖式写入。

// ProductDetailPage GET /admin/products/detail：单个商品的详情页（**只读**）。
//
// 为什么是独立页，而不是列表页里的第二、三张表（admin-ui-logic §1）：列表页只该回答
// 「有哪些商品」；变体与评分是**某个商品的子资源**，属于该商品的详情。原来三张表平铺在
// 列表页上，等于让列表页承载实体详情 —— 商品一多，变体表与评分表就是两份与商品表错位的
// 长表，改一个商品要跨三处找入口。
//
// 本节起是原先的 product_detail_page.go（详情页）。
// ==========================================================================

// 本页只**看**：基本信息 / 属性引用 / 分类与品牌 / 标签 / 捆绑构成 / 变体清单 / 评分 /
// 详情页模板的绑定状态。全部写动作（含变体与评分）在 ProductEditPage ——
// 读与写混在同一页时用户分不清「我在看还是在改」，而且每个写表单都要带 CSRF、
// 错误回显与回跳地址，那是编辑页的职责。
//
// 商品不存在（含没给 product 参数）渲染 .empty-state + 返回列表链接，**不 500**：
// 手输 URL、书签失效、商品刚被删都会走到这里，500 什么也说明不了。
// 但状态码要说实话：这是「这个 URL 指向的商品不存在」= 404 + no-store
// （契约见 docs/rules/template-boundary.md §整页 GET 的失败出口），此前一律 200 且可缓存。
//
// 取数**失败**与「不存在」此前也混在同一条路径上：`derr == nil && detail != nil` 把
// 「读不出来」（连接断了、权限、超时）一起吞成了「商品不存在」空态 —— 用户以为商品被删了。
// 现在三档分开：业务上的「不存在」= 404；参数不合法 = 400；其余 = 500 + no-store。
//
// 取数编排（工程 / 仓库 / 商品 / 分类 / 品牌 / 标签）与上面三档的判定都在 service 的读模型里
// （service.ProductDetailPage）；本函数只剩「取参数 → 调读模型 → 补控制器负责的派生值 → 一个出口」。
func (h *ProductPageHandle) ProductDetailPage(c *gin.Context) {
	req, ok := binding.Query[productdto.ProductDetailPageReq](c)
	if !ok {
		return
	}
	// 取数、三档判定、行装配、捆绑构成、返回地址全在 service.ProductDetailPage。
	view, err := h.reader.ProductDetailPage(c.Request.Context(), req)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	// 详情页模板面板：详情页只读展示绑定状态与预览入口，编辑动作在编辑页 —— 两页共用一份组装。
	view.TplPanel = h.detailTemplatePanel(c.Request.Context(), view.SelectedProject, view.ProductID)
	pageview.Render(c, view)
}

// variantPreviewRow 预览行的 JSON 形状：前端拿它渲染清单里的一行。
//
// Spec 是**服务端**拼的可读规格文本（与详情页表格同一份 specLabel），
// 前端不自己拼属性名 —— 属性值改名后前端那份就会显示旧名字。
// ProductsVariantPreview 组合生成的**预览**（POST /admin/products/variant/preview，不落库）。
//
// 用户口径（docs/14 §8）：「生产只是显示，并不会存入数据库，必须保存才行」。
// 因此本端点只回答「点这次生成会往前端清单里追加哪些行」：
//   - 笛卡尔积、维度与数量上限、与既有组合的去重全在 service（与落库路径同一套规则）；
//   - SKU 由服务端按变体 SKU 规则生成（前端不复刻这套算法），运营可在清单里逐行改写；
//   - 响应是 JSON 而不是 HTMX 片段：前端要把这些行**追加**进已渲染的清单 DOM，
//     而清单的初始行由服务端渲染（库里已有变体），两者是同一种行结构。
//
// 失败回 400 + 可读文案（走 productErrText 取词，不把 enums 裸 key 或 PG 报错铺给前端）。
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
	// 规格文本（Spec）由 service 用同一份属性组算出：控制器不再为了拼这句话回查一次商品
	// （那次回查可以静默拿到空 attrs，于是整列规格变「—」而不报错）。
	res.OK = true
	c.JSON(http.StatusOK, res)
}

// variantSkipFallbacks 清单外删除被跳过时的中文兜底（key 见 productenums.VariantSkip*）。
var variantSkipFallbacks = map[string]string{
	productenums.VariantSkipHasStock:       "仍有库存，未删除（请先处理库存或改为停用）",
	productenums.VariantSkipReferenced:     "被 BOM 清单引用，未删除（请先解除引用）",
	productenums.VariantSkipDuplicated:     "与清单里前面的行重复，已忽略",
	productenums.VariantSkipVariantMissing: "这一行对应的变体已不存在（可能已被别处删除）",
	// 本批新增的两个引用面（迁移 259）：被捆绑成员引用 / 有过库存流水。
	productenums.VariantSkipBundleReferenced: "被捆绑成员引用，未删除（请先在捆绑配置里解除引用）",
	productenums.VariantSkipHasMovement:      "有过库存流水（已被订单或库存变动用过），未删除（请改为停用）",
}

// productBulkText 一条结论文案的事实（key + 中文兜底）。
type productBulkText struct{ key, fallback string }

// fact 把一条结论文案变成**事实**（key + 中文兜底 + %s 参数）：模板在渲染期取词，
// JSON 出口用 text.MsgText 取成品串。
//
// 参数一律 strconv 成字符串（模板侧的占位符是 %s，传 int 会渲染成 %!s(int=3)）；
// 没有占位符的词条**不要**传参数 —— TranslateArgs 对多余参数会 Sprintf 出 %!(EXTRA string=…)。
func (t productBulkText) fact(args ...string) facing.Msg {
	m := facing.Msg{Key: t.key, Fallback: t.fallback}
	if len(args) > 0 {
		m = m.WithArgs(args...)
	}
	return m
}

// productBulkTextOf 取一条批量结论文案的当前语言模板（**文本**形态，给「多段拼一句」的合成句用）。
//
// 词条混进 %d 之类协议外占位符时回落中文原文（模板一律只允许 %s，数字先经 strconv.Itoa）——
// 否则 Sprintf 会把参数渲染成 int，而这条路直接给运营看。
func productBulkTextOf(c *gin.Context, t productBulkText) string {
	built := shell.TranslateFor(c)(t.key, t.fallback)
	if !i18n.HasStringPlaceholdersOnly(built) {
		return t.fallback
	}
	return built
}

// 变体清单保存的结论文案（详情页编辑表单的回执）。
var (
	productVariantSaveNoChange = productBulkText{productenums.BulkVariantNoChange, "变体清单与库里一致，没有需要保存的变化。"}
	productVariantSaveSaved    = productBulkText{productenums.BulkVariantSaved, "已保存变体清单：新增 %s 个、修改 SKU %s 个、删除 %s 个。"}
	productVariantSaveSkipped  = productBulkText{productenums.BulkVariantSkipped, "跳过 %s 个：%s"}
)

// variantSaveMsg 保存结果的回带结论（**事实**：单一情形给 key，多段拼接给成品串）。
//
// 「一条都没变」与「跳过若干」必须能区分：都写成「保存成功」时，用户无法判断是
// 清单本来就与库里一致，还是有行被拦下了（被拦下的行下一次还会出现在清单里）。
//
// 带跳过明细时是「两句词条 + 逐条原因」拼起来的复合句，一段 key 表达不了 —— 这一档给
// 成品串（Fallback，Key 为空），与 page 模块 pageTranslationsSavedMsg 同一取舍。
func variantSaveMsg(c *gin.Context, res *productdto.SaveVariantListResp) facing.Msg {
	if res == nil {
		return response.InternalMsg()
	}
	if res.Created == 0 && res.Updated == 0 && res.Deleted == 0 && len(res.Skipped) == 0 {
		return productVariantSaveNoChange.fact()
	}
	// 例外（需要成品串）：这条结论是**多段词条拼出来的复合句**（「已保存…」+「跳过 N 条：原因」），
	// 一段 key 表达不了 —— 它回的是 Fallback 成品串（Key 留空），与 page 模块同一取舍。
	tr := shell.TranslateFor(c)
	// 数字一律 strconv.Itoa 成字符串后填入（词条只允许 %s）。
	parts := []string{fmt.Sprintf(productBulkTextOf(c, productVariantSaveSaved),
		strconv.Itoa(res.Created), strconv.Itoa(res.Updated), strconv.Itoa(res.Deleted))}
	if len(res.Skipped) > 0 {
		// 只报**原因**，不再把「哪个 SKU / 什么规格」拼进结论：
		// 受控形态下（每条原因都来自 variantSkipFallbacks 的枚举）文案可读且不泄漏。
		// 被跳过的行本来就留在清单里，逐行 SKU 由页面本身呈现。
		items := make([]string, 0, len(res.Skipped))
		for _, skip := range res.Skipped {
			items = append(items, tr(skip.Reason, variantSkipFallbacks[skip.Reason]))
		}
		parts = append(parts, fmt.Sprintf(productBulkTextOf(c, productVariantSaveSkipped),
			strconv.Itoa(len(res.Skipped)), strings.Join(items, "；")))
	}
	return facing.Msg{Fallback: strings.Join(parts, " ")}
}

// SaveVariantList 以清单为准保存变体（POST /api/product/variant/save）——变体**唯一的落库动作**。
//
// rows 是整张清单（顺序即期望顺序）：行上只有 variantId（既有变体；新增行为空）、
// 可编辑的 skuCode 与 optionValues。服务端逐条重算（见 service.SaveVariantList），
// 客户端提交的形状一律不作数。
//
// 结论是一句**复合句**（「已保存 N 条」+「跳过 M 条：原因」）：一段 key 表达不了，因此走
// message 回给页面；页面用 data-write-flash 把它带过保存后的整页刷新 —— 清单刷新后才是库里的
// 样子，被跳过的那几行也才会重新出现在清单里（它们本来就是「没能写进去」的那些行）。
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
	response.SuccessMsg(c, variantSaveMsg(c, res), res)
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
	tr := shell.TranslateFor(c)
	if req.ProductID == "" {
		renderPreviewPlaceholder(c, tr, productenums.ProductsPreviewNeedSave, "保存这个商品后，这里会显示它的详情页真实形态。")
		return
	}
	if h.instances == nil {
		renderPreviewPlaceholder(c, tr, productenums.ProductsPreviewUnavailable, "预览不可用：发布能力未装配。")
		return
	}
	res, err := h.instances.PreviewInstance(c.Request.Context(), &presentationcontract.PreviewInstanceReq{
		EntityType: productcontract.EntityTypeProduct, EntityID: req.ProductID, ProjectID: req.ProjectID,
	})
	if err != nil || res == nil || strings.TrimSpace(res.HTML) == "" {
		if err != nil {
			// 原文只进日志（后台页面不得直出内部错误）；页面上给一句能行动的话。
			logger.Scene("product").With("product_id", req.ProductID).Error(err, "商品详情页预览渲染失败")
			renderPreviewPlaceholder(c, tr, productenums.ProductsPreviewFailed, "预览渲染失败（多半是这个商品还没有可用的详情页模板）。")
		} else {
			renderPreviewPlaceholder(c, tr, productenums.ProductsPreviewEmpty, "还没有可渲染的详情页：先给 product 类型建一套内容模板。")
		}
		return
	}
	// 直出的是**构建器渲染出来的详情页 HTML**（预览产物）：后端只搬运字节，不拼结构。
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(res.HTML))
}

// renderPreviewPlaceholder 预览替代页（结构与样式在 admin/templates 的 preview_notice.html）。
func renderPreviewPlaceholder(c *gin.Context, tr func(string, string) string, key, fallback string) {
	pageview.Render(c, previewPlaceholderResp(c, tr(key, fallback)))
}

// previewPlaceholderResp 预览替代页的数据（结构与样式在 internal/templates/admin/preview_notice.html）。
//
// 商品与文章两个预览帧共用同一份模板：占位页长得一样，没有理由各写一份。
func previewPlaceholderResp(c *gin.Context, text string) productdto.PreviewNoticeResp {
	return productdto.PreviewNoticeResp{Text: text, Lang: response.RequestLanguage(c)}
}

// product_bundle.go — 捆绑商品（容器 + 可选组）：配置页、成员解析接口与详情页的捆绑面板。
//
// 可用量来自库存真源端口（warehouseOptions / warehouseSKUOptions）：捆绑的「可卖几份」
// 由各成员的最小可用量决定，不在商品表里存。

// ProductBundlePage 捆绑配置页。
// 与商品页同族（挂在 ProductPageHandle 上）：选工程 → 选商品 → 配选项规则 → 保存。
// 表单是**原生并行数组**（variantId / required / defaultQty / minQty / maxQty 各自多值），
// 零自定义 JS 就能表达「一行的多个字段」—— 不用 JSON 字符串塞进隐藏域，
// 也就没有「前端拼 JSON 出错但服务端照单全收」的缝隙。
//
// 页面底部内嵌前台配置器片段（hx-get /_fragments/bundleConfigurator）：
// 运营在这里看到的就是访客看到的那一份渲染。

// 为什么单独成文件：这一段是「仓库侧只回答这条货在这个仓叫什么」在商品页的唯一落点 ——
// 候选按仓分组、每仓有上限、未接库存契约时整块降级为空。与商品页其余部分（列表 / 详情 /
// 抽屉业务字段）没有耦合，放在一起只会让两边互相牵制。
//
// 前端只是便捷入口：ProductsCreate 会带着仓库 id 让 service 在**该仓**重新复核一遍。
//
// 接口面（原先的 product_handle.go 捆绑段）在前，后台页面在后。

// 保存已迁到 JSON 写基座：基座在原页发 fetch、失败只写结果区，不再有「htmx 写失败换片段」
// 这种响应形态，页面装配也因此只有一处出口。

// ProductBundlePage 捆绑配置页。
// 与商品页同族（挂在 ProductPageHandle 上）：选工程 → 选商品 → 配选项规则 → 保存。
//
// 页面底部内嵌前台配置器片段（hx-get /_fragments/bundleConfigurator）：
// 运营在这里看到的就是访客看到的那一份渲染。
//
// 为什么单独成文件：这一段是「仓库侧只回答这条货在这个仓叫什么」在商品页的唯一落点 ——
// 候选按仓分组、每仓有上限、未接库存契约时整块降级为空。与商品页其余部分（列表 / 详情 /
// 抽屉业务字段）没有耦合，放在一起只会让两边互相牵制。
//
// 保存走 JSON 写基座（表单声明 data-write-api=/api/product/bundle/set）：成员行由
// static/js/ui/product_bundle_write.js 组装成 {productId, config:{...}}，页面只负责渲染。
func (h *ProductPageHandle) ProductBundlePage(c *gin.Context) {
	h.renderBundlePage(c)
}

// renderBundlePage 整页出口（只有这一处）。
//
// 读模型在装配期注入（见 ProductPageHandle.reader）：漏接 = 编译错误，
// 不再有「请求期断言失败 → 500 兜底」这条路径。
func (h *ProductPageHandle) renderBundlePage(c *gin.Context) {
	req, ok := binding.Query[productdto.ProductBundlePageReq](c)
	if !ok {
		return
	}
	view, err := h.reader.ProductBundlePage(c.Request.Context(), req)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	pageview.Render(c, view)
}

// bundlePageFailureView 失败档的视图（给齐模板必需键，避免失败分支只设了错误键）。
func bundlePageFailureView(status int) productdto.ProductBundlePageResp {
	return productdto.ProductBundlePageResp{
		Projects:         []projectdto.ProjectResp{},
		Products:         []*productdto.ProductResp{},
		Skus:             []*productdto.BundleSKUResp{},
		Rows:             []productdto.BundleOptionResp{},
		CandidateSKUs:    []productdto.BundleCandidateResp{},
		Sources:          []productdto.PickOptionResp{},
		SourceProducts:   []productdto.PickOptionResp{},
		SourceAttributes: []*productdto.AttributeResp{},
		SourceWarehouses: []productdto.BundleSourceWarehouseGroupResp{},
		MaxOptions:       productdto.BundleMaxOptionsLimit,
		SaveBlocked:      true,
	}
}

// bundleQueryParam 取本页的上下文参数（工程 / 商品），按候选名依次认第一个非空值。
//
// 参数名有两套且都合法：本页自己的两个选择器用 project / product；商品详情页的
// 「编辑捆绑构成」入口用 projectId / productId（与商品页其余表单同一套字段名，
// 直接复用商品 id 不会串）。两个都认，入口从哪来都能选中同一个商品 ——
// 与变体清单的预览端点（/admin/products/variant/preview）同一形态：服务端把候选行与逐条
// 跳过原因算好回 JSON，前端只负责把行**追加**进成员清单；「保存配置」才是落库动作。
//
// 为什么要 JSON 而不是服务端渲染的片段：成员清单是**未保存的前端状态**（与变体清单同理），
// 服务端渲染的片段落不进那个状态里；这里回的行结构与服务端渲染的行结构逐字一致。
//
// 失败回 400 + 可读文案（走 productErrText，不把 enums 裸 key 或 PG 报错铺给前端）。
func (h *ProductPageHandle) ProductsBundleMembersResolve(c *gin.Context) {
	ctx := c.Request.Context()
	// 属性组合的勾选用生成组合抽屉的同一套字段名（attr:<属性组 id>=值 id）：
	// 收拢规则只有一份（dto.SelectionsFromForm），两个入口不会一个认前缀、另一个漏读。
	req, ok := binding.JSON[productdto.ResolveBundleMembersReq](c)
	if !ok {
		return
	}
	res, err := h.products.ResolveBundleMembers(ctx, req)
	if err != nil {
		response.ErrorStatusAuto(c, productErrScene, err)
		return
	}
	// 回执就是响应：行结构、规格文本、来源标签、跳过原因全部由 service 填好
	// （属性组与取词函数它都有）—— 控制器不再为拼一句 spec 回查一次商品。
	c.JSON(http.StatusOK, res)
}

// warehouseSKUOptionSize 已随取数下沉搬到 outbound 适配器
//（internal/module/product/outbound/inventory 的 warehouseSKUOptionSize）：
// 每仓取多少条候选是**取数口径**，与页面怎么渲染无关。

// ==========================================================================
// 捆绑接口的四个出口：读配置、存配置、整单校验、SKU 数据源。
// 整单校验在这里是**后端硬校验**：前端片段调的是同一个 service 方法，
// 因此「构造请求绕过前端」得到的结论与前台一致（验收 4）。
// ==========================================================================
// GetBundleConfig 读某商品的捆绑配置（含每项 SKU 快照与真源可用量）。
func (h *ProductHandle) GetBundleConfig(c *gin.Context) {
	req, ok := binding.Query[productdto.GetBundleConfigReq](c)
	if !ok {
		return
	}
	res, err := h.svc.GetBundleConfig(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDetailSuccess, res)
}

// SetBundleConfig 保存捆绑配置（整体替换）。
func (h *ProductHandle) SetBundleConfig(c *gin.Context) {
	req, ok := binding.JSON[productdto.SetBundleConfigReq](c)
	if !ok {
		return
	}
	// issue #19：配置变更要落主数据变更记录，操作人只能来自会话。
	req.OperatorID = builtin.OperatorName(c)
	res, err := h.svc.SetBundleConfig(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgBundleSaveSuccess, res)
}

// ValidateBundleSelection 整单硬校验 + 算价（纯读，不写库）。
func (h *ProductHandle) ValidateBundleSelection(c *gin.Context) {
	req, ok := binding.JSON[productdto.ValidateBundleSelectionReq](c)
	if !ok {
		return
	}
	res, err := h.svc.ValidateBundleSelection(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgBundleValidateSuccess, res)
}

// ListBundleSKUs 可挑选的 SKU 清单（跨商品，后台配置器的下拉数据源）。
func (h *ProductHandle) ListBundleSKUs(c *gin.Context) {
	req, ok := binding.Query[productdto.ListBundleSKUReq](c)
	if !ok {
		return
	}
	list, err := h.svc.ListBundleSKUs(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgListSuccess, list)
}

// —— 定价工具（预览 → 应用 → 台账）——
//
// 预览与应用的输入是同一份表单解析（readPricingForm），只是前者不落库 —— 两条路径
// 各解析一次的话，「预览说会变成 9.9、应用后变成 10.0」这种分叉不会报错。
//
// 与属性 / 分类 / 品牌页同一模式：GET 渲染完整页，只用 GET/POST。三个不同之处：
//
//  1. **预览**（验收 4）：POST /admin/product-pricing/preview 直接把试算结果渲染回同一页
//     （200，不是提示页）—— 预览必须能看到明细，而明细只在这一次响应里；
//     表单值原样回填，用户接着点「应用调价」用的就是刚刚预览过的那份参数。
//     它是**只读**的，所以仍是页面 POST（原生表单 + csrf_token 隐藏域）。
//  2. **应用**（验收 3）：POST /api/product/pricing/apply 走 service 落库 + 留痕，
//     由 ui/json_write.js 提交，成功回执含本次改动的变体数（页面上是 flash 结论行）。
//  3. **留痕**（验收 4）：页面下半部分是调价台账，每个批次可展开看到逐变体「原价 → 新价」。
//
// 接口面在前，后台页面在后。

// ProductPricingPage 定价工具页：规则参考表 + 改价表单 + 留痕台账。
func (h *ProductPageHandle) ProductPricingPage(c *gin.Context) {
	h.renderPricingPageWith(c, nil, nil, "")
}

// ProductPricingPreview 试算（不落库、不留痕），结果渲染回同一页。
func (h *ProductPageHandle) ProductPricingPreview(c *gin.Context) {
	req, ok := binding.Form[productdto.PricingFormReq](c)
	if !ok {
		return
	}
	form := req.Form()
	preview, err := h.products.PreviewPricing(c.Request.Context(), &productdto.PricingPreviewReq{PricingRuleReq: *req.Rule()})
	if err != nil {
		// 试算失败不重定向：用户填的表单要留在眼前，否则「哪一项填错了」无从改起。
		// 试算失败的原文（含 PG 原文）只进日志，页面上给可读文案。
		h.renderPricingPageWith(c, &form, nil, response.MessageOf(c, "product", err))
	}
	h.renderPricingPageWith(c, &form, preview, "")
}

// renderPricingPageWith 定价页的统一渲染入口（form / preview / errMsg 三者可空）。
//
// 取数编排（工程清单 / 规则参考表 / 改价表单初值 / 留痕台账）与视图装配都在 service 的读模型
// （`ProductService.ProductPricingPage`）—— 这里只剩三件事：读参数、调读模型、渲染。
//
// form / preview / errMsg 是**写路径的产物**（试算失败要原地重渲染，把用户填的表单与试算结果
// 带回来）。service 不认识 gin.Context，所以由这里读出来经 Req 传进去。
func (h *ProductPageHandle) renderPricingPageWith(c *gin.Context, form *productdto.PricingForm, preview *productdto.PricingPreviewResp, errMsg string) {
	// 读模型在装配期注入（见 ProductPageHandle.reader）：漏接 = 编译错误，
	// 不再有「请求期断言失败 → 500 兜底」这条路径。
	reader := h.reader
	req, ok := binding.Query[productdto.ProductPricingPageReq](c)
	if !ok {
		return
	}
	// 表单回显与试算结果是**写路径的产物**（本次请求里已经在内存中），不来自查询串。
	req.Form, req.Preview, req.ErrMsg = form, preview, errMsg
	view, err := reader.ProductPricingPage(c.Request.Context(), req)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	// 表单 action 的 query：预览走页面 POST，筛选上下文要随表单带回来（URL 形状属 HTTP 边界）。
	view.ListQuery = binding.ListQuery(c, productPricingBackKeys...)
	pageview.Render(c, view)
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

// ApplyPricing 应用调价（POST /api/product/pricing/apply）：算价 → 写回 product_variants.price
// → 留痕（同一事务）→ 顺带重算自动标签。
//
// 入参**平铺**（ruleType / multiplier / amount / margin / rounding / scope / …），不套
// ruleParams 子对象：页面表单本来就是平铺的（partials/pricing_rule_fields.html），而
// 「哪个规则用哪个参数」的判定只有 pricingParamsFromForm 一份 —— 在这里另写一套装配，
// 就会出现「填了 amount 却被当成 multiplier」这类只在一个入口发生的静默错配
// （与 BulkPricing 同一条理由）。
//
// 页面调用它时提交的字符串值也说明了为什么不能直接用 PricingApplyReq：
// ruleParams 是 json.RawMessage，DOM 里只有字符串，{"multiplier":"1.2"} 会被
// ruleFloat 当成 "1.2" 去 ParseFloat（带引号）而判成「不是数字」。
func (h *ProductHandle) ApplyPricing(c *gin.Context) {
	req, ok := binding.JSON[productPricingApplyReq](c)
	if !ok {
		return
	}
	res, err := h.svc.ApplyPricing(c.Request.Context(), &productdto.PricingApplyReq{
		PricingRuleReq: productdto.PricingRuleReq{
			ProjectID: req.ProjectID, RuleType: req.RuleType, Rounding: req.Rounding,
			Scope: req.Scope, TargetID: req.TargetID,
			Status: req.Status, Keyword: req.Keyword, CategoryID: req.CategoryID,
			BrandID: req.BrandID, TagID: req.TagID,
			RuleParams: productdto.PricingParams(req.RuleType, req.Multiplier, req.Amount, req.Margin),
		},
		Note: strings.TrimSpace(req.Note),
		// 操作人取自会话，客户端传什么都不作数（留痕的操作人不可伪造）。
		OperatorID: builtin.OperatorName(c),
	})
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	// 回执沿用页面原本那句话（「本次改动 N 个变体，明细见下方留痕台账」）：三段拼一句
	// （前缀 + 计数 + 后缀）—— 两条词条还是两段式，收成一条 %s 整句要改 seed。
	tr := shell.TranslateFor(c)
	response.SuccessWithMessage(c, tr(productenums.ProductPricingAppliedLead, "已应用调价：本次改动 ")+
		strconv.Itoa(res.ChangedCount)+tr(productenums.ProductPricingAppliedTail, " 个变体，明细见下方留痕台账。"))
}

// productPricingApplyReq 定价页应用调价的 JSON 入参（规则字段平铺，理由见 ApplyPricing）。
//
// 与 productBulkPricingReq 刻意分成两个类型：那个带 ids 且 scope/targetId 由 service
// 逐商品覆盖，这个由表单的 scope/targetId 决定范围。合成一个会让批量入口悄悄接受
// 「scope=filter」这种它并不支持的形状（值被忽略，用户以为生效了）。
type productPricingApplyReq struct {
	ProjectID  string `json:"projectId"`
	Note       string `json:"note"`
	RuleType   string `json:"ruleType"`
	Multiplier string `json:"multiplier"`
	Amount     string `json:"amount"`
	Margin     string `json:"margin"`
	Rounding   string `json:"rounding"`
	Scope      string `json:"scope"`
	TargetID   string `json:"targetId"`
	Status     string `json:"status"`
	Keyword    string `json:"keyword"`
	CategoryID string `json:"categoryId"`
	BrandID    string `json:"brandId"`
	TagID      string `json:"tagId"`
}

// BulkPricing 批量「按规则改价」（POST /api/product/bulk-pricing）。
//
// 权限点独立（product:bulk_pricing）：一个权限点只能绑一条 API 路由（sys_permission 上
// permission_code 唯一），复用 product:update 会让其中一条路径拿不到策略而 403。
//
// 规则参数**平铺在顶层**（ruleType / multiplier / amount / margin / rounding），不套一层 rule
// 对象：页面表单本来就是一个平铺表单（pricing_rule_fields.html），而「哪个规则用哪个参数」的
// 判定全在 pricingParamsFromForm —— 在这里另写一套装配，就会出现「抽屉填的 amount 被当成
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
