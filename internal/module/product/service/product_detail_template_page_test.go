package productservice

// product_detail_template_page_test.go — 「详情页模板」整页读模型的档位与取数顺序判据。
//
// 这一屏的可断言面全部在「谁先失败」与「什么算失败」上，所以桩是**端口桩**而不是
// service 内部对象：本文件要证明的是取数编排本身（标注类失败不参与判定、
// 非法工程上不发派生取数），内部调用链变了这些判据也不该变。
//
// 商品本体那一段用 `newProductPageReadModel` 的 model=nil 桩：**能跑通**的那些用例
// 都在 NoProduct 或失败分支上（它们不到商品取数），真发出去会打到 nil model 上直接 panic。
// 需要「商品读到了」的判据（模板清单是主数据、清���失败后派生一条都不发、选项行的
// IsBound / IsDefault）放在 public/test/product/feature/product_detail_template_choice_test.go ——
// 那里有真实库，真发一次商品查询是安全的。

import (
	"context"
	"errors"
	"net/http"
	"testing"

	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	projectdto "go_wp/internal/module/project/dto"
)

// detailTemplatePortStub 模板 / 实例端口桩。
//
// 本文件只放**不该碰这个端口**的三条判据，所以桩不提供任何失败注入 ——
// 它的作用就是记录调用：这三条里 calls 必须一直是空的（反面判据：
// 「非法工程上照样把模板清单查一遍」只有在记录调用之后才看得出来）。
// 需要「商品与模板都读到了」的判据在 public/test/product/feature，那里有真实库。
type detailTemplatePortStub struct {
	calls []string
}

func (p *detailTemplatePortStub) DetailTemplateCandidates(context.Context, string, string) ([]productcontract.DetailTemplateCandidateView, error) {
	p.calls = append(p.calls, "Candidates")
	return nil, nil
}

func (p *detailTemplatePortStub) DefaultDetailTemplate(context.Context, string, string) (string, error) {
	p.calls = append(p.calls, "Default")
	return "", nil
}

func (p *detailTemplatePortStub) BoundDetailTemplate(context.Context, string, string, string) productcontract.BoundTemplateView {
	p.calls = append(p.calls, "Bound")
	return productcontract.BoundTemplateView{}
}

func (p *detailTemplatePortStub) DetailTemplatePanel(context.Context, string, string, string) productcontract.DetailTemplatePanelFacts {
	p.calls = append(p.calls, "Panel")
	return productcontract.DetailTemplatePanelFacts{}
}

// newDetailTemplateReadModel 读模型：只接工程清单与模板端口。
func newDetailTemplateReadModel(projects []projectdto.ProjectResp, projectErr error, port productcontract.DetailTemplatePort) (*Service, *productPageProjectStub) {
	ps := productPageProjectStub{rows: projects, err: projectErr}
	s, _, _ := newProductPageReadModel(ps)
	if port != nil {
		s.SetDetailTemplatePort(port)
	}
	return s, &ps
}

// TestDetailTemplatePageNoProductStopsBeforeTemplateFetch 没给 product：置 NoProduct 且模板侧一条都不发。
//
// 菜单入口必然走到这里。若仍去取模板清单，页面会先渲染一屏模板再被控制器改成引导出口 ——
// 白发的两次查询，而且一旦模板侧报错误，引导就会被「装载失败」盖掉，用户看到的是
// 「数据读不出来」而不是「先去选一件商品」。
func TestDetailTemplatePageNoProductStopsBeforeTemplateFetch(t *testing.T) {
	port := &detailTemplatePortStub{}
	s, _ := newDetailTemplateReadModel([]projectdto.ProjectResp{{ID: "p1"}}, nil, port)
	view, err := s.ProductDetailTemplatePage(context.Background(), &productdto.ProductDetailTemplatePageReq{ProjectID: "p1"})
	if err != nil {
		t.Fatalf("没给 product 不是失败：%v", err)
	}
	if !view.NoProduct {
		t.Fatal("没给 product 应置 NoProduct（控制器据此走引导出口）")
	}
	if view.Ready {
		t.Fatal("NoProduct 档不该 Ready —— 那会让模板渲染出没有商品的清单")
	}
	if len(port.calls) != 0 {
		t.Fatalf("NoProduct 档不该碰模板端口，实际 %v", port.calls)
	}
}

// TestDetailTemplatePageUnknownProjectStopsBeforeProductFetch 未知工程：400 + 失败槽 + 零派生取数。
func TestDetailTemplatePageUnknownProjectStopsBeforeProductFetch(t *testing.T) {
	port := &detailTemplatePortStub{}
	s, _ := newDetailTemplateReadModel([]projectdto.ProjectResp{{ID: "p1"}}, nil, port)
	view, err := s.ProductDetailTemplatePage(context.Background(), &productdto.ProductDetailTemplatePageReq{
		ProjectID: "does-not-exist", ProductID: "prod-1",
	})
	if err == nil {
		t.Fatal("未知工程必须返回错误")
	}
	wantPageFailure(t, "未知工程", view, err, http.StatusBadRequest)
	if len(port.calls) != 0 {
		t.Fatalf("非法工程上继续查模板只会把「工程不存在」渲染成「没有模板」，实际 %v", port.calls)
	}
}

// TestDetailTemplatePageProjectListFailureIsFiveHundred 工程清单读不到：500 + 零派生取数。
func TestDetailTemplatePageProjectListFailureIsFiveHundred(t *testing.T) {
	port := &detailTemplatePortStub{}
	s, _ := newDetailTemplateReadModel(nil, errors.New(`pq: relation "projects" does not exist`), port)
	view, err := s.ProductDetailTemplatePage(context.Background(), &productdto.ProductDetailTemplatePageReq{ProductID: "prod-1"})
	if err == nil {
		t.Fatal("工程清单读不到必须返回错误")
	}
	wantPageFailure(t, "工程清单读不到", view, err, http.StatusInternalServerError)
	if len(port.calls) != 0 {
		t.Fatalf("工程清单失败后一条派生取数都不该发，实际 %v", port.calls)
	}
}
