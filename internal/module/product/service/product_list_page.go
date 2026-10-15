// product_list_page.go — 商品列表整页的读模型（取数编排 + 页级事实）。
//
// 与 product_page.go 的 ProductNewPage 同一形态：一屏一个方法，失败档位由视图带出，
// 控制器只剩「读参数 → 调这里 → 渲染」。那一屏的完整理由（为什么取数编排属于业务结论、
// 为什么失败之后一条派生取数都不发、为什么不写第二份失败页）在这里同样成立，不重复。
//
// **本方法既搬取数也搬行装配**（行装配见文件末尾与 service/product_row.go）。仍留在控制器
// 的只有两件跨页共用的工具：页码归一（productPageNumber，属性页 / 分类标签页同口径）与
// 状态下拉（productStatusOptions，编辑页也用）。它们搬进本包等于让那几页各留一份或反向
// import 控制器包，两条路都比留在原地差；改价表单初值已随定价页一起下沉进本包
// （controller 拿 Rules / Roundings 调 DefaultPricingForm）。
//
// 这一屏与新建页的三个不同点：
//   - **分页与筛选**：keyword / status / page 全走查询串（服务端渲染、零 JS）。页码由
//     控制器归一后传进来（`productPageNumber` 是跨页共用的工具），本方法只做下限保护；
//     总数与列表用**同一份过滤条件** —— 两处各写一遍 WHERE 时，「共 N 条」与实际能翻出来
//     的条数对不上，而两边单独看都对；
//   - **行装配在本方法里完成**：每个商品一行（service/product_row.go），与详情页 / 编辑页
//     共用同一份组装。原料（RowDetails / Categories / Brands / Tags / WarehouseOptions）
//     仍留在视图里 —— 分类选择器按树读，行本身则已经装配进 Products；嵌套值保持
//     map[string]any 的形状（模板的 isset 判据依赖它，见 dto.ProductRow 的文件头）；
//   - **分页条只给事实**：Total / Page / Limit 由本方法给，基地址与链接由控制器按当前
//     请求拼（基地址带筛选条件，翻页不丢条件）。
//
// 逐行取详情是**有意的 N+1**（一页 20 条，后台页可接受），但**读不到就整页失败**：
// 静默 continue 会让页面少一行却仍写着「共 N 条」，用户据此以为商品已经被删了。
package productservice

import (
	"context"
	"errors"
	"fmt"
	"strings"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	projectdto "go_wp/internal/module/project/dto"
)

// ProductListPageSize 商品列表每页条数（下沉前写在控制器，口径不变）。
const ProductListPageSize = 20

