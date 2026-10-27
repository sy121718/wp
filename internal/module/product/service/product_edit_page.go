// product_edit_page.go — 商品编辑整页的读模型（取数编排 + 页级事实）。
//
// 与前三屏（新建 / 列表 / 详情）同一形态：一屏一个方法，失败档位由视图带出，控制器只剩
// 「读参数 → 调这里 → 渲染」。那三屏的完整理由在这里同样成立，不重复。
//
// 这一屏特有的两条：
//   - **属性组候选读失败不构成整页失败**：它是表单里的一块选择器，读不到时页面照常渲染，
//     但要把保存入口撤掉（控制器据 `AttributeOptionsErr` 置 EditBlocked）——
//     「选择器缺失」与「用户主动取消勾选全部」在 POST 上同形，读故障时不能装作后者。
//     原始错误随视图带出，由控制器按当前语言归口：本层不认识 HTTP，也不做取词。
//   - **没有表单回显**：保存已迁 JSON 写基座，失败不会带着提交值回来重渲染。
//
// 取数顺序与详情页**刻意不同**：这里是 分类 / 品牌 / 标签 / 仓库 **先**、商品详情**后**
// （详情页反过来）。顺序决定「谁先失败」—— 也就是用户看到的那一句话，所以逐字保留。
package productservice

import (
	"context"
	"strings"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	projectdto "go_wp/internal/module/project/dto"
)

