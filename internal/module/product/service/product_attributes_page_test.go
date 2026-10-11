package productservice

// 属性库整页读模型（service.ProductAttributesPage）的档位与归一。
//
// 本文件的桩**只给工程清单**（`newProductPageReadModel` 的 model 是 nil）：于是
// 「一条派生取数都没发」成了可断言的事实 —— 真发了 `CountAttributes` 会打到 nil model 上
// 直接 panic，而不是静默返回零值。计数之后的取页要真实库，由
// public/test/product/feature 的属性页用例覆盖（product_attribute_page_test.go）。

import (
	"context"
	"errors"
	"testing"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	projectdto "go_wp/internal/module/project/dto"
)

// TestProductAttributesPageUnknownProjectStopsBeforeFanout 未知工程 = 400 + 一条派生取数都不发。
//
// 把 `?project=<不存在的 id>` 做成 200 会让页面显示「这个工程下没有属性组」——
// 而真实事实是「这个工程不存在」，正确动作是换一个工程，不是去新建属性组。
func TestProductAttributesPageUnknownProjectStopsBeforeFanout(t *testing.T) {
	s, _, _ := newProductPageReadModel(productPageProjectStub{
		rows: []projectdto.ProjectResp{{ID: "p1", Name: "工程一"}},
	})
	view, err := s.ProductAttributesPage(context.Background(), &productdto.ProductAttributesPageReq{ProjectID: "does-not-exist"})
	if err == nil || err.Error() != productenums.ErrInvalidParam {
		t.Fatalf("未知工程应返回 %s，实际 err=%v", productenums.ErrInvalidParam, err)
	}
	if view == nil {
		t.Fatal("失败路径的视图也必须非 nil（控制器不再补 nil 判断）")
	}
	if view.HTTPStatus != 400 || view.PageStatus() != 400 {
		t.Fatalf("未知工程应 400（参数不合法），实际 %d", view.HTTPStatus)
	}
	if !view.LoadFailed || !view.PageNoStore() {
		t.Fatalf("失败页必须带 LoadFailed + no-store，实际 LoadFailed=%v", view.LoadFailed)
	}
	if view.AttributeDetails == nil {
		t.Fatal("失败页也要给齐模板必需键（空切片而不是 nil）；成品行 Attributes 由控制器装配")
	}
}

// TestProductAttributesPageProjectListFailureIsFiveHundred 工程清单读不到 = 500。
func TestProductAttributesPageProjectListFailureIsFiveHundred(t *testing.T) {
	s, _, _ := newProductPageReadModel(productPageProjectStub{
		err: errors.New(`pq: relation "projects" does not exist`),
	})
	view, err := s.ProductAttributesPage(context.Background(), &productdto.ProductAttributesPageReq{})
	if err == nil {
		t.Fatal("工程清单读不到必须返回错误")
	}
	if view == nil || view.HTTPStatus != 500 || !view.LoadFailed || !view.PageNoStore() {
		t.Fatalf("工程清单失败应 500 + LoadFailed + no-store：%+v", view)
	}
}

// TestProductAttributesPageVariationNormalization variation 只认 1 / 0，其余当「全部」。
//
// 归一错时 `?variation=yes` 会被当成一个永远命中不了的筛选值：页面显示「没有匹配的属性组」，
// 用户以为自己筛错了，其实是这个参数根本没有对应的档位。
func TestProductAttributesPageVariationNormalization(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"1", "1"},
		{"0", "0"},
		{"yes", ""},
		{"", ""},
		{"  ", ""},
	} {
		t.Run("variation="+tc.in, func(t *testing.T) {
			// 工程清单给空：selected 为空 → 计数与取页整段跳过（不碰 nil model）。
			s, _, _ := newProductPageReadModel(productPageProjectStub{})
			view, err := s.ProductAttributesPage(context.Background(), &productdto.ProductAttributesPageReq{Variation: tc.in})
			if err != nil {
				t.Fatalf("空工程清单不是失败：%v", err)
			}
			if view.FilterVariation != tc.want {
				t.Fatalf("variation=%q 归一后应为 %q，实际 %q", tc.in, tc.want, view.FilterVariation)
			}
			// 「筛过没有」的判据要跟着归一后的值走，否则空结果会被说成「筛出来是空的」。
			if wantFiltered := tc.want != ""; view.Filtered != wantFiltered {
				t.Fatalf("variation=%q 时 Filtered 应为 %v，实际 %v", tc.in, wantFiltered, view.Filtered)
			}
		})
	}
}

// TestClampPageToTotalOrder 页码收敛与 pkg/pagination.Facts 同规则：越界夹到末页、0 页夹到第 1 页。
//
// 收敛发生在**取数之前**：不收敛时越界页码会取回一个空页，而分页条按收敛后的页码渲染 ——
// 「表格为空、分页条却显示第 2 页」正是这条判据要挡的组合。
func TestClampPageToTotalOrder(t *testing.T) {
	for _, tc := range []struct {
		page  int
		total int64
		want  int
	}{
		{page: 0, total: 100, want: 1},
		{page: -3, total: 100, want: 1},
		{page: 2, total: 100, want: 2},
		{page: 5, total: 100, want: 5},
		{page: 6, total: 100, want: 5},
		{page: 999, total: 100, want: 5},
		{page: 3, total: 0, want: 1},
		{page: 1, total: 21, want: 1},
	} {
		if got := ClampPageToTotal(tc.page, ProductSubListPageSize, tc.total); got != tc.want {
			t.Errorf("ClampPageToTotal(%d, %d, %d) = %d，want %d",
				tc.page, ProductSubListPageSize, tc.total, got, tc.want)
		}
	}
}
