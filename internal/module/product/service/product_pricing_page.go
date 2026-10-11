// product_pricing_page.go — 定价工具页的读模型（工程清单 / 参考表 / 表单初值 / 留痕台账）。
//
// 这一屏的特殊之处是**没有失败档**：三处取数各自降级（工程清单读不到 → 空下拉；规则与尾数处理
// 是注册表快照，本身无 error；留痕台账读不到 → 空台账），页面主体（改价表单）始终撑得起来。
// 所以读模型恒返回 nil error —— 保留 error 返回位是为了与另外五个读模型同形（控制器同一套
// 调用形状），也为了将来某一路取数升级成「必须成功」时不必改签名。
//
// 明细的金额文本与状态徽标档位都在这里算好：模板只做展示，不做计算、也不解指针。
package productservice

import (
	"context"
	"strings"
	"time"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/pkg/money"
)

// pricingHistoryLimit 台账最多列几个批次（明细逐条展开，所以批次不能无限）。
const pricingHistoryLimit = 10

// DefaultPricingForm 改价表单初值：第一条规则 + 第一种尾数处理 + 筛选集范围（批量改价的典型用法）。
//
// 导出是因为**列表页的批量改价抽屉也用同一份初值**（那一屏的行装配还没下沉）：两处各写一份时，
// 改了一处的那天两个入口会给出不同的默认规则。
func DefaultPricingForm(rules []*productdto.PricingRuleTypeResp, roundings []*productdto.PricingRoundingOptionResp) productdto.PricingForm {
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

// PricingSelectedProject 定价页的选中工程：**表单值 > 查询串 > 工程清单第一个**。
//
// 顺序是这一屏的结论而不是实现细节：试算 / 应用失败后回到本页时，用户看的是刚才那个工程，
// 不能悄悄跳回默认工程。单独成函数是为了能在**没有库**的情况下钉住这条顺序 ——
// 读模型后半段要读留痕台账，纯单元测试跑不到那里。
func PricingSelectedProject(req *productdto.ProductPricingPageReq, projects []projectdto.ProjectResp) string {
	if req != nil {
		if req.Form != nil {
			if v := strings.TrimSpace(req.Form.ProjectID); v != "" {
				return v
			}
		}
		if v := strings.TrimSpace(req.ProjectID); v != "" {
			return v
		}
	}
	if len(projects) > 0 {
		return projects[0].ID
	}
	return ""
}

// ProductPricingPage 定价工具页的读模型（本模块第六个）。
//
// 两处顺序有意保留（都与用户看到什么有关）：
//   - **选中工程**以表单里的 projectId 为准（提交带着它），否则才看查询串、再看第一个工程 ——
//     试算/应用失败后回到本页时，用户看的是刚才那个工程，不能悄悄跳回默认工程；
//   - **表单初值**：本次提交过就用提交值（表单要留在眼前，否则「哪一项填错了」无从改起），
//     没提交过才用库里的默认初值。
func (s *Service) ProductPricingPage(ctx context.Context, req *productdto.ProductPricingPageReq) (*productdto.ProductPricingPageView, error) {
	view := &productdto.ProductPricingPageView{
		Title:          productenums.ProductPricingTitle,
		Menu:           "product-pricing",
		Projects:       []projectdto.ProjectResp{},
		PreviewSummary: map[string]any{},
		PreviewRows:    []map[string]any{},
		History:        []map[string]any{},
	}
	// 工程清单读失败 → 空下拉：定价页的主体是改价表单，不该因为一个下拉整页打不开。
	if list, err := s.project.List(ctx); err == nil {
		view.Projects = list
	}
	selected := PricingSelectedProject(req, view.Projects)
	view.SelectedProject = selected

	view.Rules = s.ListPricingRuleTypes(ctx)
	view.Roundings = s.ListPricingRoundingOptions(ctx)

	if req != nil && req.Form != nil {
		view.Form = *req.Form
	} else {
		view.Form = DefaultPricingForm(view.Rules, view.Roundings)
	}
	if view.Form.ProjectID == "" {
		view.Form.ProjectID = selected
	}

	if req != nil && req.Preview != nil {
		view.HasPreview = true
		view.PreviewSummary = pricingPreviewSummary(req.Preview)
		view.PreviewRows = pricingLineRows(req.Preview.Lines)
	}
	if req != nil {
		view.Err = req.ErrMsg
	}
	view.History = s.pricingHistory(ctx, selected)
	return view, nil
}

// pricingHistory 调价留痕（批次 + 逐变体明细）。
//
// 两级降级都是有意保留的：列表读不到 → 空台账；**单条明细读不到 → 退回批次本身的汇总** ——
// 一条明细的故障不该让整页打不开。
func (s *Service) pricingHistory(ctx context.Context, projectID string) (rows []map[string]any) {
	rows = []map[string]any{}
	if strings.TrimSpace(projectID) == "" {
		return rows
	}
	list, err := s.ListPriceAdjustments(ctx, &productdto.ListPriceAdjustmentReq{
		ProjectID: projectID, Limit: pricingHistoryLimit,
	})
	if err != nil {
		return rows
	}
	for _, a := range list {
		detail, derr := s.GetPriceAdjustment(ctx, &productdto.GetPriceAdjustmentReq{ID: a.ID})
		if derr != nil {
			rows = append(rows, pricingHistoryRow(a))
			continue
		}
		rows = append(rows, pricingHistoryRow(detail))
	}
	return rows
}

// pricingHistoryRow 批次 → 模板行（时间压成可读文本，明细逐条列出原价与新价）。
func pricingHistoryRow(a *productdto.PriceAdjustmentResp) map[string]any {
	items := make([]map[string]any, 0, len(a.Items))
	for _, it := range a.Items {
		items = append(items, map[string]any{
			"ProductName": it.ProductName, "SKUCode": it.SKUCode,
			"OldPrice": money.FormatYuan(it.OldPrice), "NewPrice": money.FormatYuan(it.NewPrice),
			"Diff": pricingSignedAmount(it.Diff),
		})
	}
	return map[string]any{
		"ID": a.ID, "RuleLabel": a.RuleLabel, "ScopeLabel": a.ScopeLabel,
		"RoundingLabel": a.RoundingLabel, "FilterLabel": a.FilterLabel,
		"ChangedCount": a.ChangedCount, "VariantCount": a.VariantCount,
		"Note": a.Note, "OperatorID": a.OperatorID,
		"CreatedAtText": pricingTimeLabel(a.CreatedAt),
		"Items":         items,
	}
}

// pricingLineRows 试算行 → 模板行（金额与状态档位由服务端算好）。
func pricingLineRows(lines []*productdto.PricingLineResp) []map[string]any {
	rows := make([]map[string]any, 0, len(lines))
	for _, l := range lines {
		if l == nil {
			continue
		}
		rows = append(rows, map[string]any{
			"ProductName": l.ProductName, "SKUCode": l.SKUCode,
			"CostPrice": money.FormatYuanOrDash(l.CostPrice),
			"OldPrice":  money.FormatYuan(l.OldPrice), "NewPrice": money.FormatYuan(l.NewPrice),
			"Diff":   pricingSignedAmount(l.NewPrice - l.OldPrice),
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
func pricingPreviewSummary(p *productdto.PricingPreviewResp) map[string]any {
	if p == nil {
		return map[string]any{}
	}
	return map[string]any{
		"RuleLabel":      p.RuleLabel,
		"RoundingLabel":  p.RoundingLabel,
		"ScopeLabel":     p.ScopeLabel,
		"TargetCount":    p.TargetCount,
		"ChangedCount":   p.ChangedCount,
		"UnchangedCount": p.UnchangedCount,
		"SkippedCount":   p.SkippedCount,
	}
}

// pricingSignedAmount 差值文本（正数带 + 号）。
func pricingSignedAmount(v float64) string {
	if v > 0 {
		return "+" + money.FormatYuan(v)
	}
	return money.FormatYuan(v)
}

// pricingTimeLabel RFC3339 → 后台展示文本（同样的压轴规则见标签页的重算时间）。
func pricingTimeLabel(at string) string {
	if strings.TrimSpace(at) == "" {
		return "—"
	}
	if t, err := time.Parse(time.RFC3339, at); err == nil {
		return t.Local().Format("2006-01-02 15:04")
	}
	return at
}

// pricingLineTone 价格行状态 → 徽标分档。
//
// 判据与模板里的 IsChanged 同源（productenums.PricingLineChanged）：改过的价格行要人复核 → info。
func pricingLineTone(status string) string {
	if status == productenums.PricingLineChanged {
		return "info"
	}
	return "mute"
}
