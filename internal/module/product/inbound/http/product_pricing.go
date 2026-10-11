package producthttp

import (
	"context"
	"encoding/json"
	"go_wp/internal/adminui"
	"go_wp/internal/middleware/builtin"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/internal/shell"
	"go_wp/pkg/response"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// product_pricing.go — 批量改价的规则页（预览 → 应用 → 历史）。
//
// 预览与应用的输入是同一份表单解析（readPricingForm），只是前者不落库 —— 两条路径
// 各解析一次的话，「预览说会变成 9.9、应用后变成 10.0」这种分叉不会报错。

// pricingHistoryLimit 后台留痕台账展示的批次数（每批再取一次明细）。
// 与属性 / 分类 / 品牌页同一模式：GET 渲染完整页，POST 处理完渲染整页提示
// （shell.RenderJump，原生表单 + csrf_token 隐藏域）。只用 GET/POST。
//
// 本页承载本票的验收 1 / 2 / 4：
//
//	· 建手工标签并挂到商品（挂载表单在商品**详情页**的「商品标签」区块 ——
//	  标签归属是商品的属性，列表页只回答「有哪些商品」，见 admin-ui-logic §1）；
//	· 自动标签只给内置规则类型 + 白名单参数（规则类型下拉来自 service 的注册表，
//	  参数输入框固定三格：days / minPrice / maxPrice，服务端按规则类型取值并严格校验）；
//	· 标签列表用标准表格（标签 / URL 段 / 类型 / 规则 / 命中 / 重算时间 / 操作），
//	  规则标签额外显示规则描述与重算时间；「命中的商品」按需加载（审计 PERF-02）：
//	  首屏只给数量，展开某个标签才按页取片段，见 ProductTagHitsFragment。
//
// 重算时机在页面上写明（商品/变体写操作后、标签定义变更后、这里的「重算」按钮）。
//
// 接口面（原先的 product_handle.go 定价段）在前，后台页面在后。

const pricingHistoryLimit = 10

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

// renderPricingPageWith 定价页的统一渲染入口（form / preview / errMsg 三者可空）。
func (h *productPageHandle) renderPricingPageWith(c *gin.Context, form *productdto.PricingForm, preview *productdto.PricingPreviewResp, errMsg string) {
	ctx := c.Request.Context()
	projects := h.pricingProjects(ctx)
	selected := strings.TrimSpace(c.Query("project"))
	if form != nil && form.ProjectID != "" {
		selected = form.ProjectID
	}
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	rules := h.products.ListPricingRuleTypes(ctx)
	roundings := h.products.ListPricingRoundingOptions(ctx)
	view := defaultPricingForm(rules, roundings)
	if form != nil {
		view = *form
	}
	if view.ProjectID == "" {
		view.ProjectID = selected
	}
	c.HTML(http.StatusOK, "admin/product/product_pricing.html", adminui.Prepare(c, gin.H{
		"title":           productenums.ProductPricingTitle,
		"menu":            "product-pricing",
		"Projects":        projects,
		"SelectedProject": selected,
		"Rules":           rules,
		"Roundings":       roundings,
		"Form":            view,
		"HasPreview":      preview != nil,
		"PreviewSummary":  pricingPreviewSummary(preview),
		"PreviewRows":     pricingLineRows(previewLines(preview)),
		"History":         h.pricingHistory(ctx, selected),
		// Err 只剩**试算失败原地重渲染**这一条来源（errMsg 是本页 handler 的产物，
		// 已过 productErrText）；写动作（应用调价）的结论走 shell.RenderJump 渲染提示页，
		// 原先 ?err= / ?applied= 的读侧已整批删除（见 product_jump.go）。
		"Err": errMsg,
		// 页面上下文（表单 action 的 query）：应用调价的提示页由 shell.BackPath 读回。
		"ListQuery": productQueryFromRequest(c, productPricingBackKeys...),
	}))
}

// pricingProjects 工程列表（读失败时返回空列表：定价页不该因为一个下拉整页打不开）。
func (h *productPageHandle) pricingProjects(ctx context.Context) (projects []projectdto.ProjectResp) {
	list, err := h.projects.List(ctx)
	if err != nil {
		return []projectdto.ProjectResp{}
	}
	return list
}

// pricingHistory 调价留痕（批次 + 逐变体明细）。
func (h *productPageHandle) pricingHistory(ctx context.Context, projectID string) (rows []gin.H) {
	rows = []gin.H{}
	if strings.TrimSpace(projectID) == "" {
		return rows
	}
	list, err := h.products.ListPriceAdjustments(ctx, &productdto.ListPriceAdjustmentReq{
		ProjectID: projectID, Limit: pricingHistoryLimit,
	})
	if err != nil {
		return rows
	}
	for _, a := range list {
		detail, derr := h.products.GetPriceAdjustment(ctx, &productdto.GetPriceAdjustmentReq{ID: a.ID})
		if derr != nil {
			// 单条明细读失败不该让整页打不开：退回批次本身的汇总。
			rows = append(rows, pricingHistoryRow(a))
			continue
		}
		rows = append(rows, pricingHistoryRow(detail))
	}
	return rows
}

// pricingHistoryRow 批次 → 模板行（时间压成可读文本，明细逐条列出原价与新价）。
func pricingHistoryRow(a *productdto.PriceAdjustmentResp) gin.H {
	items := make([]gin.H, 0, len(a.Items))
	for _, it := range a.Items {
		items = append(items, gin.H{
			"ProductName": it.ProductName, "SKUCode": it.SKUCode,
			"OldPrice": formatAmount(it.OldPrice), "NewPrice": formatAmount(it.NewPrice),
			"Diff": formatSignedAmount(it.Diff),
		})
	}
	return gin.H{
		"ID": a.ID, "RuleLabel": a.RuleLabel, "ScopeLabel": a.ScopeLabel,
		"RoundingLabel": a.RoundingLabel, "FilterLabel": a.FilterLabel,
		"ChangedCount": a.ChangedCount, "VariantCount": a.VariantCount,
		"Note": a.Note, "OperatorID": a.OperatorID,
		"CreatedAtText": pricingTimeLabel(a.CreatedAt),
		"Items":         items,
	}
}

// pricingLineRows 试算行 → 模板行（金额与状态标签由服务端算好）。
func pricingLineRows(lines []*productdto.PricingLineResp) []gin.H {
	rows := make([]gin.H, 0, len(lines))
	for _, l := range lines {
		if l == nil {
			continue
		}
		rows = append(rows, gin.H{
			"ProductName": l.ProductName, "SKUCode": l.SKUCode,
			"CostPrice": formatNullableAmount(l.CostPrice),
			"OldPrice":  formatAmount(l.OldPrice), "NewPrice": formatAmount(l.NewPrice),
			"Diff":   formatSignedAmount(l.NewPrice - l.OldPrice),
			"Status": l.Status, "StatusLabel": l.StatusLabel,
			"IsChanged":   l.Status == productenums.PricingLineChanged,
			"StatusTone":  pricingLineTone(l.Status),
			"IsSkipped":   l.Status == productenums.PricingLineSkipped,
			"ReasonLabel": l.ReasonLabel,
		})
	}
	return rows
}

// pricingPreviewSummary 试算结果的汇总行（模板只做展示，不做计算 —— 也避免模板解指针）。
func pricingPreviewSummary(p *productdto.PricingPreviewResp) gin.H {
	if p == nil {
		return gin.H{}
	}
	return gin.H{
		"RuleLabel":      p.RuleLabel,
		"RoundingLabel":  p.RoundingLabel,
		"ScopeLabel":     p.ScopeLabel,
		"TargetCount":    p.TargetCount,
		"ChangedCount":   p.ChangedCount,
		"UnchangedCount": p.UnchangedCount,
		"SkippedCount":   p.SkippedCount,
	}
}

// previewLines 试算结果的明细（nil 安全）。
func previewLines(p *productdto.PricingPreviewResp) []*productdto.PricingLineResp {
	if p == nil {
		return nil
	}
	return p.Lines
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
		if form.Multiplier != "" {
			if v, err := parseFloat(form.Multiplier); err == nil {
				params["multiplier"] = v
			}
		}
	case productenums.PricingRuleCostMarkup, productenums.PricingRuleFixedPrice:
		if form.Amount != "" {
			if v, err := parseFloat(form.Amount); err == nil {
				params["amount"] = v
			}
		}
	case productenums.PricingRuleTargetMargin:
		if form.Margin != "" {
			if v, err := parseFloat(form.Margin); err == nil {
				params["margin"] = v
			}
		}
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return json.RawMessage("{}")
	}
	return raw
}

