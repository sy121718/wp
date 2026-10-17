package producthttp

import (
	"errors"
	"fmt"
	"go_wp/internal/adminui"
	"go_wp/internal/middleware/builtin"
	presentationcontract "go_wp/internal/module/presentation/contract"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productservice "go_wp/internal/module/product/service"
	"go_wp/internal/shell"
	"go_wp/pkg/bulkids"
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/logger"
	"go_wp/pkg/pagination"
	"go_wp/pkg/response"
	"html"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

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
func (h *Handle) Create(c *gin.Context) {
	req := &productdto.CreateReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	// issue #19：变更记录的操作人从会话取（客户端传入被忽略）。
	req.OperatorID = operatorFromContext(c)
	res, err := h.svc.Create(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgCreateSuccess, res)
}

// Update 修改商品。
func (h *Handle) Update(c *gin.Context) {
	req := &productdto.UpdateReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	// issue #19：变更记录的操作人从会话取（客户端传入被忽略）。
	req.OperatorID = operatorFromContext(c)
	res, err := h.svc.Update(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgUpdateSuccess, res)
}

// Get 商品详情（含变体与价格区间）。
func (h *Handle) Get(c *gin.Context) {
	req := &productdto.GetReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.Get(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusNotFound, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDetailSuccess, res)
}

// List 商品列表。
func (h *Handle) List(c *gin.Context) {
	req := &productdto.ListReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	list, err := h.svc.List(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgListSuccess, list)
}

// Delete 删除商品。
func (h *Handle) Delete(c *gin.Context) {
	req := &productdto.DeleteReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	// issue #19：变更记录的操作人从会话取（客户端传入被忽略）。
	req.OperatorID = operatorFromContext(c)
	if err := h.svc.Delete(c.Request.Context(), req); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDeleteSuccess, nil)
}

// BulkDelete 批量删除商品（POST /api/product/bulk-delete）。
//
// 与页面侧 ProductsBulkDelete 共用**同一个 service 方法与同一个权限点**
// （permission.ProductDelete）：那条通道返回整页提示，这条返回 ui/json_write.js 基座读的
// JSON 回执。两条通道并存是过渡态 —— 页面迁完之后页面 POST 就该删掉，而不是留两条永远同值。
//
// 逐条原因在这里取词：service 只回业务 key（ReasonKey），成品句由 productErrText 按请求
// 语言给。取词只在 inbound 有，service 拿不到也不该拿。
func (h *Handle) BulkDelete(c *gin.Context) {
	req := &productdto.BulkDeleteReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	// 归一口径与表单侧（shell.BulkIDs）同一个纯函数：去空白 / 去重 / 上限 MaxIDs。
	// **超限整批拒绝，不静默截断** —— 截断会让操作者以为「勾的全删了」。
	ids, err := bulkids.Normalize(req.IDs)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	req.IDs = ids
	res, err := h.svc.BulkDeleteProducts(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDeleteSuccess, productBulkDeletedView(c, res))
}

// productBulkItemView 逐条回执的对外形状。键名与 json_write.js 的 failureLine 对齐：
// 它读 item.message / item.deleted / item.id，少一个就只剩「跳过 N 条」没有原因。
type productBulkItemView struct {
	ID string `json:"id"`
	// Deleted / Updated 二选一（与 dto.BulkItem 同义）：删除用前者、改价用后者，
	// 基座按 outcome 读对应的那个键。
	Deleted bool   `json:"deleted,omitempty"`
	Updated bool   `json:"updated,omitempty"`
	Message string `json:"message,omitempty"`
}

// productBulkResultView 批量删除的回执：两个计数给摘要句，items 给逐条原因。
type productBulkResultView struct {
	Deleted int                   `json:"deleted"`
	Skipped int                   `json:"skipped"`
	Items   []productBulkItemView `json:"items"`
}

// productBulkDeletedView 把 service 的计数 + 业务 key 转成基座要的形状。
//
// 成功键叫 deleted 而不是 succeeded：json_write.js 的 okKeyOf 按 outcome=delete 读
// data.deleted —— 对不上时摘要句恒为「已删除 0 项」，而删除其实成功了。
func productBulkDeletedView(c *gin.Context, res *productdto.BulkResult) productBulkResultView {
	view := productBulkResultView{
		Deleted: res.Succeeded,
		Skipped: res.NotDone(),
		Items:   make([]productBulkItemView, 0, len(res.Items)),
	}
	for _, it := range res.Items {
		row := productBulkItemView{ID: it.ID, Deleted: it.Deleted}
		if !it.Deleted && it.ReasonKey != "" {
			row.Message = productErrText(c, errors.New(it.ReasonKey))
		}
		view.Items = append(view.Items, row)
	}
	return view
}

// CreateVariant 新增变体（未填字段由商品级默认值补齐）。
func (h *Handle) CreateVariant(c *gin.Context) {
	req := &productdto.CreateVariantReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	// issue #19：变更记录的操作人从会话取（客户端传入被忽略）。
	req.OperatorID = operatorFromContext(c)
	res, err := h.svc.CreateVariant(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgCreateSuccess, res)
}

// UpdateVariant 修改变体（编辑路径不做默认值填充）。
func (h *Handle) UpdateVariant(c *gin.Context) {
	req := &productdto.UpdateVariantReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	// issue #19：变更记录的操作人从会话取（客户端传入被忽略）。
	req.OperatorID = operatorFromContext(c)
	res, err := h.svc.UpdateVariant(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgUpdateSuccess, res)
}

// DeleteVariant 删除变体。
func (h *Handle) DeleteVariant(c *gin.Context) {
	req := &productdto.DeleteVariantReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	// issue #19：变更记录的操作人从会话取（客户端传入被忽略）。
	req.OperatorID = operatorFromContext(c)
	if err := h.svc.DeleteVariant(c.Request.Context(), req); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDeleteSuccess, nil)
}

// GenerateVariants 按勾选的属性值生成全部变体组合（issue #8）。
//
// 未勾选任何属性值（或 selections 缺省）= 无表单路径：按商品全部参与变体的
// 属性组与其启用值生成；新变体逐字段继承商品级默认值。
func (h *Handle) GenerateVariants(c *gin.Context) {
	req := &productdto.GenerateVariantsReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	// issue #19：变更记录的操作人从会话取（客户端传入被忽略）。
	req.OperatorID = operatorFromContext(c)
	res, err := h.svc.GenerateVariants(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgVariantGenerateSuccess, res)
}

// 编辑路径的默认值语义由 service 保证：本层只做绑定与转发，不补字段。
var _ = http.StatusOK

// ==========================================================================
// 商品列表页：筛选、分页、行内状态与分类/标签单元格。
//
// 筛选条与分页是全后台通用形态（GET 表单 + select），回跳地址与提示页出口在 product_jump.go。

// productListPageTemplate 商品列表整页模板（失败页与成功页渲染**同一份**模板）。
//
// 本节起是原先的 product_list.go（列表页）。
// ==========================================================================
const productListPageTemplate = "admin/product/products.html"

