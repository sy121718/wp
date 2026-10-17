// product_taxonomy_page.go — 商品分类 / 品牌 / 标签三页的读模型（取数编排 + 页级事实）。
//
// 与 ProductAttributesPage 同一形态（一屏一个方法、失败档位由视图带出、控制器只剩
// 「读参数 → 调这里 → 装配行与抽屉 → 渲染」），所以那一屏的完整理由在这里不重复：
// 取数编排是业务结论、失败之后一条派生取数都不发、失败页不另写一份。
//
// 三页共用同一套顺序（工程清单 → 参数档位 → 计数 → 页码收敛 → 取页），因为「谁先失败」
// 决定用户看到哪一句：工程清单读不到是 500，`?project=<不存在的 id>` 是 400。
//
// **先计数再取页**（三页一致）：反过来时越界页码会让取数层返回空页，而分页条按收敛后的
// 页码渲染 ——「表格为空、分页条却显示第 2 页」正是 product_list_paging_test.go 挡的那类
// 自相矛盾组合。
//
// **本文件只搬取数，不搬纯渲染**：行装配与三个抽屉（分类 / 品牌 / 标签的编辑片段）留在控制器 ——
// 它们要 `*gin.Context`（csrf 取值 + adminui.PrepareBare 注入取词函数），读模型不认识请求。
// 所以这里交**原料**（CategoryDetails / BrandDetails / TagDetails），不做成品行。
//
// 分类树的两块原料**分开**交（CategoryDetails 与 PickFlat）：页面表格要的是本页的树
// （扁平后按 Depth 缩进），父级下拉要的是**工程内全部分类** —— 树状列表之后没有面包屑兜底，
// 缺项会直接表现为「新建子分类时选不到父级」。
package productservice

import (
	"context"
	"strings"

	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	projectdto "go_wp/internal/module/project/dto"
)

// FlattenCategoryTree 分类树 → DFS 扁平行（父在前、子紧随，模板按 Depth 缩进）。
func FlattenCategoryTree(nodes []*productdto.CategoryResp) []*productdto.CategoryResp {
	out := make([]*productdto.CategoryResp, 0, len(nodes))
	var walk func(list []*productdto.CategoryResp)
	walk = func(list []*productdto.CategoryResp) {
		for _, n := range list {
			out = append(out, n)
			walk(n.Children)
		}
	}
	walk(nodes)
	return out
}

// CategoriesInProject 只保留属于 projectID 的分类（扁平列表按行自己的 ProjectID 过滤）。
//
// 判据是行自己的 ProjectID，**不依赖 RLS**：策略未切非超级角色时 ListCategories 会把别的
// 工程的分类一起返回，混进「本工程的父级候选」里 —— 分类是工程内实体，不该串门。
// 混进来会让「父级在别的工程」这种坏数据在界面上看起来完全正常。
func CategoriesInProject(flat []*productdto.CategoryResp, projectID string) []*productdto.CategoryResp {
	out := make([]*productdto.CategoryResp, 0, len(flat))
	for _, node := range flat {
		if node.ProjectID == projectID {
			out = append(out, node)
		}
	}
	return out
}

// CategoryLabel 分类的展示标签（按层级缩进；全角空格在 <option> 与列表里都成立）。
func CategoryLabel(node *productdto.CategoryResp) string {
	if node == nil || node.Depth <= 0 {
		if node == nil {
			return ""
		}
		return node.Name
	}
	return strings.Repeat("　", node.Depth) + node.Name
}

// MissingParentIDs 挑出本页里「父级不在候选集合」的那些父级 id（去重、保持出现顺序）。
//
// 纯逻辑单独成函数：候选集合的判定（不是一次读库）才是「谁会走模板那条额外分支」的判据，
// 单测在没有数据库时也能把它钉住。
//
// 判据刻意取「父级不在候选集合」而**不是**「父级名称读得到」：后者只收读得到的那些，
// RLS 切非超级角色后跨工程父级读不到名称会被它漏掉，而那时恰恰最该让操作者看见异常。
func MissingParentIDs(nodes []*productdto.CategoryResp, known map[string]struct{}) []string {
	seen := make(map[string]struct{}, len(nodes))
	var ids []string
	for _, node := range nodes {
		if node == nil {
			continue
		}
		parentID := strings.TrimSpace(node.ParentID)
		if parentID == "" {
			continue
		}
		if _, ok := known[parentID]; ok {
			continue
		}
		if _, seenAlready := seen[parentID]; seenAlready {
			continue
		}
		seen[parentID] = struct{}{}
		ids = append(ids, parentID)
	}
	return ids
}

