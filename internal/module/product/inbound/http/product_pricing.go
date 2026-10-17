package producthttp

import (
	json "go_wp/pkg/jsonx"

	"go_wp/internal/adminui"
	"go_wp/internal/middleware/builtin"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/internal/shell"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"

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

// ProductPricingApply 应用调价（落库 + 留痕 + 顺带重算自动标签）。
func (h *productPageHandle) ProductPricingApply(c *gin.Context) {
	_, req := readPricingForm(c)
	res, err := h.products.ApplyPricing(c.Request.Context(), &productdto.PricingApplyReq{
		PricingRuleReq: *req,
		Note:           strings.TrimSpace(c.PostForm("note")),
		// 操作人取自会话，客户端的表单字段不作数（留痕不可伪造）。
		OperatorID: builtin.CurrentUserIDText(c),
	})
	if err != nil {
		productPricingJump(c, false, productErrMsg(c, err))
		return
	}
	// 成功回执：保留原先页面上那句话（「本次改动 N 个变体，明细见下方留痕台账」），
	// 但改由提示页渲染（取代 ?applied= 计数 + 页内提示条）。
	// **例外 2（提示页出口）**：shell.RenderJump 那条链要的是成品串（见 product_jump.go
	// 的 productPageJump → shell.RenderJump），这里取词一次交给它，不在页面上再取。
	// 三段拼一句（前缀 + 计数 + 后缀）：模板注入的 tf 只有 %s 位置参数，收成一条 %s 整句
	// 要改 seed，而 public/migrations/** 不在本批写范围内（page 模块按迁移 613 收过同类，
	// product 这两条 key 还是两段式）。
	tr := shell.TranslateFor(c)
	productPricingJump(c, true, productMsg{Fallback: tr(productenums.ProductPricingAppliedLead, "已应用调价：本次改动 ") +
		strconv.Itoa(res.ChangedCount) + tr(productenums.ProductPricingAppliedTail, " 个变体，明细见下方留痕台账。")})
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

// ApplyPricing 应用调价（算价 → 写回 product_variants.price → 留痕）。
func (h *Handle) ApplyPricing(c *gin.Context) {
	req := &productdto.PricingApplyReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	// 操作人取自会话，客户端传什么都不作数（留痕的操作人不可伪造）。
	req.OperatorID = operatorFromContext(c)
	res, err := h.svc.ApplyPricing(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgPricingApplySuccess, res)
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