// ProductsPage 商品管理页：工程切换 + 商品列表 + 每个商品的变体面板 + 内联新建表单。
func (h *productPageHandle) ProductsPage(c *gin.Context) {
	ctx := c.Request.Context()
	// 取数失败**不在这里出口**：整页 GET 只有一条渲染路径（函数末尾那次 c.HTML），
	// 失败页与成功页渲染同一份模板、同一份页壳数据，差别只有状态码 / no-store / 失败槽
	//（契约见 docs/rules/template-boundary.md §整页 GET 的失败出口）。
	//
	// 每个失败点各写一遍「渲染失败页」等于每页各有一份「失败页长什么样」的实现，
	// 改一处必然漏一处；这里只记第一处失败的原因，末尾统一出口。
	// httpStatus 与下面的筛选状态（status 查询串）不是一回事，名字分开避免撞车。
	// 读模型在装配期注入（见 productPageHandle.reader）：漏接 = 编译错误，
	// 不再有「请求期断言失败 → 500 兜底」这条路径。
	reader := h.reader
	// 取参数 → 调读模型（取数编排在 service：取哪些数据、按什么顺序、哪条失败算 400 /
	// 哪条只降级，都是业务结论。见 service.ProductListPage）。
	// 页码归一用跨页共用的 productPageNumber（属性页 / 分类标签页同口径）。
	view, err := reader.ProductListPage(ctx, &productdto.ProductListPageReq{
		ProjectID: strings.TrimSpace(c.Query("project")),
		Keyword:   strings.TrimSpace(c.Query("keyword")),
		Status:    strings.TrimSpace(c.Query("status")),
		Page:      productPageNumber(c.Query("page")),
	})
	if view == nil {
		// 契约上视图恒非 nil（失败也要给齐模板必需键，见 contract.ProductPageReader）：
		// 真出现 nil 时兜一个空视图，而不是在这里 nil 下去 —— 缺键会让整页在那一行中断。
		view = &productdto.ProductListPageView{
			Title: MsgProductsTitle, Menu: "products", Limit: productListPageSize,
			HTTPStatus: http.StatusInternalServerError,
		}
	}
	// 上面的取数编排（分类树 / 品牌 / 标签 / 仓库清单 / 列表与总数 / 逐行详情 / 属性组选项 /
	// 每仓候选 / 定价规则清单）全在 service 的读模型里；这里只做控制器负责的派生值与出口。
	// —— 控制器负责的派生值 ——
	// 页壳键：title 的 i18n key 常量在本包（MsgProductsTitle 是迁移 163 seed 的词条名）。
	view.Title = MsgProductsTitle
	view.Menu = "products"
	// 状态下拉与改价表单初值：两处的工具都被别的页面用着（详情页回显 / 独立定价页），
	// 所以留在本包。分类扁平行与成品行**不在控制器**了 —— 两者只服务行装配，已随行下沉
	// 进读模型（service/product_row.go），本页拿到的是排好序的 view.Products。
	view.Statuses = productStatusOptions(view.FilterStatus)
	view.Form = productservice.DefaultPricingForm(view.Rules, view.Roundings)
	// 分页条：Go 只交事实（总数 / 页码 / 每页条数 / 基地址），文案、页码窗口与链接由
	// admin/partials/pagination.html 渲染。基地址带当前筛选条件，翻页不丢条件。
	//
	// **失败页仍会注入分页事实**：total 在 List/CountProducts 之后就已确定，若随后逐行取详情
	// 失败，total 仍 >0，于是失败页拿到与成功页同一份分页事实 —— 这是迁移前的既有行为
	//（旧 BuildPagination 拿到的就是同一个 total），本批**不改**它。
	// 实测（task-109）：失败页**看不到**翻页入口 —— products.html 的分支是
	// `{{if loadError}} 失败槽 {{else}} …含分页条… {{end}}`，失败分支不 include 分页片段；
	// 注入的事实因此不被渲染。product_pagination_facts_test.go 把这两条同时钉住。
	if pg := pagination.Facts(view.Total, view.Page, view.Limit); pg.HasPagination() {
		pg.Base = productListFilterURL(view.SelectedProject, view.FilterKeyword, view.FilterStatus)
		view.Pagination = pg
	}
	// 写动作的结论不在本页回显（走提示页渲染，见 product_jump.go），所以模板里的
	// ?err= / ?done= 提示条已整批删除。ListQuery 是筛选上下文（表单 action 的 query）：
	// 写动作失败时由 pkg/backpath 读回。
	view.ListQuery = productQueryFromRequest(c, productListBackKeys...)

	if err != nil {
		// 视图已带 LoadFailed / HTTPStatus（400 参数不合法 / 500 取数失败）。
		// 这里只补**当前语言**的失败槽文案：原始的 err 只进日志。
		view.LoadFailed = true
		view.LoadError = productErrPageText(c, err)
	}
	// 出口只有这一处（页面成败都走它）：档位与 no-store 由视图带出（PageStatus / PageNoStore）。
	adminui.RenderPage(c, productListPageTemplate, view)
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
func productPageNumber(raw string) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 1 {
		return 1
	}
	return n
}

// productListFilterURL 列表页的筛选基地址（**不含** page/limit：分页组件自己拼）。
//
// 分页链接由 pkg/pagination.BuildPagination 在这个基地址后追加 page/limit —— 把筛选条件
// 拼进基地址，翻页时关键词与状态才不会丢；反过来说，若基地址里塞了 page，
// 就会出现两个 page 参数，浏览器取第一个，翻页看起来「点了没反应」。
func productListFilterURL(projectID, keyword, status string) string {
	q := url.Values{}
	if p := strings.TrimSpace(projectID); p != "" {
		q.Set("project", p)
	}
	if k := strings.TrimSpace(keyword); k != "" {
		q.Set("keyword", k)
	}
	if s := strings.TrimSpace(status); s != "" {
		q.Set("status", s)
	}
	if enc := q.Encode(); enc != "" {
		return "/admin/products?" + enc
	}
	return "/admin/products"
}

// productStatusOptions 列表筛选 / 编辑页的状态下拉项（含「全部状态」）。
//
// 槽位收**事实**（Key + 中文兜底）而不是成品串：语言是请求级的，控制器提前翻等于把这一句
// 锁死在一种语言（英文界面上仍是中文，不报错、门禁也不红）。取词由模板的 tr 在渲染期做
// （products.html 的筛选项与状态徽章、product_edit.html 的状态下拉）。
//
// 「值 → (key, 兜底)」的映射在 productenums.StatusLabel：本函数只给**值域与顺序**
// （这一屏就这四档），控制器不再自带一张手抄的三元表。
//
// 状态维度**只有这一种入口**：不另配状态徽章筛选 —— 同一维度两套控件时，
// 用户会怀疑它们是否等价（admin-ui-logic §2）。
func productStatusOptions(current string) []productdto.StatusOptionView {
	values := []string{"", productenums.StatusDraft, productenums.StatusPublished, productenums.StatusArchived}
	out := make([]productdto.StatusOptionView, 0, len(values))
	for _, value := range values {
		key, fallback := productenums.StatusLabel(value)
		out = append(out, productdto.StatusOptionView{
			Value:         value,
			LabelKey:      key,
			LabelFallback: fallback,
			Selected:      value == strings.TrimSpace(current),
		})
	}
	return out
}

// ==========================================================================
// 商品新建页与它的表单契约。
//
// 回填字段清单就是 dto.ProductCreateFormEcho 结构体本身（不再是一份手写的 []string），
// 与模板的正反双向断言见 product_create_form_fail_test.go：模板读了结构体没有的字段不是
// 「页面截断」而是渲染失败 → htmx 不 swap（用户点了保存什么都看不到）。

// productNewPageTemplate 新建商品整页模板（失败页与成功页渲染**同一份**模板）。
// 左侧基础信息表单（字段名与原抽屉逐字一致，复用 /admin/products/create），
// 右侧模板卡片区（默认模板 + 候选列表 + 预览入口）；模板绑定与可视化自定义
// 在创建后的详情页继续（create 不落实例，绑定发生在首次发布）。

