// product_attributes_page.go — 商品属性库整页的读模型（取数编排 + 页级事实）。
//
// 与 ProductListPage 同一形态：一屏一个方法，失败档位由视图带出，控制器只剩
// 「读参数 → 调这里 → 渲染」。那一屏的完整理由（为什么取数编排属于业务结论、
// 为什么失败之后一条派生取数都不发、为什么不写第二份失败页）在这里同样成立，不重复。
//
// **本方法只搬取数，不搬纯函数**：属性行的两个抽屉表单（编辑属性组 / 保存属性值）由控制器
// 用 `attrRowDrawerForms` 组装 —— 它们要 `*gin.Context`（csrf 取值 + text.Inject
// 注入取词函数 + 失败回填三键），而读模型不认识请求。所以这里交**原料**
// （`Attributes []*AttributeResp`），不做行装配。
//
// 顺序与原控制器逐字对齐（工程 → 参数档位 → 计数 → 页码收敛 → 取页），因为「谁先失败」
// 决定了用户看到哪一句：工程清单读不到是 500，`?project=<不存在的 id>` 是 400，
// 两者都能让后面的取数失败，但只有第一处失败原因会被说出来。
//
// **先计数再取页**（原控制器注释保留了理由）：反过来时越界页码会让 service 返回空页，
// 而分页条按收敛后的页码渲染 ——「表格为空、分页条却显示第 2 页」正是
// product_list_paging_test.go 要挡的那种自相矛盾组合。两次查询条数一样，不额外付代价。
package productservice

import (
	"context"
	"strings"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/pkg/pagination"
)

// ProductSubListPageSize 商品域后台**子列表**（属性 / 分类 / 标签 / 品牌）的每页条数。
//
// 20 与商品列表（ProductListPageSize）同一量级：这几页的表格都带行内抽屉与批量勾选，
// 一页塞太多等于把「翻页」换成「滚动回去找刚才那一行」。
const ProductSubListPageSize = 20

// ClampPageToTotal 把页码收敛到实际总页数以内（total=0 时收敛到第 1 页）。
//
// 为什么收敛发生在取数**之前**：越界页码（手输 URL、书签失效、上一次筛选后残留的页码）
// 直接传给取数层时，service 会老老实实返回一个空页，而分页条按收敛后的页码渲染 ——
// 「表格为空、分页条却显示第 2 页」就是这么来的（product_list_paging_test.go 钉住这条）。
//
// 唯一实现在 pkg/pagination.ClampPage（本函数只是模块内既有调用点的入口，不再自己算一遍）；
// 差别只在于**调用时机**：这里发生在取数之前。
func ClampPageToTotal(page, size int, total int64) int {
	return pagination.ClampPage(page, size, total)
}

// ProductAttributesPage 属性库整页的读模型：工程清单 → 工程存在性校验 → 计数 →
// 页码收敛 → 取页，并给出这一屏的档位。
//
// variation 的归一在这里（不是控制器）：它只认 "1"（参与变体）/ "0"（不参与）/ 其余当全部，
// 与 service 的 ListAttributeReq.Variation 是同一套值，也与表格「变体」列的口径一致 ——
// 归一写在控制器时，筛选回显与过滤条件会各拿一份原始串。
func (s *Service) ProductAttributesPage(ctx context.Context, req *productdto.ProductAttributesPageReq) (res *productdto.ProductAttributesPageResp, err error) {
	view := &productdto.ProductAttributesPageResp{
		// Title / Menu / AttributeRows / AttrCreateForm / Pagination / ListQuery 由控制器填
		//（行装配与抽屉数据要 gin.Context，见文件头）。这里只写**取数结果**。
		Projects:         []projectdto.ProjectResp{},
		AttributeDetails: []*productdto.AttributeResp{},
		Limit:            ProductSubListPageSize,
	}
	// firstErr 只记**第一处**失败：后面的步骤据此跳过，末尾据此给失败槽与档位。
	var firstErr error
	fail := func(e error) {
		if firstErr == nil {
			firstErr = e
			// 取数没读到 = 500。参数不合法是另一档（下面的工程校验）。
		}
	}

	projects, perr := s.project.List(ctx)
	if perr != nil {
		fail(perr)
	} else {
		view.Projects = projects
	}

	selected := ""
	keyword := ""
	variation := ""
	page := 1
	if req != nil {
		selected = strings.TrimSpace(req.ProjectID)
		keyword = strings.TrimSpace(req.Keyword)
		variation = strings.TrimSpace(req.Variation)
		if req.Page > 0 {
			page = req.Page
		}
	}
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	view.SelectedProject = selected
	// Variation 取值 ""（全部）/ "1"（参与变体）/ "0"（不参与）：其余一律当「全部」，
	// 否则 `?variation=yes` 会被当成一个永远不会命中的筛选值（页面显示「筛出来是空的」）。
	if variation != "1" && variation != "0" {
		variation = ""
	}
	view.FilterKeyword = keyword
	view.FilterVariation = variation
	view.Filtered = keyword != "" || variation != ""
	view.Page = page

	// 查询串不是可信边界：`?project=<不存在的 id>` 一路渲染下去会得到「这个工程下没有属性组」
	// —— 把「工程不存在」说成了「工程是空的」。判据是**本次请求已加载的工程集合**，与页面里的
	// 工程选择器同源（判据唯一一份就是下面这个 ProjectExists，各页不再各写一遍）。
	if firstErr == nil && selected != "" && !ProjectExists(projects, selected) {
		firstErr = productenums.Err(productenums.ErrInvalidParam)
	}

	// 计数与列表**同一份过滤条件**：两处口径分叉（关键词 / variation 只归一在一侧）时
	// 「共 N 条」与实际能翻出来的条数对不上。所以过滤条件只构造一次。
	if firstErr == nil && selected != "" {
		filterReq := &productdto.ListAttributeReq{ProjectID: selected, Keyword: keyword, Variation: variation}
		total, cerr := s.CountAttributes(ctx, filterReq)
		if cerr != nil {
			fail(cerr)
		} else {
			view.Total = total
			view.Page = ClampPageToTotal(page, ProductSubListPageSize, total)
			listReq := *filterReq
			listReq.Page, listReq.Size = view.Page, ProductSubListPageSize
			list, lerr := s.ListAttributes(ctx, &listReq)
			if lerr != nil {
				fail(lerr)
			} else if list != nil {
				view.AttributeDetails = list
			}
		}
	}

	if firstErr != nil {
		return nil, firstErr
	}
	view.Pagination = pagination.Page{Total: view.Total, Page: view.Page, Limit: view.Limit}
	return view, nil
}
