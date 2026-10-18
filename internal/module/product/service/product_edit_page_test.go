package productservice

// 商品编辑页读模型（整页 GET 取数下沉）的档位与短路 —— 与新建页（product_page_test.go）
// 同一族判据，这一批把控制器里原来的三条下移到了这里：
//
//	工程清单读不出来            → 500（不是空态、也不是 400）
//	?project=<不存在的 id>      → 400（判据是本次请求已加载的工程集合）
//	没给 ?product 参数          → **200 空表单**（不是 404）
//
// 第三条是本屏与详情页**刻意不同**的一处：编辑页是写入口，让运营看到一个空表单比看到一个
// 404 有用（详情页反过来 —— 那是只读页，读到没有就该说没有）。
//
// 「商品不存在 → 404」这一档要走到 Get（model），真实库由
// public/test/product/feature/product_edit_page_test.go 覆盖。

import (
	"context"
	"errors"
	"net/http"
	"testing"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	projectdto "go_wp/internal/module/project/dto"
)

// TestProductEditPageInvalidProjectStopsBeforeFanout 非法工程 = 400 + 一条派生取数都不发。
func TestProductEditPageInvalidProjectStopsBeforeFanout(t *testing.T) {
	s, wh, tpl := newProductPageReadModel(productPageProjectStub{
		rows: []projectdto.ProjectResp{{ID: "p1", Name: "工程一"}},
	})
	view, err := s.ProductEditPage(context.Background(), &productdto.ProductEditPageReq{
		ProjectID: "does-not-exist", ProductID: "prod1",
	})
	if err == nil || err.Error() != productenums.ErrInvalidParam {
		t.Fatalf("非法工程应返回 %s，实际 err=%v", productenums.ErrInvalidParam, err)
	}
	wantPageFailure(t, "非法工程", view, err, http.StatusBadRequest)
	if wh.calls != 0 || tpl.calls != 0 {
		t.Fatalf("非法工程不该发任何派生取数，实际仓库端口 %d 次、模板端口 %d 次", wh.calls, tpl.calls)
	}
}

// TestProductEditPageProjectReadFailureIsFiveHundred 工程清单读不出来 = 500 + 列表键给空而不是缺键。
func TestProductEditPageProjectReadFailureIsFiveHundred(t *testing.T) {
	s, wh, tpl := newProductPageReadModel(productPageProjectStub{
		err: errors.New(`pq: relation "projects" does not exist`),
	})
	view, err := s.ProductEditPage(context.Background(), &productdto.ProductEditPageReq{
		ProjectID: "p1", ProductID: "prod1",
	})
	if err == nil {
		t.Fatal("工程清单读不出来必须返回错误")
	}
	wantPageFailure(t, "工程清单读不到", view, err, http.StatusInternalServerError)
	if wh.calls != 0 || tpl.calls != 0 {
		t.Fatalf("主数据失败后不该继续查，实际仓库端口 %d 次、模板端口 %d 次", wh.calls, tpl.calls)
	}
}

// TestProductEditPageWithoutProductParamIsEmptyState 没给 ?product = 200 空表单（不是 404）。
//
// 反面判据（改了会红）：把这一档也做成 404 的话，运营从菜单直接进编辑页会看到「商品不存在」，
// 而他要的正是「新建一个」。
func TestProductEditPageWithoutProductParamIsEmptyState(t *testing.T) {
	s, wh, tpl := newProductPageReadModel(productPageProjectStub{
		rows: []projectdto.ProjectResp{{ID: "p1", Name: "工程一"}},
	})
	view, err := s.ProductEditPage(context.Background(), &productdto.ProductEditPageReq{ProjectID: "p1"})
	if err != nil {
		t.Fatalf("没给 product 参数不该报错，实际 %v", err)
	}
	if view == nil {
		t.Fatal("视图恒非 nil")
	}
	if view.HasProduct {
		t.Fatal("没给 product 参数时不能是「已读到商品」")
	}
	// 没给商品就没有可变的东西：这一档连分类 / 品牌 / 标签都不查（商品清单本身没意义）。
	if wh.calls != 0 || tpl.calls != 0 {
		t.Fatalf("空表单档不该发派生取数，实际仓库端口 %d 次、模板端口 %d 次", wh.calls, tpl.calls)
	}
}
