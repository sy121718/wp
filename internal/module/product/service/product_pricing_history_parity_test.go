package productservice_test

// product_pricing_history_parity_test.go — 台账成品行与**逐批次 Get 明细**口径的对账
// （dd.txt 第八批 8.1「价格结果与原规则一致」在台账这一屏的等价物）。
//
// 改造把 pricingHistory 的「每批一次 GetPriceAdjustment」换成「一次批量读明细 + 按
// adjustment_id 分组装配」，风险是**行内容悄悄变了**：模板 product_pricing.html 读的是
// RuleLabel / ScopeLabel / RoundingLabel / ChangedCount / VariantCount / CreatedAtText /
// OperatorID / FilterLabel / Note，以及明细逐条的 ProductName / SKUCode / OldPrice /
// NewPrice / Diff。少任何一项都不报错，只会让某一列变空。
//
// 对账单位是**整行**（含 Items 逐条）：①读模型 view.History；②按原实现在测试内重建
// 期望行（ListPriceAdjustments + 逐批 GetPriceAdjustment）。②里的金额与时间文本用同源
// 口径复现 —— money.FormatYuan 是导出函数，「正数带 + 号」与「RFC3339 → 本地
// 2006-01-02 15:04」两条规则在此复述一次（它们是台账这条口径的全部内容）。
//
// 堵住的两种假绿：
//   · 两边都空也相等 —— 先做夹具自检（明细条数、商品名有无、金额与时间文本非空）；
//   · 只比批次行、漏掉明细 —— 逐条比 Items 的顺序与字段。
//
// 表结构来自生产迁移，不手抄 CREATE TABLE。

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	productdto "go_wp/internal/module/product/dto"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/pkg/money"
	"go_wp/public/test/support"
)

// formatSignedYuan 差值文本（正数带 + 号）—— 与台账装配同一条口径的复述。
func formatSignedYuan(v float64) string {
	if v > 0 {
		return "+" + money.FormatYuan(v)
	}
	return money.FormatYuan(v)
}

// formatTimeLabel RFC3339 → 后台展示文本 —— 同上，复述台账的时间口径。
func formatTimeLabel(at string) string {
	if strings.TrimSpace(at) == "" {
		return "—"
	}
	if parsed, err := time.Parse(time.RFC3339, at); err == nil {
		return parsed.Local().Format("2006-01-02 15:04")
	}
	return at
}

// referencePricingHistoryRows 按**原实现**（逐批次 Get 明细）重建台账行。
//
// GetPriceAdjustmentReq 显式带 ProjectID：原调用点不传、靠「唯一工程」兜底，
// 本用例的工程桩只有一个工程，两条路径解析出同一个作用域。
func referencePricingHistoryRows(t *testing.T, s *productservice.Service, ctx context.Context, projectID string) []map[string]any {
	t.Helper()
	// 10 = 台账最多列几个批次（service 的 pricingHistoryLimit，未导出；夹具批数远小于它）。
	list, err := s.ListPriceAdjustments(ctx, &productdto.ListPriceAdjustmentReq{ProjectID: projectID, Limit: 10})
	if err != nil {
		t.Fatalf("参照实现：留痕列表读失败：%v", err)
	}
	rows := make([]map[string]any, 0, len(list))
	for _, a := range list {
		detail, derr := s.GetPriceAdjustment(ctx, &productdto.GetPriceAdjustmentReq{ID: a.ID, ProjectID: projectID})
		src := a
		if derr == nil {
			src = detail
		}
		items := make([]map[string]any, 0, len(src.Items))
		for _, it := range src.Items {
			items = append(items, map[string]any{
				"ProductName": it.ProductName, "SKUCode": it.SKUCode,
				"OldPrice": money.FormatYuan(it.OldPrice), "NewPrice": money.FormatYuan(it.NewPrice),
				"Diff": formatSignedYuan(it.Diff),
			})
		}
		rows = append(rows, map[string]any{
			"ID": src.ID, "RuleLabel": src.RuleLabel, "ScopeLabel": src.ScopeLabel,
			"RoundingLabel": src.RoundingLabel, "FilterLabel": src.FilterLabel,
			"ChangedCount": src.ChangedCount, "VariantCount": src.VariantCount,
			"Note": src.Note, "OperatorID": src.OperatorID,
			"CreatedAtText": formatTimeLabel(src.CreatedAt),
			"Items":         items,
		})
	}
	return rows
}