// —— 商品建表单的失败分档出口（渐进增强；模板见 admin/product/product_create_form.html）——
//
// 本节起是原先的 product_create.go（新建页与它的表单字段协议）。
// ==========================================================================
const productNewPageTemplate = "admin/product/products_new.html"

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
//	· 状态码与 no-store 由视图带出来（adminui.PageResult），不再在这里判档，
//	  于是「失败页长什么样」不再有第二种写法；
//	· 失败槽文案（LoadError）仍在这里取词：取词是**请求级**的（按当前语言），
//	  交给 service 就等于把语言钉进业务层。
func (h *productPageHandle) ProductNewPage(c *gin.Context) {
	// 读模型在装配期注入（见 productPageHandle.reader）：漏接 = 编译错误，
	// 不再有「请求期断言失败 → 500 兜底」这条路径。
	reader := h.reader
	req := &productdto.ProductNewPageReq{ProjectID: strings.TrimSpace(c.Query("project"))}
	view, err := reader.ProductNewPage(c.Request.Context(), req)
	if view == nil {
		// 契约上视图恒非 nil（失败也要给齐模板必需键，见 contract.ProductPageReader）：
		// 真出现 nil 时兜一个空视图，而不是在这里 nil 下去 —— 缺键会让整页在那一行中断。
		view = &productdto.ProductNewPageView{
			Title: productenums.ProductsCreateSubmit, Menu: "products", HTTPStatus: http.StatusInternalServerError,
		}
	}
	if err != nil {
		// 视图已带 LoadFailed / HTTPStatus（400 参数不合法 / 500 取数失败）。
		// 这里只补**当前语言**的失败槽文案：原始的 err 只进日志。
		view.LoadFailed = true
		view.LoadError = productErrPageText(c, err)
	}
	adminui.RenderPage(c, productNewPageTemplate, view)
}

// productCreateFormTemplate 商品建表单模板（**不带页壳**：它被整页 include，也被单独渲染）。
const productCreateFormTemplate = "admin/product/product_create_form.html"

// productCreateFormView 建表单的渲染视图：取数来自读模型（service.ProductCreateForm），
// 回填值是**请求边界**的产物（本次提交了什么），所以在这里填 —— service 不认识 *gin.Context。
//
// 回填的字段清单就是 dto.ProductCreateFormEcho 那个结构体（原来是一份手写的 13 项 []string）：
// `adminui.FillForm` 按字段名 / `form:"名字"` 标签从**这次请求的表单值**里填，漏字段在编译期
// 就不存在。这里喂的是 formEchoFrom 归一化后的值（去首尾空白、丢空项），而不是 c.Request.PostForm
// 原样 —— 与各抽屉的回填口径保持同一份（否则同一段文字在不同表单里回填出的结果不同）。
//
// 表单门控：走到这里就是「回填表单」，所以恒给一个非 nil 的 echo —— 模板靠 `isset(.FormEcho)`
// 区分空表单与回填表单，门控契约见 dto.ProductCreateFormView。
func (h *productPageHandle) productCreateFormView(c *gin.Context, projectID, submitErr string) (*productdto.ProductCreateFormView, error) {
	// 读模型在装配期注入（见 productPageHandle.reader）：漏接 = 编译错误，
	// 不再有「请求期断言失败 → 500 兜底」这条路径。
	reader := h.reader
	view, err := reader.ProductCreateForm(c.Request.Context(), &productdto.ProductCreateFormReq{
		ProjectID: strings.TrimSpace(projectID),
	})
	if err != nil {
		return view, err
	}
	if view == nil {
		view = &productdto.ProductCreateFormView{}
	}
	view.SubmitErr = submitErr
	echo := &productdto.ProductCreateFormEcho{}
	adminui.FillForm(echo, formEchoFrom(c).values)
	view.FormEcho = echo
	return view, nil
}

// productCreateFail 商品建表单的失败出口（分档口径唯一，别在调用点各写一份头判断）。
//
//	· htmx 请求：200 + 回填后的表单 HTML —— 整页原地留住已填内容；
//	· 原生请求：整页提示（shell.RenderJump）回本页（无 JS 环境：不跳走、错误可读）。
//
// **成功路径也走同一条出口**（ProductsCreate 用 productEditJump / productListJump）。
func (h *productPageHandle) productCreateFail(c *gin.Context, projectID string, msg productMsg) {
	if !isHXRequest(c) {
		// 原生档：整页提示（取代原先的 302 + ?err= 回本页）。无 JS 环境下不跳走、错误可读。
		productNewJump(c, false, projectID, msg)
		return
	}
	// 失败槽位还没取词化（模板直接输出字符串）：这里取一次文本，报告里已登记。
	view, err := h.productCreateFormView(c, projectID, productMsgText(c, msg))
	if err != nil {
		// 回填表单取数再失败：退回整页提示（错误仍可读），不把半个表单当成功返回。
		logger.Scene("product").With("project", projectID).Error(err, "商品新建失败后重建表单视图失败，退回提示页")
		productNewJump(c, false, projectID, msg)
		return
	}
	adminui.RenderPage(c, productCreateFormTemplate, view)
}