// ProductListPage 商品列表整页的读模型（取数编排 + 页级事实）。
//
// 顺序与原控制器逐字对齐（工程 → 参数档位 → 分类树 / 品牌 / 标签 / 仓库清单 → 列表与总数
// → 逐行详情 → 属性组选项 / 仓库候选 → 定价规则清单），因为「谁先失败」决定了用户看到哪一句：
// 工程清单读不到是 500，`?project=<不存在的 id>` 是 400，两者都能让后面的取数失败，
// 但只有第一处失败原因会被说出来。
func (s *Service) ProductListPage(ctx context.Context, req *productdto.ProductListPageReq) (res *productdto.ProductListPageView, err error) {
	view := &productdto.ProductListPageView{
		// Title / Menu / Form / Statuses / Pagination / ListQuery 都由控制器填：title 的
		// i18n key 常量在 http 包（MsgProductsTitle），派生值的计算工具还被别的页面共用。
		// Products（成品行）由本方法末尾装配，Flat 已随行装配下沉、视图不再有那个字段。
		Projects:            []projectdto.ProjectResp{},
		WarehouseOptions:    []productdto.WarehouseOptionView{},
		AttributeOptions:    []productdto.AttributeOptionView{},
		WarehouseSKUOptions: []productdto.WarehouseSKUGroupView{},
		Categories:          []*productdto.CategoryResp{},
		Brands:              []*productdto.BrandResp{},
		Tags:                []*productdto.TagResp{},
		RowDetails:          []*productdto.ProductResp{},
		Rules:               []*productdto.PricingRuleTypeResp{},
		Roundings:           []*productdto.PricingRoundingOptionResp{},
		Limit:               ProductListPageSize,
		HTTPStatus:          200,
	}
	// firstErr 只记**第一处**失败：后面的步骤据此跳过，末尾据此给失败槽与档位。
	var firstErr error
	fail := func(e error) {
		if firstErr == nil {
			firstErr = e
			// 取数没读到 = 500。参数不合法是另一档（下面的工程校验）。
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
	if req != nil {
		selected = strings.TrimSpace(req.ProjectID)
	}
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	view.SelectedProject = selected

	// 查询串不是可信边界：`?project=<不存在的 id>` 一路渲染下去会得到「这个工程下没有商品」
	// —— 把「工程不存在」说成了「工程是空的」。参数级失败仍是**页面请求**：整页 + 400 +
	// no-store（档位由视图带出，见 dto.ProductListPageView.PageStatus）。
	if firstErr == nil && selected != "" && !ProjectExists(projects, selected) {
		view.HTTPStatus = 400
		firstErr = errors.New(productenums.ErrInvalidParam)
	}

	keyword := ""
	status := ""
	page := 1
	if req != nil {
		keyword = strings.TrimSpace(req.Keyword)
		status = strings.TrimSpace(req.Status)
		if req.Page > 0 {
			page = req.Page
		}
	}
	view.FilterKeyword = keyword
	view.FilterStatus = status
	view.Page = page

	// 分类树与品牌列表一次取好：每个商品行都要渲染「挂哪些分类 / 主分类 / 品牌」，
	// 放在循环里取会变成 2×N 次查询。给**树**而不是扁平行 —— 扁平化（FlattenCategoryTree）
	// 归控制器那一步做，它要的是「分类树 → 渲染前的派生值」。
	if firstErr == nil {
		tree, cerr := s.ListCategories(ctx, &productdto.ListCategoryReq{ProjectID: selected})
		if cerr != nil {
			fail(cerr)
		} else if tree != nil {
			view.Categories = tree
		}
	}
	if firstErr == nil {
		brands, berr := s.ListBrands(ctx, &productdto.ListBrandReq{ProjectID: selected})
		if berr != nil {
			fail(berr)
		} else if brands != nil {
			view.Brands = brands
		}
	}
	// 标签一次取好（issue #11）：每个商品行要渲染「挂哪些手工标签 / 命中了哪些自动标签」，
	// 放在循环里取会变成 N 次查询。
	if firstErr == nil {
		tags, terr := s.ListTags(ctx, &productdto.ListTagReq{ProjectID: selected})
		if terr != nil {
			fail(terr)
		} else if tags != nil {
			view.Tags = tags
		}
	}
	// 归属仓下拉（issue #15）与多仓勾选：变体 / 首建商品的归属仓在这里选（不选 = 默认仓）。
	// **必须在商品行之前取好**：列表「库存」列的分仓明细要按工程仓库清单补齐未入库的仓。
	// 端口未注入 = 降级（与接线前的 `h.inventories == nil` 同一档），不是失败。
	if firstErr == nil && s.pageWarehouses != nil {
		rows, werr := s.pageWarehouses.WarehouseOptions(ctx, selected)
		if werr != nil {
			fail(werr)
		} else {
			view.WarehouseOptions = rows
		}
	}

	// 分页总数与列表**同一份过滤条件**（CountProducts 与 List 共用 model 的关键词 / 状态条件）：
	// 两处口径分叉时「共 N 条」与实际能翻出来的条数对不上。
	if firstErr == nil && selected != "" {
		list, lerr := s.List(ctx, &productdto.ListReq{
			ProjectID: selected, Keyword: keyword, Status: status, Page: page, Size: ProductListPageSize,
		})
		if lerr != nil {
			fail(lerr)
		} else {
			total, cerr := s.CountProducts(ctx, &productdto.ListReq{
				ProjectID: selected, Keyword: keyword, Status: status,
			})
			if cerr != nil {
				fail(cerr)
			} else {
				view.Total = total
			}
			for _, p := range list {
				// 列表项不含变体明细，逐个取详情（一页 20 条，后台页可接受）。
				detail, derr := s.Get(ctx, &productdto.GetReq{ID: p.ID})
				if derr != nil || detail == nil {
					// 这一行是**主数据**的一部分：静默 continue 会让页面少一行却仍写着「共 N 条」，
					// 用户据此以为商品已经被删了（而它只是这一行读不出来）。取不到就整页失败。
					if derr == nil {
						derr = fmt.Errorf("商品 %s 的详情为空", p.ID)
					}
					fail(fmt.Errorf("读取商品 %s 的详情失败：%w", p.ID, derr))
					break
				}
				view.RowDetails = append(view.RowDetails, detail)
			}
		}
	}

	// 属性组勾选列表：新建抽屉引用属性组时不再手打 UUID —— 可选值由服务端给出
	// （与归属仓同一手法），用户看到的是一排「颜色（color）」，而不是一个暗示 UUID 的输入框。
	if firstErr == nil {
		list, aerr := s.ListAttributes(ctx, &productdto.ListAttributeReq{ProjectID: selected, Size: PageAttributeOptionSize})
		if aerr != nil {
			fail(aerr)
		} else {
			view.AttributeOptions = AttributeOptionViews(list)
		}
	}
	// 「从仓库选」的候选（docs/14 §1.1 入口 A，迁移 251）：按仓分组的仓库 SKU 清单。
	// 前端只是便捷入口 —— 服务端在 Create 里会带着仓库 id 再复核一遍（不信任前端）。
	if firstErr == nil && s.pageWarehouses != nil {
		groups, gerr := s.pageWarehouses.WarehouseSKUOptions(ctx, selected, view.WarehouseOptions)
		if gerr != nil {
			fail(gerr)
		} else {
			view.WarehouseSKUOptions = groups
		}
	}

	// 批量改价抽屉的规则清单：抽屉与独立定价页共用同一个规则表单片段，键名或取值来源
	// 分叉会让其中一处渲染成空下拉。**表单初值不在这里算** —— 控制器拿 Rules / Roundings
	// 调 DefaultPricingForm 自己算（一份实现：独立定价页与批量改价抽屉共用）。
	view.Rules = s.ListPricingRuleTypes(ctx)
	view.Roundings = s.ListPricingRoundingOptions(ctx)

	// 行装配：成品行在**读模型**里产出（控制器不再拼行）。只在取数全部成功时装配 ——
	// 逐行取详情失败时 RowDetails 是半截的（见上面的 break），渲染它只会得到一张残表。
	// 分类树在这里扁平化一次（只服务行装配），控制器不再需要那份扁平行。
	if firstErr == nil {
		flat := FlattenCategoryTree(view.Categories)
		rows := make([]productdto.ProductRow, 0, len(view.RowDetails))
		for _, detail := range view.RowDetails {
			if row := s.productRow(ctx, selected, flat, view.Brands, view.Tags, view.WarehouseOptions, detail); row != nil {
				rows = append(rows, *row)
			}
		}
		view.Products = rows
	}

	if firstErr != nil {
		// 失败页给齐模板必需键：上面每个字段都有值（空列表而不是缺失），渲染不会断在
		// 「失败分支只设了错误键」上（internal/templates/CLAUDE.md §错误分支也必须给齐必需键）。
		view.LoadFailed = true
		return view, firstErr
	}
	return view, nil
}