// ProductCategoriesPage 分类管理页的读模型：工程清单 → 参数档位 → 本页树 → 全量候选 →
// 缺失父级的事实，并给出这一屏的档位。
func (s *Service) ProductCategoriesPage(ctx context.Context, req *productdto.ProductCategoriesPageReq) (*productdto.ProductCategoriesPageResp, error) {
	view := &productdto.ProductCategoriesPageResp{
		Projects:        []projectdto.ProjectResp{},
		CategoryDetails: []*productdto.CategoryResp{},
		PickFlat:        []*productdto.CategoryResp{},
		Limit:           ProductSubListPageSize,
		HTTPStatus:      200,
	}
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
	} else if projects != nil {
		view.Projects = projects
	}

	selected, keyword, page := "", "", 1
	if req != nil {
		selected = strings.TrimSpace(req.ProjectID)
		keyword = strings.TrimSpace(req.Keyword)
		if req.Page > 0 {
			page = req.Page
		}
	}
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	view.SelectedProject = selected
	view.FilterKeyword = keyword
	view.Filtered = keyword != ""
	view.Page = page

	// 查询串不是可信边界：`?project=<不存在的 id>` 一路渲染下去会得到「这个工程下没有分类」
	// —— 把「工程不存在」说成了「工程是空的」。判据是**本次请求已加载的工程集合**，与页面里的
	// 工程选择器同源。
	if firstErr == nil && selected != "" && !ProjectExists(projects, selected) {
		view.HTTPStatus = 400
		firstErr = productenums.Err(productenums.ErrInvalidParam)
	}

	// 每条取数都套 firstErr == nil：主数据或参数一旦判定失败，后面的派生查询一条都不发。
	if firstErr == nil && selected != "" {
		query := &productdto.ListCategoryPageReq{ProjectID: selected, Keyword: keyword, Page: page, Size: ProductSubListPageSize}
		result, lerr := s.ListCategoryPage(ctx, query)
		if lerr != nil {
			fail(lerr)
		} else if result != nil {
			view.Total, view.MatchTotal = result.Total, result.MatchTotal
			if clamped := ClampPageToTotal(page, ProductSubListPageSize, view.Total); clamped != page {
				view.Page = clamped
				query.Page = clamped
				if again, aerr := s.ListCategoryPage(ctx, query); aerr != nil {
					fail(aerr)
				} else if again != nil {
					view.Total, view.MatchTotal = again.Total, again.MatchTotal
					view.CategoryDetails = FlattenCategoryTree(again.Items)
				}
			} else {
				view.CategoryDetails = FlattenCategoryTree(result.Items)
			}
		}
	}

	// 父级下拉要的是**工程内全部分类**，不是当前这一页。
	if firstErr == nil && selected != "" {
		all, aerr := s.ListCategories(ctx, &productdto.ListCategoryReq{ProjectID: selected})
		if aerr != nil {
			fail(aerr)
		} else {
			view.PickFlat = FlattenCategoryTree(all)
		}
	}

	// 父级不在候选列表里（跨工程遗留 / 父级行已不存在）时，抽屉仍要渲染「当前选中的父级」
	// 那一项，并说明它为什么不在候选里 —— 那是操作者唯一的可见信号。
	if firstErr == nil {
		view.ParentLabels = s.categoryParentLabels(ctx, selected, view.CategoryDetails, CategoriesInProject(view.PickFlat, selected))
	}

	if firstErr != nil {
		// 失败页给齐模板必需键（空列表而不是缺失），渲染不会断在「失败分支只设了错误键」上。
		view.LoadFailed = true
		return view, firstErr
	}
	return view, nil
}