// ProductsCreate 新建商品。
//
// 两条本批确定的语义：
//
//  1. **type 必须读**（迁移 238 的商品类型）：字段一旦漏读，service 会把空串兜底成
//     variant —— 建「捆绑容器」得到的是一个带自己首个变体的常规商品，而且返回 302 成功、
//     列表页也看得见，不留任何错误线索。
//  2. **成功去商品详情页**，不是回列表：抽屉只有名称 / 类型 / URL 段 / 默认价 / 归属仓 /
//     属性组几个字段，而 CreateReq 有 40+ 个字段（描述、图片、分类、品牌、标签、SEO…），
//     商品详情页才是全量录入的地方。bundle 更明显 —— 它没有变体，回列表看不出任何进展。
//     失败仍回新建页（表单所在处）并渲染整页提示：用户就在那里重填。
func (h *productPageHandle) ProductsCreate(c *gin.Context) {
	req := &productdto.CreateReq{
		ProjectID: c.PostForm("projectId"),
		Name:      c.PostForm("name"),
		// 商品类型：variant（常规变体商品）｜bundle（捆绑容器，只有容器价）。
		Type: strings.TrimSpace(c.PostForm("type")),
		Slug: c.PostForm("slug"),
		// 主体 SKU 编码（表单 name=sku，规则见 docs/14）：留空即派生（变体商品取 URL 段、
		// 捆绑取 <商品段>_B）；填了且选了仓库 → 变体商品自动加仓库码前缀。
		// **必须透传**：漏读等于运营填了个寂寞 —— 服务端会静默按 URL 段派生另一个编码，
		// 建完看不出任何异常，直到与仓库对不上号。
		SKUCode: strings.TrimSpace(c.PostForm("sku")),
		// SKU 来源（docs/14 §1.1 的两条入口，迁移 251）：自己创建 / 从仓库选。
		// **必须透传**：漏读就等于运营在抽屉里选了「从仓库选」却拿到一个按 URL 段
		// 派生的商品，建完看不出任何异常 —— 与上面 sku 漏读是同一类静默失败。
		SKUSource:    strings.TrimSpace(c.PostForm("skuSource")),
		WarehouseSKU: strings.TrimSpace(c.PostForm("warehouseSku")),
		ExternalSKU:  strings.TrimSpace(c.PostForm("externalSku")),
		// 属性组：抽屉是勾选列表（同名多值），保留了逗号分隔单值的兼容形态。
		AttributeIDs: attributeIDsFromForm(c),
		// 归属仓：空值即兜底该工程的默认仓（单值形态保留，接口 / 脚本路径仍可用）。
		WarehouseID: strings.TrimSpace(c.PostForm("warehouseId")),
		// 多仓勾选（2026-09-19 口径）：勾了哪些仓就在哪些仓各建一行（裸码），
		// 第一个仓是**认领仓**（决定主体 SKU 的仓码前缀）。同名多值 → PostFormArray，
		// 一个都没勾时浏览器不提交这个字段（服务端按「默认仓」处理）。
		WarehouseIDs: c.PostFormArray("warehouseIds"),
	}
	// 默认价：空串 = 本次不填；非法数字**按未填处理**（既有语义，不改 —— 新建抽屉只填
	// 名称 / 类型 / URL 段 / 价格 / 归属仓，service 仍会按「没给价」给出它自己的结论）。
	// NaN / 正负无穷归入非法（dto.OptFloat 的契约）：不是数字的值进了金额运算会污染整批数据。
	if v, perr := productdto.OptFloat("defaultPrice", c.PostForm("defaultPrice")); perr == nil && v != nil {
		req.DefaultPrice = v
	}
	// 数量（迁移 261 的口径）：**不填数量 = 不跟踪 = 无限**。模板是一个「跟踪数量」开关
	// 加一个数量框：开关没勾时数量框禁用、不提交 ⇒ 这里读不到值 ⇒ Quantity 保持 nil。
	// 开关勾了而数量框留空 ⇒ 写 0（0 是「明确没货」，与「没填」是两回事）。
	// 数量（迁移 261 的口径）：开关与数量框的**同组绑定**语义收在 dto.TrackedQuantity ——
	// 没勾开关 = 不跟踪 = 无限（nil）；勾了却留空 = 明确 0；非法 / 负数 = 绑定期错误。
	if qty, qerr := productdto.TrackedQuantity(c.PostForm("trackQuantity"), c.PostForm("quantity")); qerr != nil {
		// 数量非法也是**表单失败**：htmx 档留在原页（错误槽 + 回填），不能悄悄跳走 ——
		// 用户为这一张表填了十几行内容，为一个数字问题全丢是最贵的一种失败。
		h.productCreateFail(c, req.ProjectID, productQuantityInvalidMsg)
		return
	} else {
		req.Quantity = qty
	}
	created, err := h.products.Create(c.Request.Context(), req)
	if err != nil {
		// 业务错误的 Error() 是 enums 常量（= i18n key），直接铺到页面上就是
		// 「列表页显示 ErrBundlePriceRequired」的来源：统一经 productErrText 取词。
		// 出口走分档：htmx 档 200 + 回填片段，原生档渲染整页提示（见 productCreateFail）。
		h.productCreateFail(c, req.ProjectID, productErrMsg(c, err))
		return
	}
	if created != nil && created.ID != "" {
		// 成功也走同一条出口：htmx 由 shell.RenderJump 输出 HX-Redirect（整页跳转），
		// 原生渲染提示页 —— 取代原先的 redirectWhere。
		productEditJump(c, true, req.ProjectID, created.ID, productActionDoneMsg)
		return
	}
	// 兜底：拿到商品 id 才谈得上「进详情继续编辑」，否则回列表而不是构造一个空详情页。
	productListJump(c, true, productActionDoneMsg)
}

// ==========================================================================
// 商品编辑页（整页 + 回显 + 校验失败原地留住输入）。
//
// 表单带原生 method/action（无 JS 也能提交），另加 hx-post；失败时服务端把刚提交的值
// 原样送回（formEcho 一族在 product_page_util.go），用户的输入比错误文案贵。

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

// productEditPageTemplate 商品编辑整页模板（失败页与成功页渲染**同一份**模板）。
const productEditPageTemplate = "admin/product/product_edit.html"

func (h *productPageHandle) ProductEditPage(c *gin.Context) {
	h.renderProductEditPage(c, strings.TrimSpace(c.Query("project")), strings.TrimSpace(c.Query("product")), "", false)
}

func (h *productPageHandle) renderProductEditPage(c *gin.Context, selected, productID, submitErr string, echo bool) {
	ctx := c.Request.Context()
	// 读模型在装配期注入（见 productPageHandle.reader）：漏接 = 编译错误，
	// 不再有「请求期断言失败 → 500 兜底」这条路径。
	reader := h.reader
	// 取数编排（工程 / 参数档 / 分类 / 品牌 / 标签 / 仓库 / 商品详情 / 属性组候选 + 三档判据）
	// 全在 service 的读模型里；这里只剩控制器负责的派生值、**表单回显**与出口。
	view, err := reader.ProductEditPage(ctx, &productdto.ProductEditPageReq{
		ProjectID: selected, ProductID: productID,
	})
	if view == nil {
		// 契约上视图恒非 nil（失败也要给齐模板必需键）：真出现 nil 时兜一个空视图。
		view = &productdto.ProductEditPageView{
			Title: MsgProductsTitle, Menu: "products", HTTPStatus: http.StatusInternalServerError,
		}
	}
	view.Title = MsgProductsTitle
	view.Menu = "products"
	// 写动作的结论不在本页回显（走提示页，见 product_jump.go）；只剩**原地重渲染**这一条路
	// 会给 Err（表单校验失败 / 选项读取失败）。
	view.Err = submitErr
	// 回列表的链接与「取消」都回到用户来的地方（工程上下文保留）。
	view.BackURL = productListFilterURL(view.SelectedProject, "", "")
	// 属性组候选读失败：撤掉保存入口并按当前语言归口 —— 「选择器缺失」与「用户主动取消了
	// 全部引用」在 POST 上同形，读故障时不能装作后者。
	if view.AttributeOptionsErr != nil {
		view.EditBlocked = true
		if view.Err == "" {
			view.Err = productInternalText(c, view.AttributeOptionsErr)
		}
	}
	// 状态下拉：先按「全部状态」那一档给默认值 —— 商品读不出来时页面仍完整；读到了会覆盖。
	view.Statuses = productStatusOptions("")
	if err == nil && view.HasProduct && view.Detail != nil {
		// 行已由读模型装配（service/product_row.go）：副标题 / 单位 / 描述 HTML 都是行上的
		// 字段，控制器不再往行里补键 —— 这里只剩「编辑表单要、列表与详情页不要」的那几样。
		view.Statuses = productStatusOptions(view.Detail.Status)
		// 属性组勾选态（详情页用的是逗号分隔的 id 输入框，编辑页是勾选列表）：
		// 可选值来自读模型给的候选，勾选态由商品已引用的 id 决定。
		// 候选读失败那一档走上面的 EditBlocked（AttributeChecks 保持为空）。
		if view.AttributeOptionsErr == nil {
			view.AttributeChecks = checkedAttributeOptions(view.AttributeOptions, view.Detail.AttributeIDs)
		}
		// 图集与数值字段：表单里是文本（textarea / input），按行与可空文本回填。
		view.ImagesText = strings.Join(view.Detail.Images, "\n")
		view.ImageAltsText = strings.Join(view.Detail.ImageAlts, "\n")
		view.WeightText = nullableNumberText(view.Detail.Weight)
		view.DefaultPriceText = nullableNumberText(view.Detail.DefaultPrice)
		// 表单回显：校验失败重渲染时用**这次提交的值**覆盖上面读回来的值。
		// 提交值只在请求体里，所以这一整段是控制器的事（service 不认识 gin.Context）。
		if echo {
			form := formEchoFrom(c)
			// 文本字段逐项回填到行字段：行已是结构体，字段名由编译器对账，不再走
			// 「表单名 → 行键」的映射表（改名会静默丢回显，且不报错）。
			view.Product.Name = form.value("name")
			view.Product.Subtitle = form.value("subtitle")
			view.Product.Slug = form.value("slug")
			view.Product.SKUCode = form.value("sku")
			view.Product.Unit = form.value("unit")
			view.ImagesText = c.PostForm("images")
			view.ImageAltsText = c.PostForm("imageAlts")
			view.WeightText = c.PostForm("weight")
			view.DefaultPriceText = c.PostForm("defaultPrice")
			view.Statuses = productStatusOptions(form.value("status"))
			// 勾选态回填的**唯一**入口（见上）：一个都不给时按「已选中」渲染，
			// 所以没提交参数时必须在这里挡掉。
			//
			// 属性勾选清单是具体类型 []productdto.AttributeCheckOptionView，
			// `.([]gin.H)` / `.([]map[string]any)` 断言都会失败（ok=false）——
			// 直接用值回填的选项清单重算，不要在已勾选的视图上改。
			view.AttributeChecks = checkedAttributeOptions(view.AttributeOptions, form.list("attributeIds"))
			view.Product.CategoryChecks = checkedProductEditOptions(view.Product.CategoryChecks, form.list("categoryIds"))
			view.Product.TagChecks = checkedProductEditOptions(view.Product.TagChecks, form.list("tagIds"))
			view.Product.PrimaryOptions = selectedProductEditOptions(view.Product.PrimaryOptions, form.value("primaryCategoryId"))
			view.Product.BrandOptions = selectedProductEditOptions(view.Product.BrandOptions, form.value("brandId"))
		}

		// 捆绑容器：构成表在详情页只读展示，编辑入口在独立页 /admin/products/bundle ——
		// 这里只给一个链接标记（type=variant 时为假，模板据 isset 整块跳过）。
		view.IsBundle = isBundleProduct(view.Detail.Type)
	}
	// 详情页模板面板：详情页只读展示绑定状态，写动作（进入自定义 / 编辑模板 / 重新套用预设 /
	// 回滚）都在本页 —— 与详情页共用同一份组装。
	view.TplPanel = h.detailTemplatePanel(ctx, view.SelectedProject, view.ProductID)
	if err != nil {
		// 视图已带 LoadFailed / HTTPStatus；这里只补**当前语言**的失败槽文案：
		// 驱动原文与原始错误只进日志。
		view.LoadFailed = true
		view.LoadError = productErrPageText(c, err)
	}
	// 出口只有这一处（页面成败都走它）：档位与 no-store 由视图带出（PageStatus / PageNoStore）。
	adminui.RenderPage(c, productEditPageTemplate, view)
}

