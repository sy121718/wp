package producthttp

import (
	"context"
	"go_wp/internal/adminui"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productservice "go_wp/internal/module/product/service"
	"go_wp/pkg/binding"
	"go_wp/pkg/pagination"
	"go_wp/pkg/response"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// product_attributes.go — 商品属性库（属性组 / 属性值）：列表页与它的抽屉表单。
//
// 属性分两组形态：**规格属性**（isVariation，参与变体生成）与普通属性（只作展示与筛选）。
// 属性值行（ProductAttributesValueRows）是 HTMX 片段，商品编辑页与新建页都复用它。

// 接口面（原先的 product_handle.go 属性段）在前，后台页面在后。

// ProductAttributesPage 属性管理页：工程切换 + 筛选栏 + 属性组列表 + 值编辑器 + 内联新建表单。
// productAttributesPageTemplate 属性库整页模板名。
const productAttributesPageTemplate = "admin/product/product_attributes.html"

// ProductAttributesPage 属性管理页：工程切换 + 筛选栏 + 属性组列表 + 值编辑器 + 内联新建表单。
func (h *ProductPageHandle) ProductAttributesPage(c *gin.Context) {
	ctx := c.Request.Context()
	// 读模型在装配期注入（见 ProductPageHandle.reader）：漏接 = 编译错误，
	// 不再有「请求期断言失败 → 500 兜底」这条路径。
	reader := h.reader
	// 取参数 → 调读模型（取数编排在 service：取哪些数据、按什么顺序、哪条失败算 400 /
	// 哪条只降级，都是业务结论。见 service.ProductAttributesPage）。
	view, err := reader.ProductAttributesPage(ctx, &productdto.ProductAttributesPageReq{
		ProjectID: strings.TrimSpace(c.Query("project")),
		Keyword:   strings.TrimSpace(c.Query("keyword")),
		Variation: strings.TrimSpace(c.Query("variation")),
		// 页码归一用跨页共用的 productPageNumber（商品列表页 / 分类标签页同口径）。
		Page: productPageNumber(c.Query("page")),
	})
	if view == nil {
		// 契约上视图恒非 nil（失败也要给齐模板必需键，见 contract.ProductPageReader）：
		// 真出现 nil 时兜一个空视图，而不是在这里 nil 下去 —— 缺键会让整页在那一行中断。
		view = &productdto.ProductAttributesPageView{
			Title: productenums.ProductAttributesTitle, Menu: "product-attributes",
			HTTPStatus: http.StatusInternalServerError,
		}
	}
	// —— 控制器负责的部分 ——
	view.Title = productenums.ProductAttributesTitle
	view.Menu = "product-attributes"
	// 抽屉表单的数据（编辑属性组 / 保存属性值 / 新建属性组）留在控制器：它们要 csrf
	//（表单隐藏域）与 adminui.PrepareBare 注入的取词函数，读模型不认识 gin.Context。
	// 行装配只在取数成功时做 —— 失败页渲染的是失败槽，装配出的行不会被读到。
	view.AttrCreateForm = attrCreateDrawerForm(c, view.SelectedProject)
	rows := make([]map[string]any, 0, len(view.AttributeDetails))
	if err == nil {
		for _, a := range view.AttributeDetails {
			// 两个抽屉的数据与失败重渲染**同形**（同一个 attrRowDrawerForms）——两处各拼
			// 一份必然分叉，而分叉只会在失败路径上缺键、静默截断整页。
			editForm, valuesForm := attrRowDrawerForms(c, view.SelectedProject, a)
			rows = append(rows, map[string]any{
				"ID": a.ID, "Key": a.Key, "Name": a.Name,
				"IsVariation": a.IsVariation, "Sort": a.Sort,
				"ValueCount": a.ValueCount,
				"EditForm":   editForm, "ValuesForm": valuesForm,
			})
		}
	}
	view.Attributes = rows
	// 分页条：Go 只交事实（总数 / 页码 / 每页条数 / 基地址），文案、页码窗口与链接由
	// admin/partials/pagination.html 渲染。基地址带上筛选条件，翻页不丢条件。
	if pg := pagination.Facts(view.Total, view.Page, view.Limit); pg.HasPagination() {
		pg.Base = pagination.BaseURL("/admin/product-attributes",
			attributeListFilterQuery(view.SelectedProject, view.FilterKeyword, view.FilterVariation))
		view.Pagination = pg
	}
	if err != nil {
		// 视图已带 LoadFailed / HTTPStatus（400 参数不合法 / 500 取数失败）。
		// 这里只补**当前语言**的失败槽文案：原始的 err 只进日志。
		view.LoadFailed = true
		view.LoadError = productErrPageText(c, err)
	}
	// 出口只有这一处（页面成败都走它）：档位与 no-store 由视图带出（PageStatus / PageNoStore）。
	adminui.RenderPage(c, productAttributesPageTemplate, view)
}

// attributeListFilterQuery 属性页的筛选条件（project + keyword + variation）。
func attributeListFilterQuery(projectID, keyword, variation string) url.Values {
	q := pagination.Query("project", projectID, "keyword", keyword)
	if variation != "" {
		q.Set("variation", variation)
	}
	return q
}

// ProductAttributesValueRows 值编辑器片段：add / remove 一次，回渲染整段行列表。
//
// 行编辑只重建原始表单值与位置；落库时的归一 / 去重 / 排序仍由 service 负责。
func (h *ProductPageHandle) ProductAttributesValueRows(c *gin.Context) {
	req, ok := binding.Form[productdto.AttributeRowsFormReq](c)
	if !ok {
		return
	}
	rows := req.Rows
	switch strings.TrimSpace(req.Action) {
	case "add":
		index := 0
		if len(rows) > 0 {
			index = rows[len(rows)-1].Index + 1
		}
		rows = append(rows, productdto.AttrValueFormRow{Index: index, Enabled: true})
	case "remove":
		if idx := req.RemoveIndexValue(); idx >= 0 && idx < len(rows) {
			rows = append(rows[:idx], rows[idx+1:]...)
		}
	}
	// 兜底：至少要有一行可编辑，空列表会让「添加」按钮无从下手。
	if len(rows) == 0 {
		rows = []productdto.AttrValueFormRow{{Enabled: true}}
	}
	groupID := strings.TrimSpace(req.GroupID)
	if groupID == "" {
		groupID = "new"
	}
	c.HTML(http.StatusOK, "admin/product/product_attribute_rows.html", attrRowsCtxData(c, groupID, rows))
}

func attrRowsFromResp(rows []productdto.AttributeValueResp) []productdto.AttrValueFormRow {
	out := make([]productdto.AttrValueFormRow, 0, len(rows))
	for i, row := range rows {
		out = append(out, productdto.AttrValueFormRow{
			Index: i, ID: row.ID, Key: row.Key, Label: row.Label,
			Sort: strconv.Itoa(row.Sort), Enabled: row.Enabled,
		})
	}
	return out
}

// attrRowsCtxData 属性值编辑片段（include）的渲染数据。
//
// GroupID 既标识「这是哪个组的编辑器」，也是片段拼容器 id / hx-target 的依据
// （attr-values-<GroupID>）—— 片段的 hx-include 会自动带上按钮自身的 groupId，
// 故客户端不需要额外传参。
//
// 为什么是 gin.H 而不是一个数据类 struct：片段的两条渲染路径（HTMX 片段端点用本函数的
// 返回值、抽屉表单 include 传 .RowsCtx）都要**按请求语言取词**，而 Jet 在 struct 上不支持
// `.["t"]`（渲染时报 can't use t as field name in struct type）。曾经这里给 struct 挂
// `Tr func(key, fallback string) string` 字段由控制器填 shell.TranslateFor(c) —— 那是
// 控制器取词。改成 gin.H 之后 t / tf 由 adminui.PrepareBare 注入，取词统一回到渲染期
// （与 values_form / group_form 两个片段同形）。
func attrRowsCtxData(c *gin.Context, groupID string, rows []productdto.AttrValueFormRow) gin.H {
	return adminui.PrepareBare(c, gin.H{"GroupID": groupID, "Rows": rows})
}

func parseIntOr(s string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return fallback
	}
	return n
}

