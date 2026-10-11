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
//   - **表单回显（echo）不在这里**：校验失败重渲染要用**这次提交的值**覆盖读回来的值，
//     而提交值在请求体里 —— 那是控制器的事（inbound/http 的 formEchoFrom）。
//
// 取数顺序与详情页**刻意不同**：这里是 分类 / 品牌 / 标签 / 仓库 **先**、商品详情**后**
// （详情页反过来）。顺序决定「谁先失败」—— 也就是用户看到的那一句话，所以逐字保留。
package productservice

import (
	"context"
	"errors"
	"strings"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	projectdto "go_wp/internal/module/project/dto"
)

// ProductEditPage 商品编辑整页的读模型（取数编排 + 页级事实）。
func (s *Service) ProductEditPage(ctx context.Context, req *productdto.ProductEditPageReq) (res *productdto.ProductEditPageView, err error) {
	view := &productdto.ProductEditPageView{
		Projects:         []projectdto.ProjectResp{},
		WarehouseOptions: []productdto.WarehouseOptionView{},
		AttributeOptions: []productdto.AttributeOptionView{},
		Categories:       []*productdto.CategoryResp{},
		Brands:           []*productdto.BrandResp{},
		Tags:             []*productdto.TagResp{},
		HTTPStatus:       200,
	}
	var firstErr error
	fail := func(e error) {
		if firstErr == nil {
			firstErr = e
			view.HTTPStatus = 500
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
		view.HTTPStatus = 400
		firstErr = errors.New(productenums.ErrInvalidParam)
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
			view.HTTPStatus = 404
		case isProductErrKey(derr, productenums.ErrInvalidParam):
			view.HTTPStatus = 400
			firstErr = errors.New(productenums.ErrInvalidParam)
		default:
			// 其余（数据库 / 连接 / 未知错误，以及 detail 为 nil 的畸形返回）：500 + no-store。
			if derr == nil {
				derr = errors.New(productenums.ErrNotFound)
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

	if firstErr != nil {
		// 失败页给齐模板必需键：上面每个字段都有值（空列表而不是缺失），渲染不会断在
		// 「失败分支只设了错误键」上（internal/templates/CLAUDE.md §错误分支也必须给齐必需键）。
		view.LoadFailed = true
		return view, firstErr
	}
	return view, nil
}
