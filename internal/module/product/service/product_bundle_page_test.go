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
	if view == nil {
		t.Fatal("失败路径的视图也必须非 nil（控制器不再补 nil 判断）")
	}
	if view.HTTPStatus != http.StatusBadRequest || view.PageStatus() != http.StatusBadRequest {
		t.Fatalf("未知工程应 400，实际 %d", view.HTTPStatus)
	}
	if !view.PageNoStore() {
		t.Fatal("400 档必须 no-store（重试要真的回到服务端）")
	}
	// 失败页上这一屏仍有表单：成员清单 / 候选 / 来源面板都得给齐键，保存入口撤掉。
	if view.Rows == nil || view.CandidateSKUs == nil || view.Sources == nil ||
		view.SourceProducts == nil || view.SourceWarehouses == nil {
		t.Fatalf("失败页也要给齐模板必需键（空切片而不是 nil）：%+v", view)
	}
	if !view.SaveBlocked {
		t.Fatal("取数失败时必须 SaveBlocked（否则运营会照着一张读不出来的空表提交，把配置写坏）")
	}
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
	if view.HTTPStatus != http.StatusInternalServerError || !view.PageNoStore() {
		t.Fatalf("工程清单失败应 500 + no-store：%+v", view)
	}
	if !view.SaveBlocked {
		t.Fatal("读不出来时必须禁止保存")
	}
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
	if view.HTTPStatus != http.StatusInternalServerError || !view.PageNoStore() {
		t.Fatalf("（既有行为）应 500 + no-store，实际 %d", view.HTTPStatus)
	}
	if !view.SaveBlocked {
		t.Fatal("读不出来时必须禁止保存")
	}
	// 来源面板的上下文与接线前一致（默认「从商品导入」，而不是被清空）：
	// 清空会让面板在失败页上多一处「来源未知」，而它其实只是默认值。
	if view.Source != productenums.BundleSourceProduct {
		t.Fatalf("来源默认值应与接线前一致，实际 %q", view.Source)
	}
}

// TestBundleSourceProductOptionsShape 来源商品下拉的形状契约（与 productPickOptions 同一判据）。
//
// 模板的判据是 `{{if isset(p.LabelKey)}}` —— isset 问的是**键在不在**，不是「值空不空」：
// 数据行（商品名，它不是文案、没有词条）一旦带上 `LabelKey: ""`，isset 即为真，
// 于是走取词分支渲染出**空白 option**：页面 200、结构完整、没有任何报错。
// 这条判据两个方向都守：空项没给非空 LabelKey → 失败；数据行带上了键 → 也失败。
//
// 它随装配一起从控制器搬到这里（原来在 product_dropdown_option_shape_test.go），
// 因为装配现在是这一层的职责。
func TestBundleSourceProductOptionsShape(t *testing.T) {
	products := []*productdto.ProductResp{{ID: "p-1", Name: "衬衫 A"}, {ID: "p-2", Name: "裤装 B"}}
	out := bundleSourceProductOptions(products, "p-2")
	if len(out) != 3 {
		t.Fatalf("应为「空项 + 2 个商品」，实际 %d 项", len(out))
	}
	if key, _ := out[0]["LabelKey"].(string); key == "" {
		t.Fatal("空项必须给非空 LabelKey（它是一句词条，取词在模板）")
	}
	if out[0]["Selected"] != false {
		t.Fatal("选了 p-2 时空项不该是选中态")
	}
	for i, row := range out[1:] {
		if _, ok := row["LabelKey"]; ok {
			t.Fatalf("数据项 %d 带上了 LabelKey 键 —— 同 productPickOptions，整键省略", i)
		}
		if row["Label"] != products[i].Name {
			t.Fatalf("数据项 %d 的 Label 应为 %q，实际 %v", i, products[i].Name, row["Label"])
		}
		if row["Selected"] != (row["ID"] == "p-2") {
			t.Fatalf("数据项 %d 的选中态不对：%v", i, row["Selected"])
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
		if out[i]["Value"] != want {
			t.Fatalf("第 %d 项的值应为 %q，实际 %v", i, want, out[i]["Value"])
		}
		if key, _ := out[i]["LabelKey"].(string); key == "" {
			t.Fatalf("第 %d 项必须给非空 LabelKey（取词在模板）", i)
		}
		if fallback, _ := out[i]["LabelFallback"].(string); fallback == "" {
			t.Fatalf("第 %d 项必须给中文兜底（缺词时不能渲染成空白）", i)
		}
	}
	// 选中态只跟着传进来的那一个值走。
	for i, want := range wantValues {
		if out[i]["Selected"] != (want == productenums.BundleSourceWarehouse) {
			t.Fatalf("第 %d 项选中态不对：%v", i, out[i]["Selected"])
		}
	}
}
