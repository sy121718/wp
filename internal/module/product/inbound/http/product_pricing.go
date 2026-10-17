package producthttp

import (
	json "go_wp/pkg/jsonx"

	"go_wp/internal/adminui"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/internal/shell"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"

	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// product_pricing.go — 批量改价的规则页（预览 → 应用 → 历史）。
//
// 预览与应用的输入是同一份表单解析（readPricingForm），只是前者不落库 —— 两条路径
// 各解析一次的话，「预览说会变成 9.9、应用后变成 10.0」这种分叉不会报错。

// 与属性 / 分类 / 品牌页同一模式：GET 渲染完整页，POST 处理完渲染整页提示
// （shell.RenderJump，原生表单 + csrf_token 隐藏域）。只用 GET/POST。
//
//
// 接口面（原先的 product_handle.go 定价段）在前，后台页面在后。

// ProductPricingPage 定价工具页：规则参考表 + 改价表单 + 留痕台账。
func (h *productPageHandle) ProductPricingPage(c *gin.Context) {
	h.renderPricingPageWith(c, nil, nil, "")
}

// ProductPricingPreview 试算（不落库、不留痕），结果渲染回同一页。
func (h *productPageHandle) ProductPricingPreview(c *gin.Context) {
	form, req := readPricingForm(c)
	preview, err := h.products.PreviewPricing(c.Request.Context(), &productdto.PricingPreviewReq{PricingRuleReq: *req})
	if err != nil {
		// 试算失败不重定向：用户填的表单要留在眼前，否则「哪一项填错了」无从改起。
		// 试算失败的原文（含 PG 原文）只进日志，页面上给可读文案。
		h.renderPricingPageWith(c, &form, nil, productErrText(c, err))
		return
	}
	h.renderPricingPageWith(c, &form, preview, "")
}

// productPricingPageTemplate 定价页模板名（与「提示页出口」同处本文件）。
const productPricingPageTemplate = "admin/product/product_pricing.html"

// renderPricingPageWith 定价页的统一渲染入口（form / preview / errMsg 三者可空）。
//
// 取数编排（工程清单 / 规则参考表 / 改价表单初值 / 留痕台账）与视图装配都在 service 的读模型
// （`ProductService.ProductPricingPage`）—— 这里只剩三件事：读参数、调读模型、渲染。
//
// form / preview / errMsg 是**写路径的产物**（试算失败要原地重渲染，把用户填的表单与试算结果
// 带回来）。service 不认识 gin.Context，所以由这里读出来经 Req 传进去。
func (h *productPageHandle) renderPricingPageWith(c *gin.Context, form *productdto.PricingForm, preview *productdto.PricingPreviewResp, errMsg string) {
	// 读模型在装配期注入（见 productPageHandle.reader）：漏接 = 编译错误，
	// 不再有「请求期断言失败 → 500 兜底」这条路径。
	reader := h.reader
	view, err := reader.ProductPricingPage(c.Request.Context(), &productdto.ProductPricingPageReq{
		ProjectID: strings.TrimSpace(c.Query("project")),
		Form:      form,
		Preview:   preview,
		ErrMsg:    errMsg,
	})
	if view == nil {
		view = &productdto.ProductPricingPageView{
			Title: productenums.ProductPricingTitle, Menu: "product-pricing",
			HTTPStatus: http.StatusInternalServerError,
		}
	}
	if err != nil {
		// 契约上本页恒不返回 error（三处取数各自降级）：真出现时按失败档渲染并留日志。
		logger.Scene("product").Error(err, "定价页取数失败")
		view.HTTPStatus = http.StatusInternalServerError
	}
	// 表单 action 的 query：应用调价的提示页由 pkg/backpath 读回 —— URL 形状属 HTTP 边界。
	view.ListQuery = productQueryFromRequest(c, productPricingBackKeys...)
	adminui.RenderPage(c, productPricingPageTemplate, view)
}

// readPricingForm 读定价表单并归一为 service 入参。
//
// 规则参数只在规则类型对应的输入框里取（与标签页同一手法）：
// 「只接受内置参数」在后台这一侧同样成立，多余输入不会进 params。
// 数字解析失败时给空对象，由 service 给出统一的可读错误，这里不重复一套校验。
func readPricingForm(c *gin.Context) (form productdto.PricingForm, req *productdto.PricingRuleReq) {
	form = productdto.PricingForm{
		ProjectID:  strings.TrimSpace(c.PostForm("projectId")),
		RuleType:   strings.TrimSpace(c.PostForm("ruleType")),
		Multiplier: strings.TrimSpace(c.PostForm("multiplier")),
		Amount:     strings.TrimSpace(c.PostForm("amount")),
		Margin:     strings.TrimSpace(c.PostForm("margin")),
		Rounding:   strings.TrimSpace(c.PostForm("rounding")),
		Scope:      strings.TrimSpace(c.PostForm("scope")),
		TargetID:   strings.TrimSpace(c.PostForm("targetId")),
		Status:     strings.TrimSpace(c.PostForm("status")),
		Keyword:    strings.TrimSpace(c.PostForm("keyword")),
		CategoryID: strings.TrimSpace(c.PostForm("categoryId")),
		BrandID:    strings.TrimSpace(c.PostForm("brandId")),
		TagID:      strings.TrimSpace(c.PostForm("tagId")),
		Note:       strings.TrimSpace(c.PostForm("note")),
	}
	req = &productdto.PricingRuleReq{
		ProjectID:  form.ProjectID,
		RuleType:   form.RuleType,
		Rounding:   form.Rounding,
		Scope:      form.Scope,
		TargetID:   form.TargetID,
		Status:     form.Status,
		Keyword:    form.Keyword,
		CategoryID: form.CategoryID,
		BrandID:    form.BrandID,
		TagID:      form.TagID,
		RuleParams: pricingParamsFromForm(form),
	}
	return form, req
}

// pricingRuleReqFromForm 读「按规则改价」表单并套上指定的作用范围。
//
// 两个入口共用：独立定价页（/admin/product-pricing/preview|apply，范围由表单的 scope /
// targetId 决定）与商品列表的批量改价抽屉（逐个商品套 scope=product + 该商品 id）。
// 规则类型、规则参数、尾数的解析只有一份（readPricingForm + pricingParamsFromForm）——
// 抽屉若另抄一份，最先出问题的是「填了 amount 却被当成 multiplier」这类静默错配。
func pricingRuleReqFromForm(c *gin.Context, scope, targetID string) (req *productdto.PricingRuleReq) {
	_, req = readPricingForm(c)
	req.Scope = scope
	req.TargetID = targetID
	return req
}

// pricingParamsFromForm 按规则类型从表单拼规则参数（只取本类型用得到的键）。
func pricingParamsFromForm(form productdto.PricingForm) json.RawMessage {
	params := map[string]any{}
	switch form.RuleType {
	case productenums.PricingRuleCostMultiple:
		// 空串 = 本次不给这个参数（与既有的「非空才解析」前置判据同义）；非法数字**不给键** ——
		// service 会统一报「参数缺失或不是数字」，这里不重复一套校验（见函数头注释）。
		// NaN / 正负无穷归入非法：它们进了算价会得到一整批 NaN 价格。
		if v, err := productdto.OptFloat("multiplier", form.Multiplier); err == nil && v != nil {
			params["multiplier"] = *v
		}
	case productenums.PricingRuleCostMarkup, productenums.PricingRuleFixedPrice:
		if v, err := productdto.OptFloat("amount", form.Amount); err == nil && v != nil {
			params["amount"] = *v
		}
	case productenums.PricingRuleTargetMargin:
		if v, err := productdto.OptFloat("margin", form.Margin); err == nil && v != nil {
			params["margin"] = *v
		}
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return json.RawMessage("{}")
	}
	return raw
}

// ==========================================================================
// 定价接口：JSON 入参、pkg/response 出参、消息取 enums。
// 本层只做绑定、覆盖操作人与转发：规则校验、算价、落库与留痕一律在 service。
// ==========================================================================
// ListPricingRuleTypes 内置定价规则类型清单（后台下拉与参数说明的唯一来源）。
func (h *Handle) ListPricingRuleTypes(c *gin.Context) {
	response.SuccessWithMessage(c, productenums.MsgListSuccess, h.svc.ListPricingRuleTypes(c.Request.Context()))
}

// ListPricingRoundingOptions 尾数处理清单。
func (h *Handle) ListPricingRoundingOptions(c *gin.Context) {
	response.SuccessWithMessage(c, productenums.MsgListSuccess, h.svc.ListPricingRoundingOptions(c.Request.Context()))
}

// PreviewPricing 按规则试算（不落库、不留痕）。
func (h *Handle) PreviewPricing(c *gin.Context) {
	req := &productdto.PricingPreviewReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.PreviewPricing(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
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
func (h *Handle) ApplyPricing(c *gin.Context) {
	req := &productPricingApplyReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	form := productdto.PricingForm{
		ProjectID:  req.ProjectID,
		RuleType:   req.RuleType,
		Multiplier: req.Multiplier,
		Amount:     req.Amount,
		Margin:     req.Margin,
		Rounding:   req.Rounding,
		Scope:      req.Scope,
		TargetID:   req.TargetID,
		Status:     req.Status,
		Keyword:    req.Keyword,
		CategoryID: req.CategoryID,
		BrandID:    req.BrandID,
		TagID:      req.TagID,
		Note:       req.Note,
	}
	res, err := h.svc.ApplyPricing(c.Request.Context(), &productdto.PricingApplyReq{
		PricingRuleReq: productdto.PricingRuleReq{
			ProjectID: req.ProjectID, RuleType: req.RuleType, Rounding: req.Rounding,
			Scope: req.Scope, TargetID: req.TargetID,
			Status: req.Status, Keyword: req.Keyword, CategoryID: req.CategoryID,
			BrandID: req.BrandID, TagID: req.TagID,
			RuleParams: pricingParamsFromForm(form),
		},
		Note: strings.TrimSpace(req.Note),
		// 操作人取自会话，客户端传什么都不作数（留痕的操作人不可伪造）。
		OperatorID: operatorFromContext(c),
	})
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
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
func (h *Handle) BulkPricing(c *gin.Context) {
	req := &productBulkPricingReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	// 归一口径与其余批量端点同一个纯函数：去空白 / 去重 / 上限 MaxIDs，超限整批拒绝
	//（回执带「上限 / 本次条数」两个数字，见 productBulkIDs）。
	ids, ok := productBulkIDs(c, req.IDs)
	if !ok {
		return
	}
	// 没勾选就提交：明确拒绝，不是「0 个改动」的成功回执。页面侧原有这条守卫，随页面 POST
	// 一起消失 —— 判据搬到这里，否则删掉页面通道后「空勾选静默成功」就没人拦了。
	if len(ids) == 0 {
		response.ErrorWithMessage(c, http.StatusBadRequest, productMsgText(c, productMsg{
			Key: productenums.BulkPricingNoneSelected, Fallback: "批量改价：没有勾选任何商品，请先勾选左侧复选框再执行。"}))
		return
	}
	form := productdto.PricingForm{
		ProjectID:  req.ProjectID,
		RuleType:   req.RuleType,
		Multiplier: req.Multiplier,
		Amount:     req.Amount,
		Margin:     req.Margin,
		Rounding:   req.Rounding,
		Note:       req.Note,
	}
	res, err := h.svc.BulkApplyPricing(c.Request.Context(), &productdto.BulkPricingReq{
		IDs:        ids,
		ProjectID:  req.ProjectID,
		Note:       req.Note,
		OperatorID: operatorFromContext(c),
		Rule: productdto.PricingRuleReq{
			ProjectID:  req.ProjectID,
			RuleType:   req.RuleType,
			Rounding:   req.Rounding,
			Scope:      productenums.PricingScopeProduct,
			RuleParams: pricingParamsFromForm(form),
		},
	})
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgPricingApplySuccess, productBulkPricedView(c, res))
}

// productBulkPricingReq 批量改价的 JSON 入参（规则字段平铺，理由见 BulkPricing）。
type productBulkPricingReq struct {
	IDs        []string `json:"ids"`
	ProjectID  string   `json:"projectId"`
	Note       string   `json:"note"`
	RuleType   string   `json:"ruleType"`
	Multiplier string   `json:"multiplier"`
	Amount     string   `json:"amount"`
	Margin     string   `json:"margin"`
	Rounding   string   `json:"rounding"`
}

// productBulkPricedView 批量改价的回执。
//
// 成功键是 updated 而不是 deleted：json_write.js 的 okKeyOf 按 outcome=update 读
// data.updated —— 「它被改过了」与「把它从列表里删掉」不是同一件事（基座据此决定要不要
// 把行从表格里移除；改价后商品还在，移掉会让人以为商品没了）。
type productBulkPricingView struct {
	Updated int                   `json:"updated"`
	Changed int                   `json:"changedVariants"`
	Skipped int                   `json:"skipped"`
	Items   []productBulkItemView `json:"items"`
}

// productBulkPricedView 把 service 的计数 + 业务 key 转成基座要的形状（与删除那一份同源，
// 只是成功键不同）。
func productBulkPricedView(c *gin.Context, res *productdto.BulkPricingResult) productBulkPricingView {
	view := productBulkPricingView{
		Updated: res.Succeeded,
		Changed: res.ChangedVariants,
		Skipped: res.NotDone(),
		Items:   make([]productBulkItemView, 0, len(res.Items)),
	}
	for _, it := range res.Items {
		row := productBulkItemView{ID: it.ID, Updated: it.Updated}
		if !it.Updated && it.ReasonKey != "" {
			row.Message = productErrText(c, errors.New(it.ReasonKey))
		}
		view.Items = append(view.Items, row)
	}
	return view
}

// ListPriceAdjustments 调价留痕列表（按工程）。
func (h *Handle) ListPriceAdjustments(c *gin.Context) {
	req := &productdto.ListPriceAdjustmentReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	list, err := h.svc.ListPriceAdjustments(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgListSuccess, list)
}

// GetPriceAdjustment 单批次留痕详情（含逐变体「原价 → 新价」明细）。
func (h *Handle) GetPriceAdjustment(c *gin.Context) {
	req := &productdto.GetPriceAdjustmentReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.GetPriceAdjustment(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusNotFound, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDetailSuccess, res)
}