// categoryParentLabels 为「父级不在候选列表里」的分类补上父级事实（id → 名称 + 跨工程标记）。
//
// 候选列表是**本工程**分类的全集（见 CategoriesInProject），父级不在其中的情形有两种：
// 父级行已不存在，或父级属于别的工程（历史搬迁留下的坏数据）。两种都要让操作者看出来 ——
// 否则下拉里要么是一串裸 ID、要么看起来像个正常选项，用户以为层级没问题。
//
// 名称都读不到（父级行没了 / RLS 切角色后跨工程行不可见）时**不登记**这一项，模板退回显示
// 原始 ID —— 显示裸 ID 也比编一个父级名安全。逐个父级查一次库：只对本页里真正缺失的那几个发生。
func (s *Service) categoryParentLabels(ctx context.Context, projectID string, nodes []*productdto.CategoryResp, candidates []*productdto.CategoryResp) map[string]productdto.CategoryParentInfoResp {
	known := make(map[string]struct{}, len(candidates))
	for _, node := range candidates {
		if node != nil {
			known[node.ID] = struct{}{}
		}
	}
	return CategoryParentFacts(MissingParentIDs(nodes, known), projectID, func(id string) (*productdto.CategoryResp, error) {
		return s.GetCategory(ctx, &productdto.GetCategoryReq{ProjectID: projectID, ID: id})
	})
}

// CategoryParentFacts 按 id 逐个读父级、收成「名称 + 是否跨工程」的事实表。
//
// 读库那一步做成参数（load）是为了让这条判据在没有数据库时也能被钉住：
// 它真正管的是「同一父级只读一次」「读不到就不写键」（模板因此退回裸 ID），
// 而不是 SQL 本身。ids 的顺序即查询顺序。
func CategoryParentFacts(ids []string, projectID string, load func(id string) (*productdto.CategoryResp, error)) map[string]productdto.CategoryParentInfoResp {
	if len(ids) == 0 {
		return nil
	}
	labels := make(map[string]productdto.CategoryParentInfoResp, len(ids))
	for _, parentID := range ids {
		parent, err := load(parentID)
		if err != nil || parent == nil {
			// 读不到（父级行没了 / 不在作用域内）就不登记：模板退回显示原始 ID。
			continue
		}
		labels[parentID] = productdto.CategoryParentInfoResp{Name: parent.Name, OutOfScope: parent.ProjectID != projectID}
	}
	return labels
}

// ProductBrandsPage 品牌管理页的读模型：工程清单 → 参数档位 → 计数 → 页码收敛 → 取页。
//
// 品牌列表**分页下推到 service**（审计 D12 收口）：请求类型自带 Page/Size，总数由契约的
// CountBrands 给出 —— 不再「全量取回再切片」，翻到第 N 页也只从库里取那一页。
// 关键词过滤仍在查询里（与计数同一份过滤条件）。
func (s *Service) ProductBrandsPage(ctx context.Context, req *productdto.ProductBrandsPageReq) (*productdto.ProductBrandsPageResp, error) {
	view := &productdto.ProductBrandsPageResp{
		Projects:     []projectdto.ProjectResp{},
		BrandDetails: []*productdto.BrandResp{},
		Limit:        ProductSubListPageSize,
		HTTPStatus:   200,
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
	} else if projects != nil {
		view.Projects = projects
	}

	selected, keyword, page := "", "", 1
	if req != nil {
		selected = strings.TrimSpace(req.ProjectID)
		keyword = strings.TrimSpace(req.Keyword)
		if req.Page > 0 {
			page = req.Page
		}
	}
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	view.SelectedProject = selected
	view.FilterKeyword = keyword
	view.Filtered = keyword != ""
	view.Page = page

	if firstErr == nil && selected != "" && !ProjectExists(projects, selected) {
		view.HTTPStatus = 400
		firstErr = productenums.Err(productenums.ErrInvalidParam)
	}

	// 过滤条件只构造一次（计数与列表各自复制、只给列表那份填 Page/Size）：两处口径分叉
	//（关键词只归一在一侧）在这里是不可能的。
	var filterReq *productdto.ListBrandReq
	if firstErr == nil && selected != "" {
		filterReq = &productdto.ListBrandReq{ProjectID: selected, Keyword: keyword}
		n, cerr := s.CountBrands(ctx, filterReq)
		if cerr != nil {
			fail(cerr)
		} else {
			view.Total = n
		}
	}
	if firstErr == nil && filterReq != nil {
		view.Page = ClampPageToTotal(page, ProductSubListPageSize, view.Total)
		listReq := *filterReq
		listReq.Page, listReq.Size = view.Page, ProductSubListPageSize
		got, lerr := s.ListBrands(ctx, &listReq)
		if lerr != nil {
			fail(lerr)
		} else if got != nil {
			view.BrandDetails = got
		}
	}

	if firstErr != nil {
		view.LoadFailed = true
		return view, firstErr
	}
	return view, nil
}