// ProductEditPage 商品编辑整页的读模型（取数编排 + 页级事实）。
func (s *Service) ProductEditPage(ctx context.Context, req *productdto.ProductEditPageReq) (res *productdto.ProductEditPageResp, err error) {
	view := &productdto.ProductEditPageResp{
		Projects:            []projectdto.ProjectResp{},
		WarehouseOptions:    []productdto.WarehouseOptionResp{},
		WarehouseSKUOptions: []productdto.WarehouseSKUGroupResp{},
		AttributeOptions:    []productdto.AttributeOptionResp{},
		Categories:          []*productdto.CategoryResp{},
		Brands:              []*productdto.BrandResp{},
		Tags:                []*productdto.TagResp{},
	}
	var firstErr error
	fail := func(e error) {
		if firstErr == nil {
			firstErr = e
		}
	}

	projects, perr := s.project.List(ctx)
	if perr != nil {
		fail(perr)
	} else {
		view.Projects = projects
	}

	selected := ""
	productID := ""
	if req != nil {
		selected = strings.TrimSpace(req.ProjectID)
		productID = strings.TrimSpace(req.ProductID)
	}
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	view.SelectedProject = selected
	view.ProductID = productID

	if firstErr == nil && selected != "" && !ProjectExists(projects, selected) {
		firstErr = productenums.Err(productenums.ErrInvalidParam)
	}

	// 分类树 / 品牌 / 标签 / 仓库各取一次：行数据复用同一个 productRow 组装 ——
	// 两页各算一遍必然分叉（分类下拉的层级缩进、品牌名、标签勾选态都从这里来）。
	// 每条都套 firstErr == nil：前面读失败之后，后面的派生查询一条都不发。
	ready := firstErr == nil && selected != "" && productID != ""
	if ready {
		if tree, cerr := s.ListCategories(ctx, &productdto.ListCategoryReq{ProjectID: selected}); cerr != nil {
			fail(cerr)
		} else if tree != nil {
			view.Categories = tree
		}
	}
	if firstErr == nil && ready {
		if brands, berr := s.ListBrands(ctx, &productdto.ListBrandReq{ProjectID: selected}); berr != nil {
			fail(berr)
		} else if brands != nil {
			view.Brands = brands
		}
	}
	if firstErr == nil && ready {
		if tags, terr := s.ListTags(ctx, &productdto.ListTagReq{ProjectID: selected}); terr != nil {
			fail(terr)
		} else if tags != nil {
			view.Tags = tags
		}
	}
	// 仓库清单要进模板：变体的两个抽屉（新建 / 生成组合）用它渲染归属仓下拉。
	// 端口未注入 = 降级（与接线前的 `h.inventories == nil` 同一档），不是失败。
	if firstErr == nil && ready && s.pageWarehouses != nil {
		if rows, werr := s.pageWarehouses.WarehouseOptions(ctx, selected); werr != nil {
			fail(werr)
		} else {
			view.WarehouseOptions = rows
			// 每仓的候选货（B8 仓库明细的「添加」入口）：与新建页同一份取数（同一个端口），
			// 编辑页此前只取仓库清单，没有候选货 —— 于是没法在编辑态往别的仓加一条货行。
			if groups, gerr := s.pageWarehouses.WarehouseSKUOptions(ctx, selected, rows); gerr != nil {
				fail(gerr)
			} else {
				view.WarehouseSKUOptions = groups
			}
		}
	}

	// 商品三档（与详情页同一套判据与同一条 isProductErrKey）。
	// 注意：这里**没给 product 参数不是 404**，而是「读到了、没有这一条」的空态 ——
	// 编辑页是写入口，让运营看到一个空表单比看到一个 404 更有用（与详情页的取舍不同）。
	if firstErr == nil && ready {
		detail, derr := s.Get(ctx, &productdto.GetReq{ID: productID, ProjectID: selected})
		switch {
		case derr == nil && detail != nil:
			view.Detail = detail
			view.HasProduct = true
		case isProductErrKey(derr, productenums.ErrNotFound):
			firstErr = productenums.Err(productenums.ErrNotFound)
		case isProductErrKey(derr, productenums.ErrInvalidParam):
			firstErr = productenums.Err(productenums.ErrInvalidParam)
		default:
			// 其余（数据库 / 连接 / 未知错误，以及 detail 为 nil 的畸形返回）：500 + no-store。
			if derr == nil {
				derr = productenums.Err(productenums.ErrNotFound)
			}
			fail(derr)
		}
	}

	// 属性组勾选列表：**读失败不让整页失败**（见文件头）。原始错误交给控制器归口。
	if firstErr == nil && view.HasProduct {
		list, aerr := s.ListAttributes(ctx, &productdto.ListAttributeReq{ProjectID: selected, Size: PageAttributeOptionSize})
		if aerr != nil {
			view.AttributeOptionsErr = aerr
		} else {
			view.AttributeOptions = AttributeOptionViews(list)
		}
	}

	// 行装配：成品行在**读模型**里产出（控制器不再拼行）。条件与原控制器一致 —— 取数全部
	// 成功、商品存在、详情非 nil 才装配；失败时不装配：原料可能是半截的，渲染它只会得到
	// 一张残表（列表页逐行取详情失败会 break）。
	// 评分读不到（productRow 返回的 error）**不吞**：与列表路径同一档，页面据此走 500 失败页，
	// 而不是把数据库故障渲染成「暂无评分」（dd.txt 3.5 验收表最后一行）。
	if firstErr == nil && view.HasProduct && view.Detail != nil {
		row, rerr := s.productRow(ctx, selected, FlattenCategoryTree(view.Categories),
			view.Brands, view.Tags, view.WarehouseOptions, view.Detail)
		if rerr != nil {
			fail(rerr)
		} else {
			view.Product = row
			// 仓库明细（B8）：编辑表单要按仓列出「已挂的货行 + 当前在库」，这一屏才有消费方。
			s.fillVariantListStocksForRow(ctx, selected, row)
		}
	}
	if firstErr != nil {
		return nil, firstErr
	}
	// 回列表的链接与「取消」都回到用户来的地方（工程上下文保留）。
	view.BackURL = ProductListFilterURL(selected, "", "")
	FillProductEditFormEcho(view)
	if view.HasProduct {
		view.TplPanel = s.detailTemplatePanel(ctx, selected, view.ProductID)
	}
	return view, nil
}
