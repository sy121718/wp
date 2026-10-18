package productservice

// product_bundle_page_test.go — 捆绑配置页读模型的档位与「不该碰端口」的判据。
//
// 与其余几屏同一手法：桩**只给工程清单**（`newProductPageReadModel` 的 model 是 nil），
// 于是「一条派生取数都没发」成了可断言的事实 —— 真发出去会打到 nil model 上直接 panic。
// 需要「商品 / 配置真的读到了」的判据（成员行装配、提交快照优先、来源面板候选）
// 在 public/test/product/feature/product_bundle_page_test.go，那里有真实库。
//
// 本文件另有一条**纯装配**判据（下拉项形状）：它是纯函数，不需要任何桩。

import (
	"context"
	"errors"
	"net/http"
	"testing"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	projectdto "go_wp/internal/module/project/dto"
)

// TestProductBundlePageUnknownProjectStopsBeforeFanout 未知工程 = 400 + 一条派生取数都不发。
//
// 把 `?project=<不存在的 id>` 做成 200 会让页面显示成「这个工程下没有可捆绑的商品」——
// 真实事实是工程不存在，正确动作是换一个工程。
func TestProductBundlePageUnknownProjectStopsBeforeFanout(t *testing.T) {
	s, _, _ := newProductPageReadModel(productPageProjectStub{
		rows: []projectdto.ProjectResp{{ID: "p1", Name: "工程一"}},
	})
	view, err := s.ProductBundlePage(context.Background(), &productdto.ProductBundlePageReq{
		ProjectID: "does-not-exist", ProductID: "prod-1",
	})
	if err == nil || err.Error() != productenums.ErrInvalidParam {
		t.Fatalf("未知工程应返回 %s，实际 err=%v", productenums.ErrInvalidParam, err)
	}
	wantPageFailure(t, "未知工程", view, err, http.StatusBadRequest)
}

// TestProductBundlePageProjectListFailureIsFiveHundred 工程清单读不到 = 500 + SaveBlocked。
func TestProductBundlePageProjectListFailureIsFiveHundred(t *testing.T) {
	s, _, _ := newProductPageReadModel(productPageProjectStub{
		err: errors.New(`pq: relation "projects" does not exist`),
	})
	view, err := s.ProductBundlePage(context.Background(), &productdto.ProductBundlePageReq{ProductID: "prod-1"})
	if err == nil {
		t.Fatal("工程清单读不到必须返回错误")
	}
	wantPageFailure(t, "工程清单读不到", view, err, http.StatusInternalServerError)
}

// TestProductBundlePageNoProjectIsFiveHundred 一条工程都没有时：**500**（既有行为，逐字保留）。
//
// 这条钉的是一个**既有缺口**而不是本批的设计：`ListBundleSKUs` 经 resolveProjectID 解析
// 空工程 id 会返回参数错误，于是全新部署（还没建工程）打开这一页看到的是失败页，
// 而不是「先去建一个工程」的空态。取数下沉时逐字保留了这个行为 —— 顺手把它改成 200
// 属于另一件事（要同时决定空态文案与「没有工程」与「工程读不出来」的档位区分），
// 记在这里是为了让它**可见**：改的那一天这条会变红，正好提醒一并改掉。
func TestProductBundlePageNoProjectIsFiveHundred(t *testing.T) {
	s, _, _ := newProductPageReadModel(productPageProjectStub{})
	view, err := s.ProductBundlePage(context.Background(), &productdto.ProductBundlePageReq{})
	if err == nil {
		t.Fatal("（既有行为）没有工程时读候选 SKU 会返回参数错误，这条判据钉住它")
	}
	// 空工程 id 落到 unresolved 参数错误（业务错误 → 400），与「工程清单读不到」分开。
	wantPageFailure(t, "没有工程", view, err, http.StatusBadRequest)
}

// TestBundleSourceProductOptionsShape 来源商品下拉的形状契约（与 productPickOptions 同一判据）。
//
// 空项与数据项的分野由 IsNone 显式表达：空项的文案是事实（LabelKey + 兜底，渲染期取词），
// 数据项是数据（商品名，翻不了）。这条判据两个方向都守 —— 空项必须 IsNone 且有 LabelKey，
// 数据项必须不是 IsNone 且没有 LabelKey，否则模板会把商品名当词条 key 去取词、渲染出空白 option。
func TestBundleSourceProductOptionsShape(t *testing.T) {
	products := []*productdto.ProductResp{{ID: "p-1", Name: "衬衫 A"}, {ID: "p-2", Name: "裤装 B"}}
	out := bundleSourceProductOptions(products, "p-2")
	if len(out) != 3 {
		t.Fatalf("应为「空项 + 2 个商品」，实际 %d 项", len(out))
	}
	if !out[0].IsNone || out[0].LabelKey == "" {
		t.Fatalf("空项必须以 IsNone 标记并给非空 LabelKey，实际 %+v", out[0])
	}
	if out[0].Selected {
		t.Fatal("选了 p-2 时空项不该是选中态")
	}
	for i, row := range out[1:] {
		if row.IsNone || row.LabelKey != "" {
			t.Fatalf("数据项 %d 不该是空项、也不该带 LabelKey，实际 %+v", i, row)
		}
		if row.Label != products[i].Name {
			t.Fatalf("数据项 %d 的 Label 应为 %q，实际 %q", i, products[i].Name, row.Label)
		}
		if row.Selected != (row.Value == "p-2") {
			t.Fatalf("数据项 %d 的选中态不对：%v", i, row.Selected)
		}
	}
}

// TestBundleSourceOptionsShape 来源下拉三项：值即 enums 常量，标签交事实（key + 兜底）。
//
// 值写错的表现是「保存后来源被服务端按未知来源拒绝」——页面上看不出哪里不对。
func TestBundleSourceOptionsShape(t *testing.T) {
	out := bundleSourceOptions(productenums.BundleSourceWarehouse)
	if len(out) != 3 {
		t.Fatalf("来源下拉应恒为三项，实际 %d 项", len(out))
	}
	wantValues := []string{
		productenums.BundleSourceProduct,
		productenums.BundleSourceWarehouse,
		productenums.BundleSourceAttributes,
	}
	for i, want := range wantValues {
		if out[i].Value != want {
			t.Fatalf("第 %d 项的值应为 %q，实际 %q", i, want, out[i].Value)
		}
		if out[i].LabelKey == "" {
			t.Fatalf("第 %d 项必须给非空 LabelKey（取词在模板）", i)
		}
		if out[i].LabelFallback == "" {
			t.Fatalf("第 %d 项必须给中文兜底（缺词时不能渲染成空白）", i)
		}
	}
	// 选中态只跟着传进来的那一个值走。
	for i, want := range wantValues {
		if out[i].Selected != (want == productenums.BundleSourceWarehouse) {
			t.Fatalf("第 %d 项选中态不对：%v", i, out[i].Selected)
		}
	}
}