// defaultPricingForm 表单初值：第一条规则 + 第一种尾数处理 + 筛选集范围（批量改价的典型用法）。
func defaultPricingForm(rules []*productdto.PricingRuleTypeResp, roundings []*productdto.PricingRoundingOptionResp) productdto.PricingForm {
	form := productdto.PricingForm{
		Rounding: productenums.PricingRoundingNone,
		Scope:    productenums.PricingScopeFilter,
	}
	if len(rules) > 0 && rules[0] != nil {
		form.RuleType = rules[0].Type
	}
	if len(roundings) > 0 && roundings[0] != nil {
		form.Rounding = roundings[0].Value
	}
	return form
}

// pricingTimeLabel RFC3339 → 后台展示文本（同样的压轴规则见标签页的 recalcLabel）。
func pricingTimeLabel(at string) string {
	if strings.TrimSpace(at) == "" {
		return "—"
	}
	if t, err := time.Parse(time.RFC3339, at); err == nil {
		return t.Local().Format("2006-01-02 15:04")
	}
	return at
}

// formatSignedAmount 差值文本（正数带 + 号）。
func formatSignedAmount(v float64) string {
	if v > 0 {
		return "+" + formatAmount(v)
	}
	return formatAmount(v)
}

// pricingLineTone 价格行状态 → 徽标分档。
//
// 判据与「IsChanged」同源（productenums.PricingLineChanged）：改过的价格行要人复核 → info。
func pricingLineTone(status string) string {
	if status == productenums.PricingLineChanged {
		return "info"
	}
	return "mute"
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