// 属性组表单的两种形态：新建（页头 / 空态打开的抽屉 tpl-attr-create）
// 与编辑（列表每行一个抽屉 tpl-attr-edit-<id>）。
const (
	attrGroupModeCreate = "create"
	attrGroupModeEdit   = "edit"
)

// 属性页抽屉表单的模板名（片段自带 host，可独立渲染，不带 layout）。
const (
	attrGroupFormTemplate  = "admin/product/product_attribute_group_form.html"
	attrValuesFormTemplate = "admin/product/product_attribute_values_form.html"
)

// 三个抽屉表单的提交地址（原生 action 与 hx-post 恒为同一个值）。
const (
	attrGroupCreateAction = "/admin/product-attributes/create"
	attrGroupUpdateAction = "/admin/product-attributes/update"
	attrValuesAction      = "/admin/product-attributes/set-values"
)

// attrGroupFormOpts 属性组抽屉表单的数据入参。
//
// 用结构体而不是位置参数：这里连着两个 id（工程 / 属性组）与一个模式串，位置写反
// （工程 id 塞进属性组槽）不会编译失败 —— 只会在失败片段里把工程 id 渲染进隐藏域，
// 提交后回跳到一个不存在的工程，且只在失败路径上出现。
type attrGroupFormOpts struct {
	ProjectID string
	Mode      string
	GroupID   string
	// Name / Key / Sort 是抽屉的初值（编辑抽屉取该组的当前值，新建给零值）。
	Name      string
	Key       string
	Sort      int
	Variation bool
	RowsCtx   gin.H
}

