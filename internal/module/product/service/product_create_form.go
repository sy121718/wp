// product_create_form.go — 商品建表单的读模型（仓库 / 属性组 / 每仓 SKU 候选）。
//
// 「表单」不是另一种响应形态：后端给数据、Jet 渲染 HTML。这份视图撑的是**用数据渲染出来的
// 那一段 HTML** —— 首屏整页与提交失败后的重渲染共用同一份装配，所以取数编排只有这里一处。
//
// 与四个整页读模型同一形态：视图恒非 nil、失败带 LoadFailed 与 HTTPStatus、失败后不再发派生取数。
// 两处不同，都写在 ProductCreateFormView 的文件注释里（没有工程清单；多一组请求边界的回填值）。
package productservice

import (
	"context"
	"strings"

	productdto "go_wp/internal/module/product/dto"
)

// ProductCreateForm 商品建表单的取数：仓库清单 → 属性组候选 → 每仓 SKU 候选。
//
// 顺序有意保留为串行（每仓 SKU 候选依赖上一步拿到的仓库清单）：并发化会让「仓库读失败」
// 与「SKU 读失败」同时发生时的结论变得不确定，而这里要的是**第一处失败**。
func (s *Service) ProductCreateForm(ctx context.Context, req *productdto.ProductCreateFormReq) (*productdto.ProductCreateFormView, error) {
	view := &productdto.ProductCreateFormView{
		WarehouseOptions:    []productdto.WarehouseOptionView{},
		AttributeOptions:    []productdto.AttributeOptionView{},
		WarehouseSKUOptions: []productdto.WarehouseSKUGroupView{},
		HTTPStatus:          200,
	}
	projectID := ""
	if req != nil {
		projectID = strings.TrimSpace(req.ProjectID)
	}
	view.SelectedProject = projectID
	if projectID == "" {
		// 没给工程：表单照常渲染，候选一律空列表（与四个整页「空工程是空态不是失败」同一档）。
		return view, nil
	}
	// 仓库端口未注入 = 降级（与接线前的 `h.warehouses == nil` 同一档）：表单仍渲染，只是没有归属仓下拉。
	if s.pageWarehouses == nil {
		return view, nil
	}
	warehouses, err := s.pageWarehouses.WarehouseOptions(ctx, projectID)
	if err != nil {
		return failedCreateForm(view), err
	}
	view.WarehouseOptions = warehouses

	list, err := s.ListAttributes(ctx, &productdto.ListAttributeReq{ProjectID: projectID, Size: PageAttributeOptionSize})
	if err != nil {
		return failedCreateForm(view), err
	}
	view.AttributeOptions = AttributeOptionViews(list)

	groups, err := s.pageWarehouses.WarehouseSKUOptions(ctx, projectID, warehouses)
	if err != nil {
		return failedCreateForm(view), err
	}
	view.WarehouseSKUOptions = groups
	return view, nil
}

// failedCreateForm 取数失败档：视图仍非 nil（渲染方据此决定退回整页提示），列表键回退成空列表。
//
// **不把 err 写进 LoadError**：视图是往模板去的形状，驱动原文只该经 err 返回值进日志
// （渲染方拿到 err 就知道失败，不必读视图里的字符串 —— 写进去只会给将来「顺手渲染它」留个口子）。
func failedCreateForm(view *productdto.ProductCreateFormView) *productdto.ProductCreateFormView {
	view.LoadFailed = true
	view.HTTPStatus = 500
	return view
}