// ProductTagsPage 标签管理页的读模型：工程清单 → 参数档位 → 规则类型清单 → 计数 →
// 页码收敛 → 取页。
//
// 标签列表**分页下推到 service**（审计 D13 收口）：总数由契约的 CountTags 给出（与 ListTags
// 同一份过滤条件）。命中数仍是 service 的一次批量聚合（审计 PERF-02）——分页后只聚合当页标签，
// 页面 SQL 条数与标签总数无关这一条不变。
func (s *Service) ProductTagsPage(ctx context.Context, req *productdto.ProductTagsPageReq) (*productdto.ProductTagsPageResp, error) {
	view := &productdto.ProductTagsPageResp{
		Projects:   []projectdto.ProjectResp{},
		TagDetails: []*productdto.TagResp{},
		RuleTypes:  []*productdto.TagRuleTypeResp{},
		Limit:      ProductSubListPageSize,
		HTTPStatus: 200,
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
	} else if projects != nil {
		view.Projects = projects
	}

	selected, keyword, page := "", "", 1
	if req != nil {
		selected = strings.TrimSpace(req.ProjectID)
		keyword = strings.TrimSpace(req.Keyword)
		if req.Page > 0 {
			page = req.Page
		}
	}
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	view.SelectedProject = selected
	view.FilterKeyword = keyword
	view.Filtered = keyword != ""
	view.Page = page

	if firstErr == nil && selected != "" && !ProjectExists(projects, selected) {
		view.HTTPStatus = 400
		firstErr = productenums.Err(productenums.ErrInvalidParam)
	}

	// RuleTypes 全页只查一次：行级编辑片段、页面级新建片段与页面下拉共享同一份清单，
	// 逐行各查一遍是纯浪费（audit 同款：命中商品曾因每行一次 GetTag 被打回）。
	// 也套守卫：契约上它不带 error（每次查询只是把内置规则表读出来），
	// 但失败页不该再发任何查询 ——「失败后零派生查询」因此是一条可断言的整页性质。
	if firstErr == nil {
		if ruleTypes := s.ListTagRuleTypes(ctx); ruleTypes != nil {
			view.RuleTypes = ruleTypes
		}
	}

	var filterReq *productdto.ListTagReq
	if firstErr == nil && selected != "" {
		filterReq = &productdto.ListTagReq{ProjectID: selected, Keyword: keyword}
		n, cerr := s.CountTags(ctx, filterReq)
		if cerr != nil {
			fail(cerr)
		} else {
			view.Total = n
		}
	}
	if firstErr == nil && filterReq != nil {
		view.Page = ClampPageToTotal(page, ProductSubListPageSize, view.Total)
		listReq := *filterReq
		listReq.Page, listReq.Size = view.Page, ProductSubListPageSize
		got, lerr := s.ListTags(ctx, &listReq)
		if lerr != nil {
			fail(lerr)
		} else if got != nil {
			view.TagDetails = got
		}
	}

	if firstErr != nil {
		view.LoadFailed = true
		return view, firstErr
	}
	return view, nil
}

// 编译期断言：本文件新增的三个读模型必须留在 ProductPageReader 里（接口是整体，
// 少实现一个方法就是处理器侧的运行期断言失败 → 整页静默走兜底出口）。
var _ productcontract.ProductPageReader = (*Service)(nil)