// attrGroupFormData 属性组抽屉表单的渲染数据。
//
// 只有结构性键（Csrf / Project / t / tf / Mode / Action / IsCreate / GroupID /
// VariationChecked / RowsCtx）—— 恒定给齐，片段在**任何**渲染路径下都能独立成立。
// 没有回填键：抽屉的保存已迁 JSON 写基座，失败不再由服务端重渲染这一块。
//
// t / tf 由 adminui.PrepareBare 注入：片段是**带参数 include** 的（数据是每个抽屉
// 自己的数据类，不是页面 data），拿不到 adminui.Prepare 注入的页面键 —— 缺了 t 片段里的
// `.["t"](...)` 静默变空串（200、日志干净），缺了 tf 则是渲染中断（500，htmx 不 swap）。
// **不注入 csrf_token**（PrepareBare 只管多语言），token 仍由 Csrf 键显式带。
//
// 键恒在：模板直接读它们，缺键会让 Jet 从那行截断整个片段。
func attrGroupFormData(c *gin.Context, o attrGroupFormOpts) gin.H {
	return adminui.PrepareBare(c, gin.H{
		"Project": o.ProjectID,
		"GroupID": o.GroupID, "IsCreate": o.Mode == attrGroupModeCreate,
		"Name": o.Name, "Key": o.Key, "Sort": o.Sort,
		"VariationChecked": o.Variation,
		"RowsCtx":          o.RowsCtx,
	})
}

// attrValuesFormOpts 属性值抽屉表单的数据入参。
type attrValuesFormOpts struct {
	ProjectID string
	GroupID   string
	RowsCtx   gin.H
}

// attrValuesFormData 属性值抽屉表单的渲染数据。
//
// 可见字段全是 values[n].*，行的现值由 RowsCtx 带出（attrRowsFromResp 组装）。
func attrValuesFormData(c *gin.Context, o attrValuesFormOpts) gin.H {
	return adminui.PrepareBare(c, gin.H{
		"Project": o.ProjectID,
		"GroupID": o.GroupID,
		"RowsCtx": o.RowsCtx,
	})
}

// attrCreateDrawerForm 「新建属性组」抽屉的表单数据（页头与空态两个入口共用同一份）。
func attrCreateDrawerForm(c *gin.Context, projectID string) gin.H {
	return attrGroupFormData(c, attrGroupFormOpts{
		ProjectID: projectID,
		// 新建时没有可继承的组：勾选态沿用既有默认（勾上「参与变体」）；
		// 属性值行**首屏为空**（Rows: nil）：编辑器渲染 0 行 + 「还没有属性值…」的引导文案，
		// 用户点「+ 添加值」后由 ProductAttributesValueRows 兜底保证至少一行。两者口径**不同**，
		// 别按「同口径」推 —— 这里若补一行空输入，抽屉一打开就是一行空行配着引导文案，更费解。
		Mode: attrGroupModeCreate, GroupID: "new", Variation: true,
		RowsCtx: attrRowsCtxData(c, "new", nil),
	})
}