func checkedProductEditOptions(options []map[string]any, selected []string) []map[string]any {
	for _, option := range options {
		id, _ := option["ID"].(string)
		option["Checked"] = containsString(selected, id)
	}
	return options
}

func selectedProductEditOptions(options []map[string]any, selected string) []map[string]any {
	for _, option := range options {
		id, _ := option["ID"].(string)
		option["Selected"] = id == selected
	}
	return options
}

// nullableNumberText 可空数值 → 输入框文本（未设置给空串，不是 0）。
//
// 与列表展示用的 formatNullableAmount（空显示为 —）分开：— 是**展示**符号，
// 填进 input 的 value 会变成一个待提交的非法值。
func nullableNumberText(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(*v, 'f', -1, 64)
}

// checkedAttributeOptions 属性组勾选列表：给每组补一个 Checked（商品是否引用它）。
//
// 内嵌 AttributeOptionView 而不是复制键：标签口径仍只有 service 一处，
// 这里只加「这一行在本次商品上是什么状态」。
func checkedAttributeOptions(options []productdto.AttributeOptionView, selected []string) []productdto.AttributeCheckOptionView {
	out := make([]productdto.AttributeCheckOptionView, 0, len(options))
	for _, o := range options {
		out = append(out, productdto.AttributeCheckOptionView{
			AttributeOptionView: o,
			Checked:             containsString(selected, o.ID),
		})
	}
	return out
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
// productDetailPageTemplate 商品详情整页模板（失败页与成功页渲染**同一份**模板）。
//
// 本节起是原先的 product_detail_page.go（详情页）。
// ==========================================================================
const productDetailPageTemplate = "admin/product/product_detail.html"

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
func (h *productPageHandle) ProductDetailPage(c *gin.Context) {
	ctx := c.Request.Context()
	// 读模型在装配期注入（见 productPageHandle.reader）：漏接 = 编译错误，
	// 不再有「请求期断言失败 → 500 兜底」这条路径。
	reader := h.reader
	view, err := reader.ProductDetailPage(ctx, &productdto.ProductDetailPageReq{
		ProjectID: strings.TrimSpace(c.Query("project")),
		ProductID: strings.TrimSpace(c.Query("product")),
	})
	if view == nil {
		// 契约上视图恒非 nil（失败也要给齐模板必需键）：真出现 nil 时兜一个空视图。
		view = &productdto.ProductDetailPageView{
			Title: productenums.ProductDetailTitle, Menu: "products",
			HTTPStatus: http.StatusInternalServerError,
		}
	}
	// 仓库清单 / 商品详情 / 分类树 / 品牌 / 标签的取数编排全在 service 的读模型里
	//（service.ProductDetailPage，含 404 / 400 / 500 三档）；这里只做控制器负责的派生值与出口。
	// —— 控制器负责的派生值 ——
	view.Title = productenums.ProductDetailTitle
	view.Menu = "products"
	// 返回列表带上工程上下文：回到列表时不会掉回默认工程。
	view.BackURL = "/admin/products?project=" + view.SelectedProject
	// 行已由读模型装配（service/product_row.go）—— 控制器只补详情页独有的面板；
	// 「取数失败时不装配」由读模型自己保证（HasProduct / Detail 为假时它不建行）。
	if err == nil && view.HasProduct && view.Detail != nil && view.Product != nil {
		// 捆绑容器（type=bundle）在详情页多一块「捆绑构成」编辑区：成员只作为选项与履约明细，
		// 容器价才是套餐价。type=variant 时**不给这个字段**，模板据 isset 整块跳过
		// —— 区块的入口就在详情页，不该再要求运营去另一个菜单页找它。
		// （Bundle 保持可 nil：nil 才让 isset(p.Bundle) 为假、整块跳过。）
		if isBundleProduct(view.Detail.Type) {
			view.Product.Bundle = h.bundlePanel(c, view.SelectedProject, view.Detail.ID)
		}
	}

	// 详情页模板面板：详情页只读展示绑定状态与预览入口，编辑动作（进入自定义 / 编辑模板 /
	// 重新套用预设 / 回滚）在编辑页 —— 两页共用 detailTemplatePanel 这一份组装。
	view.TplPanel = h.detailTemplatePanel(ctx, view.SelectedProject, view.ProductID)
	// 出口只有这一处（页面成败都走它）：档位与 no-store 由视图带出（PageStatus / PageNoStore，
	// 非 200 一律 no-store）。「不存在」那一档没有失败槽 —— 模板自己的 .HasProduct=false
	// 空态就是它的文案（读到了、库里没有这一条），但它与「没读到」一样 no-store：
	// 重试必须真的回到服务端确认商品是否又出现了。
	if err != nil {
		view.LoadFailed = true
		view.LoadError = productErrPageText(c, err)
	}
	adminui.RenderPage(c, productDetailPageTemplate, view)
}

// variantSelectionFromForm 收「生成组合」表单里按前缀提交的勾选（attr:<属性组 id> → 值 id）。
//
// 生成（落库路径）、预览（不落库）两条 handler 共用它：字段名与收拢规则只有一份，
// 两处各写一遍必然分叉（一个认 attr: 前缀、另一个漏读，表现为「预览有行、保存却没数据」）。
func variantSelectionFromForm(c *gin.Context) []productdto.VariantSelectionReq {
	_ = c.Request.ParseForm()
	keys := make([]string, 0, len(c.Request.PostForm))
	for key := range c.Request.PostForm {
		if strings.HasPrefix(key, variantSelectionPrefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	out := make([]productdto.VariantSelectionReq, 0, len(keys))
	for _, key := range keys {
		attrID := strings.TrimPrefix(key, variantSelectionPrefix)
		if attrID == "" {
			continue
		}
		out = append(out, productdto.VariantSelectionReq{
			AttributeID: attrID, ValueIDs: c.Request.PostForm[key],
		})
	}
	return out
}

// ProductsVariantGenerate 按勾选的属性值批量生成变体组合（issue #8）。
//
// mode=all：不勾选任何值，按商品全部参与变体的属性组 × 全部启用值生成
// （service 的无表单路径，导入 / 接口走同一条）。
// 其它情况按勾选生成；一个都没勾选时直接退回并提示 —— 不静默退化成「全部生成」，
// 那是最容易一次误造出上百个变体的路径。
func (h *productPageHandle) ProductsVariantGenerate(c *gin.Context) {
	projectID := c.PostForm("projectId")
	productID := c.PostForm("productId")
	req := &productdto.GenerateVariantsReq{
		ProductID:   productID,
		WarehouseID: strings.TrimSpace(c.PostForm("warehouseId")),
	}
	if strings.TrimSpace(c.PostForm("mode")) != "all" {
		req.Selections = variantSelectionFromForm(c)
		if len(req.Selections) == 0 {
			// 裸 enums key 铺到页面上只会显示 ErrVariationSelectionEmpty —— 交事实（key + 中文兜底），
			// 由提示页在渲染期取词。
			productEditJump(c, false, projectID, productID,
				productErrMsg(c, errors.New(productenums.ErrVariationSelectionEmpty)))
			return
		}
	}
	if _, err := h.products.GenerateVariants(c.Request.Context(), req); err != nil {
		// 非业务错误的原文（PG / 构建器）只进日志，对外给归口文案。
		productEditJump(c, false, projectID, productID, productErrMsg(c, err))
		return
	}
	productEditJump(c, true, projectID, productID, productActionDoneMsg)
}

// variantPreviewRow 预览行的 JSON 形状：前端拿它渲染清单里的一行。
//
// Spec 是**服务端**拼的可读规格文本（与详情页表格同一份 specLabel），
// 前端不自己拼属性名 —— 属性值改名后前端那份就会显示旧名字。
type variantPreviewRow struct {
	SKUCode      string          `json:"skuCode"`
	OptionValues json.RawMessage `json:"optionValues"`
	OptionKey    string          `json:"optionKey"`
	Spec         string          `json:"spec"`
}

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
func (h *productPageHandle) ProductsVariantPreview(c *gin.Context) {
	ctx := c.Request.Context()
	productID := strings.TrimSpace(c.PostForm("productId"))
	req := &productdto.PreviewVariantReq{
		ProductID:   productID,
		ProjectID:   strings.TrimSpace(c.PostForm("projectId")),
		WarehouseID: strings.TrimSpace(c.PostForm("warehouseId")),
		Selections:  variantSelectionFromForm(c),
	}
	// 清单里已有的组合原样回传（每行一条）：由服务端归一去重，前端不需要懂 optionKey。
	for _, raw := range c.PostFormArray("optionValues") {
		if trimmed := strings.TrimSpace(raw); trimmed != "" {
			req.ExistingOptionValues = append(req.ExistingOptionValues, json.RawMessage(trimmed))
		}
	}
	res, err := h.products.PreviewVariantCombinations(ctx, req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "message": productErrText(c, err)})
		return
	}
	// 取属性组那一跳带作用域（req.ProjectID 上面已从表单读出）：多工程部署下不带它
	// 会被 product 侧的 resolveProjectID 拒掉，attrs 退化为空 —— derr 被刻意忽略，
	// 于是 spec 静默降级成 optionKey（「属性组：值」标签全丢），页面上看不出报错。
	attrs := []*productdto.AttributeResp{}
	if detail, derr := h.products.Get(ctx, &productdto.GetReq{ID: productID, ProjectID: req.ProjectID}); derr == nil && detail != nil {
		attrs = detail.Attributes
	}
	rows := make([]variantPreviewRow, 0, len(res.Rows))
	for _, row := range res.Rows {
		if row == nil {
			continue
		}
		rows = append(rows, variantPreviewRow{
			SKUCode: row.SKUCode, OptionValues: row.OptionValues,
			OptionKey: row.OptionKey, Spec: productservice.SpecLabel(row.OptionValues, attrs),
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"ok": true, "total": res.Total, "skipped": res.Skipped, "rows": rows,
	})
}

// ProductsVariantSave 以清单为准保存变体（POST /admin/products/variant/save）——**唯一的落库动作**。
//
// 表单里的 rows 是一段 JSON（前端清单的全部行，顺序即期望顺序）：行上只有
// variantId（既有变体；新增行为空）、可编辑的 skuCode 与 optionValues。
// 服务端逐条重算（见 service.SaveVariantList），前端提交的形状一律不作数。
//
// 结果由提示页渲染（shell.RenderJump）回编辑页：成功含「新增 / 改 SKU / 删除」与逐条跳过原因，
// 失败给白名单文案 —— 与编辑页其它写动作同一套出口，不新增渲染通道。
func (h *productPageHandle) ProductsVariantSave(c *gin.Context) {
	ctx := c.Request.Context()
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	productID := strings.TrimSpace(c.PostForm("productId"))
	req := &productdto.SaveVariantListReq{
		ProductID:   productID,
		ProjectID:   projectID,
		WarehouseID: strings.TrimSpace(c.PostForm("warehouseId")),
		// 操作人取自会话，表单字段不作数（变体留痕的操作人不可伪造）。
		OperatorID: builtin.CurrentUserIDText(c),
	}
	if raw := strings.TrimSpace(c.PostForm("rows")); raw != "" {
		if uerr := json.Unmarshal([]byte(raw), &req.Rows); uerr != nil {
			// 前端拼错了 JSON：这属于调用方参数错误，给可读提示而不是 PG / JSON 原始报错。
			productEditJump(c, false, projectID, productID, productErrMsg(c, errors.New(productenums.ErrInvalidParam)))
			return
		}
	}
	// 这里不再预取商品详情（只为拼「哪个 SKU 被跳过」）：回带文案只报原因枚举，
	// 逐行 SKU 由详情页本身呈现（见 variantSaveNotice 的说明），因此少一次 DB 往返。
	res, err := h.products.SaveVariantList(ctx, req)
	if err != nil {
		productEditJump(c, false, projectID, productID, productErrMsg(c, err))
		return
	}
	productEditJump(c, true, projectID, productID, variantSaveMsg(c, res))
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

// variantSaveMsg 保存结果的回带结论（**事实**：单一情形给 key，多段拼接给成品串）。
//
// 「一条都没变」与「跳过若干」必须能区分：都写成「保存成功」时，用户无法判断是
// 清单本来就与库里一致，还是有行被拦下了（被拦下的行下一次还会出现在清单里）。
//
// 带跳过明细时是「两句词条 + 逐条原因」拼起来的复合句，一段 key 表达不了 —— 这一档给
// 成品串（Fallback，Key 为空），与 page 模块 pageTranslationsSavedMsg 同一取舍。
func variantSaveMsg(c *gin.Context, res *productdto.SaveVariantListResp) productMsg {
	if res == nil {
		return adminui.InternalMsg()
	}
	if res.Created == 0 && res.Updated == 0 && res.Deleted == 0 && len(res.Skipped) == 0 {
		return productVariantSaveNoChange.fact()
	}
	// 例外（提示页出口）：这条结论是**多段词条拼出来的复合句**（「已保存…」+「跳过 N 条：原因」），
	// 一段 key 表达不了 —— 提示页出口（shell.RenderJump）本来就是「需要成品串」的那一类。
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
	return productMsg{Fallback: strings.Join(parts, " ")}
}

// ProductsVariantCreate 为商品新增变体（未填字段继承商品级默认值）。
func (h *productPageHandle) ProductsVariantCreate(c *gin.Context) {
	req := &productdto.CreateVariantReq{
		ProductID: c.PostForm("productId"),
		SKUCode:   strings.TrimSpace(c.PostForm("skuCode")),
		// 归属仓（issue #15）：空值即兜底默认仓；无论选没选都会在归属仓生成库存记录。
		WarehouseID: strings.TrimSpace(c.PostForm("warehouseId")),
	}
	// 变体价：空串 = 不填（service 继承商品级默认价）；非法数字按未填处理（与商品新建同一分档）。
	if v, perr := productdto.OptFloat("price", c.PostForm("price")); perr == nil && v != nil {
		req.Price = v
	}
	projectID := c.PostForm("projectId")
	if _, err := h.products.CreateVariant(c.Request.Context(), req); err != nil {
		productEditJump(c, false, projectID, req.ProductID, productErrMsg(c, err))
		return
	}
	productEditJump(c, true, projectID, req.ProductID, productActionDoneMsg)
}

// ProductsVariantDelete 删除变体。
//
// 表单里的 id 是**变体 id**，回详情的商品 id 只能取 productId 隐藏域（不给 id 回落的机会）。
func (h *productPageHandle) ProductsVariantDelete(c *gin.Context) {
	projectID := c.PostForm("projectId")
	productID := c.PostForm("productId")
	if err := h.products.DeleteVariant(c.Request.Context(), &productdto.DeleteVariantReq{ID: c.PostForm("id")}); err != nil {
		productEditJump(c, false, projectID, productID, productErrMsg(c, err))
		return
	}
	productEditJump(c, true, projectID, productID, productActionDoneMsg)
}

// productRatingInvalidMsg 评分非数字的提示事实（0~5 的范围由 service 与 DB CHECK 兜底）。
var productRatingInvalidMsg = productMsg{Key: productenums.ProductsRatingInvalid, Fallback: "评分必须是 0~5 的数字"}

// ProductsRatingAdd 补录一条商品评分（issue #33）。
//
// 评分是**独立明细表**（#30），所以这是「加一条记录」而不是「改商品字段」——
// 分值范围由 service 与数据库 CHECK 双重兜底，这里只把非数字提前拦下并给出可读文案。
func (h *productPageHandle) ProductsRatingAdd(c *gin.Context) {
	projectID := c.PostForm("projectId")
	productID := c.PostForm("productId")
	score, perr := strconv.ParseFloat(strings.TrimSpace(c.PostForm("score")), 64)
	if perr != nil {
		productEditJump(c, false, projectID, productID, productRatingInvalidMsg)
		return
	}
	if _, err := h.products.AddRating(c.Request.Context(), &productdto.AddRatingReq{
		ProductID:  productID,
		Score:      score,
		OperatorID: builtin.GetUsername(c),
	}); err != nil {
		productEditJump(c, false, projectID, productID, productErrMsg(c, err))
		return
	}
	productEditJump(c, true, projectID, productID, productActionDoneMsg)
}

// ProductsRatingDelete 删掉一条评分（issue #33）：录错了能撤掉。
//
// 表单里的 id 是**评分 id**，回详情的商品 id 只能取 productId 隐藏域（不给 id 回落的机会）。
func (h *productPageHandle) ProductsRatingDelete(c *gin.Context) {
	projectID := c.PostForm("projectId")
	productID := c.PostForm("productId")
	if err := h.products.DeleteRating(c.Request.Context(), &productdto.DeleteRatingReq{
		ID: c.PostForm("id"),
		// 工程显式回传（DB-009）：product_ratings 有 FORCE 策略，删一条评分要在工程作用域里。
		ProjectID: projectID,
	}); err != nil {
		productEditJump(c, false, projectID, productID, productErrMsg(c, err))
		return
	}
	productEditJump(c, true, projectID, productID, productActionDoneMsg)
}

// ProductsAttributesSet 整体替换某商品引用的属性组（issue #7）。
//
// 引用的组必须是同一工程内真实存在的组（service 校验）；提交空数组即解绑全部。
//
// 表单的隐藏域是 id（值就是商品 id），formProductID 优先认 productId。
func (h *productPageHandle) ProductsAttributesSet(c *gin.Context) {
	projectID := c.PostForm("projectId")
	req := &productdto.UpdateReq{
		ID:           formProductID(c),
		AttributeIDs: splitIDs(c.PostForm("attributeIds")),
	}
	if _, err := h.products.Update(c.Request.Context(), req); err != nil {
		productEditJump(c, false, projectID, req.ID, productErrMsg(c, err))
		return
	}
	productEditJump(c, true, projectID, req.ID, productActionDoneMsg)
}

// ProductsDelete 删除商品（连带变体）。
//
// **例外：只有这个端点成功后仍回列表页** —— 商品已经不存在了，回详情页只会看到一个
// 「商品不存在」的空态，用户还得再点一次返回列表。
func (h *productPageHandle) ProductsDelete(c *gin.Context) {
	// projectId 必须带上：模板一直渲染着这个隐藏域，但这里曾经没读它 ——
	// service 的 resolveProjectID 在它为空时只肯走「唯一工程」兜底，
	// 多工程部署下删除直接回 ErrInvalidParam（页面不报错、只是删不掉）。
	req := &productdto.DeleteReq{
		ID:        strings.TrimSpace(c.PostForm("id")),
		ProjectID: strings.TrimSpace(c.PostForm("projectId")),
	}
	if err := h.products.Delete(c.Request.Context(), req); err != nil {
		productListJump(c, false, productErrMsg(c, err))
		return
	}
	productListJump(c, true, productBulkDoneMsg(productBulkDone))
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
func (h *productPageHandle) ProductPreviewFrame(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	// 例外（非模板出口的成品串，同 JSON 出口那一类）：iframe 里直出的是**一段字面 HTML**
	// （productPreviewFrameNotice），没有模板、也就没有渲染期取词这一层 ——
	// 这里必须自己把事实取成当前语言的文本。
	tr := shell.TranslateFor(c)
	id := strings.TrimSpace(c.Query("productId"))
	projectID := strings.TrimSpace(c.Query("project"))
	if id == "" {
		productPreviewFrameNotice(c, tr(productenums.ProductsPreviewNeedSave, "保存这个商品后，这里会显示它的详情页真实形态。"))
		return
	}
	if h == nil || h.instances == nil {
		productPreviewFrameNotice(c, tr(productenums.ProductsPreviewUnavailable, "预览不可用：发布能力未装配。"))
		return
	}
	res, err := h.instances.PreviewInstance(c.Request.Context(), &presentationcontract.PreviewInstanceReq{
		EntityType: productcontract.EntityTypeProduct, EntityID: id, ProjectID: projectID,
	})
	if err != nil {
		// 原文只进日志（后台页面不得直出内部错误）；页面上给一句能行动的话。
		logger.Scene("product").With("product_id", id).Error(err, "商品详情页预览渲染失败")
		productPreviewFrameNotice(c, tr(productenums.ProductsPreviewFailed, "预览渲染失败（多半是这个商品还没有可用的详情页模板）。"))
		return
	}
	if res == nil || strings.TrimSpace(res.HTML) == "" {
		productPreviewFrameNotice(c, tr(productenums.ProductsPreviewEmpty, "还没有可渲染的详情页：先给 product 类型建一套内容模板。"))
		return
	}
	// 直出的是**构建器渲染出来的详情页 HTML**（预览产物），不是错误信息。
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(res.HTML))
}

// productPreviewFrameNotice 预览不可用时的替代页（iframe 里的一句人话）。
//
// 与 content 模块那个同形但是两份：各自在自己的模块包里（跨模块共用要走契约，而这是纯展示装配）。
// 不用 5xx：iframe 对 5xx 显示浏览器自带错误页，读的人只会以为后台坏了 —— 而这里大多数情况是
// 「还没保存」或「还没建模板」，都是正常状态。
func productPreviewFrameNotice(c *gin.Context, text string) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(
		`<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><style>`+
			`body{margin:0;padding:24px;font:14px/1.7 -apple-system,"Segoe UI","PingFang SC","Microsoft YaHei",sans-serif;`+
			`color:#57606a;background:#fff;}p{margin:0;}</style></head><body><p>`+html.EscapeString(text)+`</p></body></html>`))
}

// ==========================================================================
// 列表页的批量写动作：批量改价与批量删除。
//
// 结论一律「成功 N / 跳过 M」都说清楚（只报「操作完成」会把部分成功静默成全部成功）。

// bulkPricingNothingSelected 没勾选就提交的意见（与批量删除同一口径：说清怎么继续）。
//
// 与其它批量回执同一个形状（key + 中文原文）：写侧 productBulkTextOf 取当前语言，
// 提示页直接渲染成品文案（不再经 URL）。
//
// 本节起是原先的 product_bulk_ops.go（列表页的批量改价与批删）。
// ==========================================================================
var bulkPricingNothingSelected = productBulkText{productenums.BulkPricingNoneSelected, "批量改价：没有勾选任何商品，请先勾选左侧复选框再执行。"}

// ProductsBulkPricing 批量「按规则改价」（POST /admin/products/bulk-pricing）。
//
// 入口为什么搬到商品列表：定价工具的作用对象本来就是「筛选出的商品集 / 标签 / SKU」，
// 而运营是在列表上圈出要改的那几个的；独立页 /admin/product-pricing 保留可用
// （它能对单个 SKU / 筛选集改价，也是唯一能试算的地方），本端点补上「按勾选批量应用」。
//
// 语义与其它批量操作一致（见 internal/module/CLAUDE.md §inbound/http）：
//
//	· id 一律经 shell.BulkIDs（去空白 / 去重 / 上限），超限**整批拒绝**并说明原因；
//	· 逐条走**单商品的应用路径**（scope=product），一条失败不中断整批；
//	· 结论按「改价 N 个变体 / 跳过 M 个商品」回带列表页，不做静默的部分成功。
//
// 规则解析与校验完全复用定价工具那一份（pricingRuleReqFromForm → readPricingForm /
// pricingParamsFromForm）：复制一份就会出现「抽屉填的 amount 被当成 multiplier」这类
// 只在一个入口发生的错。
func (h *productPageHandle) ProductsBulkPricing(c *gin.Context) {
	ctx := c.Request.Context()
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		// 受控提示（一次最多操作 N 项）保持可见，但同样经归口助手判定来源。
		productListJump(c, false, productBulkIDsMsg(c, berr))
		return
	}
	if len(ids) == 0 {
		productListJump(c, false, bulkPricingNothingSelected.fact())
		return
	}
	// 规则解析与校验完全复用定价工具那一份（readPricingForm / pricingParamsFromForm）：
	// 复制一份就会出现「抽屉填的 amount 被当成 multiplier」这类只在一个入口发生的错。
	// 范围恒为「单个商品的全部变体」，TargetID 由 service 逐商品注入。
	rule := pricingRuleReqFromForm(c, productenums.PricingScopeProduct, "")
	res, aerr := h.products.BulkApplyPricing(ctx, &productdto.BulkPricingReq{
		IDs: ids,
		// 操作人取自会话，表单字段不作数（调价留痕的操作人不可伪造）。
		Note:       strings.TrimSpace(c.PostForm("note")),
		OperatorID: builtin.CurrentUserIDText(c),
		Rule:       *rule,
	})
	if aerr != nil {
		productListJump(c, false, productErrMsg(c, aerr))
		return
	}
	mark := true
	if res.NotDone() > 0 || res.ChangedVariants == 0 {
		// 有跳过或一个变体都没改：走失败态（更显眼），用户下次会去看剩下的那些。
		mark = false
	}
	// 跳过原因由 service 回**业务 key**；取词（按请求语言翻成整句）留在这一层。
	reason := ""
	if res.SkipReasonKey != "" {
		reason = productErrText(c, errors.New(res.SkipReasonKey))
	}
	productListJump(c, mark, bulkPricingResultMsg(res.ChangedVariants, res.UnchangedProducts, res.NotDone(), reason))
}

