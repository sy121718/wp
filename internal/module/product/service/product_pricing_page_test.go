package productservice

// 定价页读模型（service.ProductPricingPage）的降级档与工程选中顺序。
//
// 这一屏**没有取数失败档**：三处取数各自降级（工程清单读不到 → 空下拉；规则与尾数处理是注册表
// 快照、本身无 error；留痕台账读不到 → 空台账），页面主体（改价表单）始终撑得起来。
// 所以这里的判据不是「失败给几档」，而是「降级之后页面还剩什么」。
//
// 留痕台账那一半要真实库，由 public/test/product/feature 的定价页用例覆盖；本文件只跑
// 「选中空工程 → 不查台账」这条路径（查了会打到 nil model 上）。

import (
	"context"
	"errors"
	"testing"

	productdto "go_wp/internal/module/product/dto"
	projectdto "go_wp/internal/module/project/dto"
)

// TestProductPricingPageDegradesWhenProjectListFails 工程清单读失败：空下拉，页面照常 200。
//
// 反面判据：把这一路也做成失败档会让「项目表暂时读不到」变成整页打不开 ——
// 而这一屏的主体是改价表单，它不依赖工程清单。
func TestProductPricingPageDegradesWhenProjectListFails(t *testing.T) {
	s, _, _ := newProductPageReadModel(productPageProjectStub{
		err: errors.New(`pq: relation "projects" does not exist`),
	})
	view, err := s.ProductPricingPage(context.Background(), &productdto.ProductPricingPageReq{})
	if err != nil {
		t.Fatalf("本页恒不返回错误（三处取数各自降级）：%v", err)
	}
	if view == nil {
		t.Fatal("视图恒非 nil")
	}
	if view.Projects == nil || len(view.Projects) != 0 {
		t.Fatalf("工程清单读失败要给空切片而不是 nil：%+v", view.Projects)
	}
	if view.SelectedProject != "" {
		t.Fatalf("没有工程可选时选中项应为空，实际 %q", view.SelectedProject)
	}
	// 注册表快照不受影响：参考表与表单初值仍要给齐，否则页面只剩一个空壳。
	if len(view.Rules) == 0 || len(view.Roundings) == 0 {
		t.Fatalf("规则与尾数处理是注册表快照，不该因工程清单失败而空：rules=%d roundings=%d",
			len(view.Rules), len(view.Roundings))
	}
	if view.Form.RuleType == "" {
		t.Fatal("表单初值应取第一条规则的类型（批量改价的典型用法）")
	}
	if view.Form.ProjectID != "" {
		t.Fatalf("没有工程可选时表单里的工程 id 应为空，实际 %q", view.Form.ProjectID)
	}
	if view.History == nil {
		t.Fatal("留痕台账要给空切片（模板按 len 判断空态）")
	}
	if view.HasPreview {
		t.Fatal("没提交试算时不该有试算结果块")
	}
}

// TestProductPricingPageKeepsSubmittedForm 试算失败原地重渲染：用提交值而不是默认初值。
//
// 反面判据：失败后回到默认初值，用户填的六项全丢 —— 而页面上没有任何痕迹说明「刚才试的是哪条规则」。
func TestProductPricingPageKeepsSubmittedForm(t *testing.T) {
	s, _, _ := newProductPageReadModel(productPageProjectStub{
		err: errors.New("project list unavailable"),
	})
	submitted := &productdto.PricingForm{
		RuleType:   "cost_multiple",
		Multiplier: "1.35",
		Note:       "这是本次提交的备注",
	}

	view, err := s.ProductPricingPage(context.Background(), &productdto.ProductPricingPageReq{Form: submitted})
	if err != nil {
		t.Fatalf("本页恒不返回错误：%v", err)
	}
	if view.Form != *submitted {
		t.Fatalf("有提交值时应原样回显，实际 %+v，want %+v", view.Form, *submitted)
	}
	if view.Form.Note != "这是本次提交的备注" {
		t.Fatalf("提交的备注丢了：%q", view.Form.Note)
	}
}

// TestProductPricingPagePreviewFlagsPreviewBlock 只有给了试算结果才渲染预览块。
func TestProductPricingPagePreviewFlagsPreviewBlock(t *testing.T) {
	s, _, _ := newProductPageReadModel(productPageProjectStub{
		err: errors.New("project list unavailable"),
	})
	view, err := s.ProductPricingPage(context.Background(), &productdto.ProductPricingPageReq{
		Preview: &productdto.PricingPreviewResp{
			RuleLabel: "成本倍数",
			Lines: []*productdto.PricingLineResp{
				{ProductName: "样例商品", OldPrice: 100, NewPrice: 135, Status: "changed", StatusLabel: "会改"},
			},
		},
	})
	if err != nil {
		t.Fatalf("本页恒不返回错误：%v", err)
	}
	if !view.HasPreview {
		t.Fatal("给了试算结果就必须打开预览块")
	}
	if len(view.PreviewRows) != 1 {
		t.Fatalf("试算明细行应逐行转成模板行，实际 %d 行", len(view.PreviewRows))
	}
	if view.PreviewSummary == nil || view.PreviewSummary["RuleLabel"] != "成本倍数" {
		t.Fatalf("汇总行要给模板用，实际 %+v", view.PreviewSummary)
	}
}

// TestPricingSelectedProjectOrder 选中工程：表单值 > 查询串 > 工程清单第一个。
//
// 顺序错的症状是「试算失败后页面悄悄跳到另一个工程」—— 用户会以为刚才那条规则改了别的工程。
func TestPricingSelectedProjectOrder(t *testing.T) {
	projects := []projectdto.ProjectResp{{ID: "first"}, {ID: "second"}}
	for _, tc := range []struct {
		name string
		req  *productdto.ProductPricingPageReq
		want string
	}{
		{"没有入参取第一个工程", &productdto.ProductPricingPageReq{}, "first"},
		{"查询串优先于工程清单", &productdto.ProductPricingPageReq{ProjectID: "second"}, "second"},
		{
			"表单值优先于查询串",
			&productdto.ProductPricingPageReq{ProjectID: "second", Form: &productdto.PricingForm{ProjectID: "first"}},
			"first",
		},
		{"空白值不算选中", &productdto.ProductPricingPageReq{ProjectID: "   "}, "first"},
		{"没有工程可选时为空", nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			projs := projects
			if tc.req == nil {
				projs = nil
			}
			if got := PricingSelectedProject(tc.req, projs); got != tc.want {
				t.Fatalf("选中工程 = %q，want %q", got, tc.want)
			}
		})
	}
}