// attrRowDrawerForms 属性组列表里**一行**对应的两个抽屉表单数据（页面首屏）。
//
// 两个抽屉的初值都从同一行数据来：编辑抽屉取该组的名称 / 标识 / 排序 / 参与变体，
// 属性值抽屉取该组的属性值列表。
func attrRowDrawerForms(c *gin.Context, projectID string,
	a *productdto.AttributeResp) (gin.H, gin.H) {

	group := attrGroupFormData(c, attrGroupFormOpts{
		ProjectID: projectID,
		Mode:      attrGroupModeEdit, GroupID: a.ID,
		Name: a.Name, Key: a.Key, Sort: a.Sort, Variation: a.IsVariation,
	})
	values := attrValuesFormData(c, attrValuesFormOpts{
		ProjectID: projectID, GroupID: a.ID,
		RowsCtx: attrRowsCtxData(c, a.ID, attrRowsFromResp(a.Values)),
	})
	return group, values
}

// attributeOptions 属性组勾选列表（新建抽屉 / 编辑页；本批起不再让人手打 UUID）。
//
// 取数与标签口径（名字 + 标识 key，只给名字会在同名属性组之间选错）唯一一份在
// service 的 AttributeOptionViews —— 编辑页这条老路径与新建页读模型共用同一个结论，
// 两处各拼一遍时「带上 key」这条规则改一处就漏一处。
//
// 上限同样取 service 的常量：抽屉与新建页是同一类「一屏全量勾选」，两处各写一个 200
// 时改一处会静默分叉。
func (h *ProductPageHandle) attributeOptions(ctx context.Context, projectID string) (out []productdto.AttributeOptionView, err error) {
	out = []productdto.AttributeOptionView{}
	if strings.TrimSpace(projectID) == "" {
		return out, nil
	}
	list, lerr := h.products.ListAttributes(ctx, &productdto.ListAttributeReq{
		ProjectID: projectID, Size: productservice.PageAttributeOptionSize,
	})
	if lerr != nil {
		return nil, lerr
	}
	return productservice.AttributeOptionViews(list), nil
}

// ==========================================================================
// 属性接口：JSON 入参、pkg/response 出参、消息取 enums。
// 本层只做绑定与转发：值组的归一 / 去重 / 排序、引用校验一律在 service。
// ==========================================================================
// CreateAttribute 新建属性组（可同时带初始属性值）。
func (h *ProductHandle) CreateAttribute(c *gin.Context) {
	req, ok := binding.JSON[productdto.CreateAttributeReq](c)
	if !ok {
		return
	}
	res, err := h.svc.CreateAttribute(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgCreateSuccess, res)
}

// UpdateAttribute 修改属性组本身（值走 SetAttributeValues）。
func (h *ProductHandle) UpdateAttribute(c *gin.Context) {
	req, ok := binding.JSON[productdto.UpdateAttributeReq](c)
	if !ok {
		return
	}
	res, err := h.svc.UpdateAttribute(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgUpdateSuccess, res)
}

// SetAttributeValues 整体保存属性值（全量替换：请求里没有的值被删除）。
func (h *ProductHandle) SetAttributeValues(c *gin.Context) {
	req, ok := binding.JSON[productdto.SetAttributeValuesReq](c)
	if !ok {
		return
	}
	res, err := h.svc.SetAttributeValues(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgUpdateSuccess, res)
}

// GetAttribute 属性组详情。
func (h *ProductHandle) GetAttribute(c *gin.Context) {
	req, ok := binding.Query[productdto.GetAttributeReq](c)
	if !ok {
		return
	}
	res, err := h.svc.GetAttribute(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusNotFound, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDetailSuccess, res)
}

// ListAttributes 属性组列表。
func (h *ProductHandle) ListAttributes(c *gin.Context) {
	req, ok := binding.Query[productdto.ListAttributeReq](c)
	if !ok {
		return
	}
	list, err := h.svc.ListAttributes(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgListSuccess, list)
}

// DeleteAttribute 删除属性组（被商品引用时拒绝）。
func (h *ProductHandle) DeleteAttribute(c *gin.Context) {
	req, ok := binding.JSON[productdto.DeleteAttributeReq](c)
	if !ok {
		return
	}
	if err := h.svc.DeleteAttribute(c.Request.Context(), req); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDeleteSuccess, nil)
}

// BulkDeleteAttributes 批量删除属性组（页面 /admin/product-attributes/bulk-delete 的承接方）。
//
// 入口与三套归类共用（bulkDeleteByIDs）：归一、上限、回执键名必须逐字同口径，
// 否则超限那批会被静默截断、或摘要句恒为「已删除 0 项」。
func (h *ProductHandle) BulkDeleteAttributes(c *gin.Context) {
	h.bulkDeleteByIDs(c, h.svc.BulkDeleteProductAttributes)
}
