package productservice

// 商品建表单读模型（service.ProductCreateForm）的短路与降级。
//
// 「表单」不是另一种响应形态（后端给数据、Jet 渲染 HTML）：这份视图既给首屏用，也给提交失败后的
// 重渲染用，所以取数编排只有这一处 —— 它的短路档（谁先失败、失败之后还发不发后续查询）
// 只在 service 可见，必须在这里钉住。
//
// 成功路径（真的取到仓库 / 属性组 / 每仓 SKU 候选）要真实库，由
// public/test/product/feature 的建表单用例覆盖。

import (
	"context"
	"errors"
	"net/http"
	"testing"

	productdto "go_wp/internal/module/product/dto"
)

// TestProductCreateFormWithoutProjectSkipsFetches 没给工程：表单照常渲染，一条取数都不发。
func TestProductCreateFormWithoutProjectSkipsFetches(t *testing.T) {
	s, wh, tpl := newProductPageReadModel(productPageProjectStub{})
	view, err := s.ProductCreateForm(context.Background(), &productdto.ProductCreateFormReq{})
	if err != nil {
		t.Fatalf("没给工程不该报错：%v", err)
	}
	if view == nil {
		t.Fatal("视图恒非 nil")
	}
	if wh.calls != 0 || tpl.calls != 0 {
		t.Fatalf("没给工程不该发任何取数，实际仓库端口 %d 次、模板端口 %d 次", wh.calls, tpl.calls)
	}
	// 选项键必须在场且非 nil（模板用 isset + len 判断整块渲染，nil 切片会走成「缺数据」）。
	if view.WarehouseOptions == nil || view.AttributeOptions == nil || view.WarehouseSKUOptions == nil {
		t.Fatalf("空态也要给空切片而不是 nil：%+v", view)
	}
}

// TestProductCreateFormPortMissingIsDegraded 仓库端口未注入 = 降级（表单照常渲染，只是没有候选）。
//
// 这一档与「接线前的 h.warehouses == nil」同一口径：不是失败 —— 失败会让整块表单换成一页错误，
// 而这里少的是一个下拉的候选。
func TestProductCreateFormPortMissingIsDegraded(t *testing.T) {
	s := NewService(nil, productPageProjectStub{})
	view, err := s.ProductCreateForm(context.Background(), &productdto.ProductCreateFormReq{ProjectID: "p1"})
	if err != nil {
		t.Fatalf("端口未注入是降级不是失败，不该返回错误：%v", err)
	}
	if view == nil {
		t.Fatal("降级档仍要交成功视图")
	}
	if view.WarehouseOptions == nil || len(view.WarehouseOptions) != 0 {
		t.Fatalf("降级档的选项键要给空切片而不是 nil：%v", view.WarehouseOptions)
	}
}

// TestProductCreateFormWarehouseFailureStopsFanout 仓库清单读失败 = 500 + 后续取数一条不发。
//
// 每仓 SKU 候选依赖上一步拿到的仓库清单：仓库读失败还继续查，会把「读不出来」渲染成
// 「这个工程下没有可选项」—— 与四个整页的短路同判据，只是这一屏没有三档（表单撑不起来就退回提示）。
func TestProductCreateFormWarehouseFailureStopsFanout(t *testing.T) {
	s, wh, _ := newProductPageReadModel(productPageProjectStub{})
	wh.err = errors.New(`pq: relation "inventories" does not exist`)

	view, err := s.ProductCreateForm(context.Background(), &productdto.ProductCreateFormReq{ProjectID: "p1"})
	if err == nil {
		t.Fatal("仓库清单读失败必须返回错误（调用方据此退回整页提示，不留半个表单）")
	}
	wantPageFailure(t, "仓库清单读不到", view, err, http.StatusInternalServerError)
	if wh.calls != 1 {
		t.Fatalf("仓库读失败之后不该再发取数，实际仓库端口 %d 次（want 1：只有那次失败的调用）", wh.calls)
	}
}
