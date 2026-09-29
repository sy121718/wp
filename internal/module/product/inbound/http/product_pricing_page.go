// product_pricing_handle.go — 后台定价工具页（issue #13）。
//
// 与属性 / 分类 / 品牌 / 标签页同一模式：GET 渲染完整页，POST 处理完 302 回列表，
// 错误经 ?err= 回显（原生表单 + csrf_token 隐藏域）。只用 GET/POST。
//
// 三个与其它后台页不同的地方，都是本票的验收要求：
//
//  1. **预览**（验收 4）：POST /admin/product-pricing/preview 直接把试算结果渲染回同一页
//     （200，不是 302）—— 预览必须能看到明细，而明细只在这一次响应里；
//     表单值原样回填，用户接着点「应用调价」用的就是刚刚预览过的那份参数。
//  2. **应用**（验收 3）：POST /admin/product-pricing/apply 走 service 落库 + 留痕，
//     完成后 302 回列表并带 applied=N。
//  3. **留痕**（验收 4）：页面下半部分是调价台账，每个批次可展开看到逐变体「原价 → 新价」。
package producthttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/internal/web/shell"
)

// pricingHistoryLimit 后台留痕台账展示的批次数（每批再取一次明细）。
const pricingHistoryLimit = 10

// pricingForm 定价表单的取值（试算后要原样回填，用户接着点应用）。
type pricingForm struct {
	ProjectID  string
	RuleType   string
	Multiplier string
	Amount     string
	Margin     string
	Rounding   string
	Scope      string
	TargetID   string
	Status     string
	Keyword    string
	CategoryID string
	BrandID    string
	TagID      string
	Note       string
}

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
		OperatorID: shell.CurrentUserIDText(c),
	})
	if err != nil {
		c.Redirect(http.StatusFound, "/admin/product-pricing?project="+url.QueryEscape(req.ProjectID)+
			"&err="+url.QueryEscape(productErrText(c, err)))
		return
	}
	c.Redirect(http.StatusFound, "/admin/product-pricing?project="+req.ProjectID+"&applied="+strconv.Itoa(res.ChangedCount))
}

// renderPricingPageWith 定价页的统一渲染入口（form / preview / errMsg 三者可空）。
func (h *productPageHandle) renderPricingPageWith(c *gin.Context, form *pricingForm, preview *productdto.PricingPreviewResp, errMsg string) {
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
	c.HTML(http.StatusOK, "admin/product/product_pricing.html", shell.Prepare(c, gin.H{
		"title":           shell.TranslateFor(c)(productenums.ProductPricingTitle, "定价工具"),
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
		// errMsg 是本页 handler 的产物（productErrText）；query 那一路过白名单（product_err.go）。
		"Err": firstNonEmpty(errMsg, productPageErr(c)),
		// ?applied= 只承载计数：只放行纯数字。
		"Applied": productAppliedToken(c),
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
func readPricingForm(c *gin.Context) (form pricingForm, req *productdto.PricingRuleReq) {
	form = pricingForm{
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
func pricingParamsFromForm(form pricingForm) json.RawMessage {
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
func defaultPricingForm(rules []*productdto.PricingRuleTypeResp, roundings []*productdto.PricingRoundingOptionResp) pricingForm {
	form := pricingForm{
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

// firstNonEmpty 取第一个非空字符串。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
