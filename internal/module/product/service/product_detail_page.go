// product_detail_page.go — 商品详情整页的读模型（取数编排 + 页级事实）。
//
// 与 product_page.go / product_list_page.go 同一形态：一屏一个方法，失败档位由视图带出，
// 控制器只剩「读参数 → 调这里 → 渲染」。那两屏的完整理由（为什么取数编排属于业务结论、
// 为什么失败之后一条派生取数都不发、为什么不写第二份失败页）在这里同样成立，不重复。
//
// 这一屏特有的一条：**商品三档**。业务上的「不存在」= 404（模板自带空态，它不是失败）、
// 参数不合法 = 400、其余（数据库 / 连接 / 超时 / 畸形返回）= 500 + no-store。
// 三档此前混在一句 `derr == nil && detail != nil` 里，「读不出来」被一起吞成了「商品不存在」
// —— 用户以为商品被删了，而它只是这一层没读到。
//
// 行装配（productRow）仍留在控制器：它产出 gin.H、要取词函数、内部还会读一次评分，
// 且列表页 / 编辑页 / 本页三处共用同一份组装 —— 本方法只把**原料**（Detail / Categories /
// Brands / Tags / WarehouseOptions）交给它。理由与 product_list_page.go 的文件头同一套。
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

// ProductDetailPage 商品详情整页的读模型（取数编排 + 页级事实）。
//
// 顺序与原控制器逐字对齐（工程 → 参数档位 → 仓库清单 → 商品详情 → 分类 / 品牌 / 标签），
// 因为「谁先失败」决定了用户看到哪一档：工程清单读不到是 500，`?project=<不存在的 id>` 是
// 400，商品不存在是 404，三者都能让后面的取数失败，但只有第一处原因会被说出来。
func (s *Service) ProductDetailPage(ctx context.Context, req *productdto.ProductDetailPageReq) (res *productdto.ProductDetailPageView, err error) {
	view := &productdto.ProductDetailPageView{
		Projects:         []projectdto.ProjectResp{},
		WarehouseOptions: []productdto.WarehouseOptionView{},
		Categories:       []*productdto.CategoryResp{},
		Brands:           []*productdto.BrandResp{},
		Tags:             []*productdto.TagResp{},
		HTTPStatus:       200,
	}
	// firstErr 只记**第一处**失败：后面的步骤据此跳过，末尾据此给失败槽与档位。
	var firstErr error
	fail := func(e error) {
		if firstErr == nil {
			firstErr = e
			// 取数没读到 = 500。404 / 400 是另外两档，不经过这里。
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

	// 与列表页同一档参数校验：?project=<不存在的 id> 不该渲染成「这个工程下没有商品」。
	if firstErr == nil && selected != "" && !ProjectExists(projects, selected) {
		view.HTTPStatus = 400
		firstErr = errors.New(productenums.ErrInvalidParam)
	}

	// 仓库清单：只读页不用下拉，但行数据组装（productRow 的库存列）要按工程仓库清单
	// 补齐未入库的仓 —— 与编辑页 / 列表页共用同一份组装，取数口径不能分叉。
	// 端口未注入 = 降级（与接线前的 `h.inventories == nil` 同一档），不是失败。
	if firstErr == nil && s.pageWarehouses != nil {
		rows, werr := s.pageWarehouses.WarehouseOptions(ctx, selected)
		if werr != nil {
			fail(werr)
		} else {
			view.WarehouseOptions = rows
		}
	}

	switch {
	case firstErr != nil:
		// 前面的取数已经失败：这一页的结论已定，不再为它取商品数据。
	case productID == "":
		// 没给 product 参数：这一页没有可读的对象，与「商品已被删除」是同一档（404）。
		// 手输 URL、书签失效、商品刚被删都会走到这里；模板自带的空态就是它的文案。
		view.HTTPStatus = 404
	default:
		detail, derr := s.Get(ctx, &productdto.GetReq{ID: productID})
		switch {
		case derr == nil && detail != nil:
			view.Detail = detail
			// 分类树 / 品牌 / 标签：与 productRow 共用同一份原料 —— 三页各算一遍必然分叉
			//（分类下拉的层级缩进、品牌名、标签勾选态都从这里来）。
			if tree, cerr := s.ListCategories(ctx, &productdto.ListCategoryReq{ProjectID: selected}); cerr != nil {
				fail(cerr)
			} else if tree != nil {
				view.Categories = tree
			}
			if brands, berr := s.ListBrands(ctx, &productdto.ListBrandReq{ProjectID: selected}); berr != nil {
				fail(berr)
			} else if brands != nil {
				view.Brands = brands
			}
			if tags, terr := s.ListTags(ctx, &productdto.ListTagReq{ProjectID: selected}); terr != nil {
				fail(terr)
			} else if tags != nil {
				view.Tags = tags
			}
			if firstErr == nil {
				view.HasProduct = true
			}
		case isProductErrKey(derr, productenums.ErrNotFound):
			// 业务上明确的「不存在」：404 + 同一页 + 模板自带的空态。
			// 不进失败槽（LoadFailed 保持为假）—— 那是「读到了、库里没有」，不是「没读到」。
			view.HTTPStatus = 404
		case isProductErrKey(derr, productenums.ErrInvalidParam):
			// 参数本身不合法（id 形态不对等）：400 + 同一页。
			view.HTTPStatus = 400
			firstErr = errors.New(productenums.ErrInvalidParam)
		default:
			// 其余（数据库 / 连接 / 未知错误，以及 detail 为 nil 的畸形返回）：500 + no-store。
			// 「读不出来」与「不存在」必须分开：前者重试可能就成功了，后者再试也没用。
			if derr == nil {
				derr = fmt.Errorf("商品 %s 的详情为空", productID)
			}
			fail(derr)
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

// isProductErrKey 错误的业务 key 是否等于 want。
//
// 本模块的错误一律是 `errors.New(productenums.ErrXxx)`（错误 message 就是 i18n key，
// 见 enums 的文件头），所以判据读 message。控制器侧的 productErrKeyOf 做的是同一件事
// （它多一层「文案兜底表」用于渲染），两侧口径一致。
//
// 用 errors.Is 判不了：每个 `errors.New` 都是新实例，没有可比较的 sentinel。
// 换成 sentinel 是独立一批的事（要动 service 全部抛错点），不在这一批的范围内。
func isProductErrKey(err error, want string) bool {
	return err != nil && want != "" && err.Error() == want
}