// historyJSON 台账行 → 可比对的文本（失败信息里给全量证据，而不是只说「不一致」）。
func historyJSON(t *testing.T, rows []map[string]any) string {
	t.Helper()
	data, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		t.Fatalf("台账行序列化失败：%v", err)
	}
	return string(data)
}

// TestProductPricingHistoryRowsMatchPerBatchDetailPath 三批台账（2 条明细 / 空批次 / 3 条明细）：
// 读模型产出的行与逐批次 Get 口径**逐字段一致**。
func TestProductPricingHistoryRowsMatchPerBatchDetailPath(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "台账行对账")
	// 空批次那一档是「这一批没有逐变体明细」的模板分支；第 3 条明细的商品已删除，
	// 商品名该留空（留痕快照仍可读）。
	seedPricingHistoryFixture(t, db, projectID, []int{2, 0, 3})

	s := productservice.NewService(productmodel.NewModel(db), listPageProjectStub{
		rows: []projectdto.ProjectResp{{ID: projectID, Name: "台账行对账"}},
	})
	ctx := context.Background()

	view, err := s.ProductPricingPage(ctx, &productdto.ProductPricingPageReq{ProjectID: projectID})
	if err != nil {
		t.Fatalf("台账读模型失败：%v", err)
	}
	if len(view.History) != 3 {
		t.Fatalf("台账 %d 行，期望 3 行（含空批次）", len(view.History))
	}

	// 夹具自检：被比较的字段不能两边都空，否则对账是假绿。
	named, blank, total := 0, 0, 0
	for i, row := range view.History {
		where := fmt.Sprintf("第 %d 行", i+1)
		for _, field := range []string{"RuleLabel", "ScopeLabel", "RoundingLabel", "CreatedAtText"} {
			text, _ := row[field].(string)
			if text == "" || text == "—" {
				t.Fatalf("夹具自检失败：%s 的 %s 是空文本（%q）", where, field, text)
			}
		}
		if note, _ := row["Note"].(string); note == "" {
			t.Fatalf("夹具自检失败：%s 的备注是空文本", where)
		}
		items := historyItemsOf(t, row, where)
		total += len(items)
		for _, it := range items {
			for _, field := range []string{"SKUCode", "OldPrice", "NewPrice", "Diff"} {
				text, _ := it[field].(string)
				if text == "" {
					t.Fatalf("夹具自检失败：%s 明细的 %s 是空文本", where, field)
				}
			}
			if name, _ := it["ProductName"].(string); name == "" {
				blank++
			} else {
				named++
			}
		}
		// 明细顺序是这一屏的口径之一：同一批内按 sku_code 升序（夹具按倒序插入）。
		for j := 1; j < len(items); j++ {
			prev, _ := items[j-1]["SKUCode"].(string)
			cur, _ := items[j]["SKUCode"].(string)
			if prev > cur {
				t.Fatalf("夹具自检失败：%s 的明细不是按 SKU 升序（%q 在 %q 之前）", where, prev, cur)
			}
		}
	}
	if total != 5 || named == 0 || blank == 0 {
		t.Fatalf("夹具自检失败：明细 %d 条（期望 5）、有商品名 %d 条、无商品名 %d 条（两边都要有）", total, named, blank)
	}

	want := referencePricingHistoryRows(t, s, ctx, projectID)
	if !reflect.DeepEqual(view.History, want) {
		t.Fatalf("台账行与逐批次 Get 口径不一致：读模型 = %s；参照实现 = %s",
			historyJSON(t, view.History), historyJSON(t, want))
	}
}