// bulkPricingResultMsg 批量改价的结论**事实**（四类结果各一条词条 + `%s` 参数）。
//
// 选句与计数装配在 productenums.PricingBulkOutcome（控制器不再按计数挑文案）；
// reason 是**已取词的成品串**（逐商品的跳过原因），作为 `%s` 参数填入 —— 它是数据不是结论，
// 所以由调用方先取好（productErrText 在 JSON/片段出口仍在使用）。
func bulkPricingResultMsg(changedVariants, unchangedProducts, skipped int, reason string) productMsg {
	if strings.TrimSpace(reason) == "" {
		reason = productErrFallbackText(productenums.ErrPricingTargetNotFound)
	}
	return productOutcomeMsg(productenums.PricingBulkOutcome(changedVariants, unchangedProducts, skipped, reason))
}

// ProductsBulkDelete 批量删除商品（连同其全部变体，由 service 保证）。
//
// 逐条走同一条删除路径：失败的那条（已不存在 / 被其它数据引用）由服务端拒绝，其余照常删除
// —— 批量操作不能因为一条失败就整批回滚（用户会以为「一条都没删」，然后反复重试）。
// 结果按「已删 N 个 / 跳过 M 个」渲染提示页，避免静默的部分成功。
func (h *productPageHandle) ProductsBulkDelete(c *gin.Context) {
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		productListJump(c, false, productBulkIDsMsg(c, berr))
		return
	}
	res, err := h.products.BulkDeleteProducts(c.Request.Context(), &productdto.BulkDeleteReq{IDs: ids})
	if err != nil {
		productListJump(c, false, productErrMsg(c, err))
		return
	}
	ok, msg := productBulkDeleteResult(res.Succeeded, res.NotDone(), productBulkPartial, productBulkDone)
	productListJump(c, ok, msg)
}
