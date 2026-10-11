package producthttp

import (
	"context"
	"encoding/json"
	"errors"
	"go_wp/internal/adminui"
	"go_wp/internal/middleware/builtin"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/internal/shell"
	"go_wp/pkg/pagination"
	"go_wp/pkg/response"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// product_taxonomy.go — 商品的三套「归类」：分类树、品牌、标签（接口面 + 后台页面）。
//
// 三者在界面上分属三个菜单页，但它们是同一类东西：都只被商品**引用**、都参与筛选、
// 都有 slug / 引用守卫 / 批量删除，改一处口径另两处必须跟着看（用户否掉了「按实体拆文件」）。

func (h *productPageHandle) ProductCategoriesPage(c *gin.Context) {
	ctx := c.Request.Context()
	// 取数失败**不在这里出口**：整页 GET 只有一条渲染路径（函数末尾那次 c.HTML），
	// 失败页与成功页渲染同一份模板、同一份页壳数据，差别只有状态码 / no-store / 失败槽
	//（契约见 docs/rules/template-boundary.md §整页 GET 的失败出口）。
	httpStatus := http.StatusOK
	var loadErr error
	fail := func(err error) {
		if loadErr == nil {
			loadErr = err
			// 取数没读到 = 500。参数不合法是另一档（400，见下面的工程校验）。
			httpStatus = http.StatusInternalServerError
		}
	}
	projects, err := h.projects.List(ctx)
	if err != nil {
		fail(err)
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	// 未知工程**原先就在这里被拒**（那时给的是 JSON 出口）：判据不新造，换成共享的
	// productProjectExists（列表页 / 详情页 / 新建页同一份），出口也换成页面请求该有的
	// 那一档：整页 + 400 + no-store。
	// 判据是**本次请求已加载的工程集合**（h.projects.List 返回全量：契约「列出全部站点工程」，
	// 无 Page/Size 参数、model 只 Order 不 LIMIT/WHERE、projects 根表未开 RLS），且与同一份
	// pageData 里的工程选择器**同源**。工程被删除或并发变化会让旧链接里的工程失效 ——
	// 那时 400 正是准确的事实：参数指向的工程已不在可选集合里（不是「工程是空的」）。

	if loadErr == nil && selected != "" && !productProjectExists(projects, selected) {
		httpStatus = http.StatusBadRequest
		loadErr = errors.New(productenums.ErrInvalidParam)
	}
	keyword := strings.TrimSpace(c.Query("keyword"))
	page := productPageNumber(c.Query("page"))
	pageRows := []*productdto.CategoryResp{}
	pickFlat := []*productdto.CategoryResp{}
	total, matchTotal := int64(0), int64(0)
	// 每条取数都套 loadErr == nil：主数据或参数一旦判定失败，后面的派生查询一条都不发。
	if loadErr == nil && selected != "" {
		query := &productdto.ListCategoryPageReq{ProjectID: selected, Keyword: keyword, Page: page, Size: productSubListPageSize}
		result, lerr := h.products.ListCategoryPage(ctx, query)
		if lerr != nil {
			fail(lerr)
		} else {
			total, matchTotal = result.Total, result.MatchTotal
			// 越界页码先收敛再重取一页：表格与分页条必须说的是同一件事。
			if clamped := clampPageToTotal(page, productSubListPageSize, total); clamped != page {
				page = clamped
				query.Page = page
				if again, aerr := h.products.ListCategoryPage(ctx, query); aerr != nil {
					fail(aerr)
				} else {
					result = again
				}
			}
			// 一页的树 → DFS 扁平行（父在前、子紧随，模板按 Depth 缩进）。
			pageRows = flattenCategoryTree(result.Items)
		}
	}
	// 父级下拉要的是**工程内全部分类**，不是当前这一页 ——
	// 树状列表之后没有面包屑兜底，缺项会直接表现为「新建子分类时选不到父级」。
	if loadErr == nil && selected != "" {
		all, aerr := h.products.ListCategories(ctx, &productdto.ListCategoryReq{ProjectID: selected})
		if aerr != nil {
			fail(aerr)
		} else {
			pickFlat = flattenCategoryTree(all)
		}
	}
	// 候选列表**只收本工程**的分类：ListCategories 的跨工程可见性由 RLS 决定
	//（未切非超级角色时策略不生效，别的工程的分类会一起回来），而这里的选择动作是工程内行为 ——
	// 混进来会让「父级在别的工程」这种坏数据在界面上看起来完全正常。
	options := categoryPickOptions(filterCategoriesInProject(pickFlat, selected))
	createForm := categoryDrawerData(c, "create", selected, nil, options, nil)
	filterQuery := listFilterQuery(selected, keyword)
	// 父级不在候选列表里（跨工程遗留 / 父级行已不存在）时，抽屉仍要渲染「当前选中的父级」
	// 那一项，并说明它为什么不在候选里 —— 那是操作者唯一的可见信号。
	rows := categoryTreeRows(c, selected, pageRows, options, keyword != "", h.missingParentLabels(c, selected, pageRows, options))
	// withCSRF：注入 csrf_token（POST 表单隐藏域）+ 导航树 + 权限码 + 多语言。
	data := gin.H{
		"title":           productenums.ProductCategoriesTitle,
		"menu":            "product-categories",
		"Projects":        projects,
		"SelectedProject": selected,
		"Categories":      rows,
		// 父级下拉选项：扁平列表 + 缩进标签（模板里排除自身，避免明显的自环提交）。
		"Options":    options,
		"SearchMode": keyword != "",
		// MatchTotal 是**命中条数**（提示文案用）；分页条按 Total（= 命中所属的根分类数）。
		"MatchTotal":         matchTotal,
		"CategoryCreateForm": createForm,
		// 筛选回显（GET 表单的 value）：提交后条件留在控件上，
		// 否则用户看不出「现在到底筛了什么」；Filtered 让空态能区分
		// 「筛出来是空的」与「这个工程还没有分类」。
		"FilterKeyword": keyword,
		"Filtered":      keyword != "",
		// 写动作的结论不在本页回显（走 shell.RenderJump 渲染提示页，见 product_jump.go）；
		// ListQuery 是筛选上下文（表单 action 的 query）：写动作失败时由 shell.BackPath 读回。
		"ListQuery": productQueryFromRequest(c, productCategoriesBackKeys...),
	}
	// 分页条：Go 只交事实，文案与链接由 admin/partials/pagination.html 渲染
	//（基地址带关键词与工程筛选，翻页不丢条件；单页/空数据不注入）。
	if pg := pagination.Facts(total, page, productSubListPageSize); pg.HasPagination() {
		pg.Base = productListBaseURL("/admin/product-categories", filterQuery)
		data["Pagination"] = pg
	}
	// 出口只有这一处（页面成败都走它）：失败槽 + no-store 只在这一处加。
	if loadErr != nil {
		data["LoadFailed"] = true
		data["LoadError"] = productErrPageText(c, loadErr)
		c.Header("Cache-Control", "no-store")
	}
	c.HTML(httpStatus, "admin/product/product_categories.html", adminui.Prepare(c, data))
}

// ProductCategoryParents is a bounded, project-scoped parent picker search.
func (h *productPageHandle) ProductCategoryParents(c *gin.Context) {
	projectID := strings.TrimSpace(c.Query("project"))
	if !h.categoryProjectExists(c, projectID) {
		c.Status(http.StatusNotFound)
		return
	}
	keyword := strings.TrimSpace(c.Query("keyword"))
	options := []gin.H{}
	if keyword != "" {
		page, err := h.products.ListCategoryPage(c.Request.Context(), &productdto.ListCategoryPageReq{
			ProjectID: projectID, Keyword: keyword, Page: productPageNumber(c.Query("page")), Size: productSubListPageSize,
		})
		if err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		for _, item := range flattenCategoryTree(page.Items) {
			if item.Matched {
				options = append(options, gin.H{"ID": item.ID, "Label": categoryLabel(item)})
			}
		}
	}
	c.HTML(http.StatusOK, "admin/product/product_category_parent_options.html",
		adminui.PrepareBare(c, gin.H{"Options": options}))
}

func (h *productPageHandle) categoryProjectExists(c *gin.Context, projectID string) bool {
	if projectID == "" {
		return false
	}
	projects, err := h.projects.List(c.Request.Context())
	if err != nil {
		return false
	}
	for _, project := range projects {
		if project.ID == projectID {
			return true
		}
	}
	return false
}

// categoryTreeRows 分类树 → 列表行；每行带自己的编辑抽屉数据。
//
// parentLabels 是「父级不在候选列表里」的分类 → 父级事实（见 missingParentLabels），
// 只用于抽屉里那一项的回显。传 nil 表示没有这类行（新建表单就走这条路）。
//
// ParentOutOfScope 是列表行上的徽章：父级属于别的工程（或父级行已不存在）时置真 ——
// 这类行的 ParentID 指向本工程之外，操作者只看分类名看不出来，得有个可见标记。
func categoryTreeRows(c *gin.Context, projectID string, nodes []*productdto.CategoryResp, options []gin.H, searching bool, parentLabels map[string]categoryParentInfo) []gin.H {
	// 判据取 missingParentIDs（父级不在候选列表）而**不是** parentLabels：后者只收
	// 「名称读得到」的那些父级，RLS 切非超级角色后跨工程父级读不到名称会被它漏掉，
	// 而那时恰恰最该让操作者看见异常。两者共用同一个判定函数，不另起一份候选集合逻辑。
	outOfScope := make(map[string]struct{})
	for _, parentID := range missingParentIDs(nodes, options) {
		outOfScope[parentID] = struct{}{}
	}
	rows := make([]gin.H, 0, len(nodes))
	for _, node := range nodes {
		_, flagged := outOfScope[node.ParentID]
		rows = append(rows, gin.H{
			"ID": node.ID, "Name": node.Name, "Slug": node.Slug, "Label": node.Name,
			"ParentID": node.ParentID, "Sort": node.Sort, "Depth": node.Depth,
			"HasChildren":      node.HasChildren,
			"Matched":          node.Matched,
			"SearchMode":       searching,
			"ParentOutOfScope": flagged,
			"EditForm":         categoryDrawerData(c, "update", projectID, node, options, parentLabels),
		})
	}
	return rows
}

// ProductCategoriesCreate 新建分类。
func (h *productPageHandle) ProductCategoriesCreate(c *gin.Context) {
	projectID := c.PostForm("projectId")
	// SEO 标题 / 描述不再从表单读（2026-09-30 字段合并）：分类名即网页标题、
	// 分类描述即 meta description，读侧统一映射（service 的 categoryValues、
	// SEO 评分、构建期 SEO 头）。列保留但不再是任何 UI 的来源。
	req := &productdto.CreateCategoryReq{
		ProjectID:   projectID,
		ParentID:    strings.TrimSpace(c.PostForm("parentId")),
		Name:        strings.TrimSpace(c.PostForm("name")),
		Slug:        strings.TrimSpace(c.PostForm("slug")),
		Description: c.PostForm("description"),
		Image:       strings.TrimSpace(c.PostForm("image")),
		Sort:        parseIntOr(c.PostForm("sort"), 0),
	}
	if _, err := h.products.CreateCategory(c.Request.Context(), req); err != nil {
		h.categoryFormFail(c, "create", productErrMsg(c, err))
		return
	}
	categoryFormSuccess(c, projectID)
}

// ProductCategoriesUpdate 修改分类（改名 / 换父级 / 排序 / 描述与图）。
func (h *productPageHandle) ProductCategoriesUpdate(c *gin.Context) {
	projectID := c.PostForm("projectId")
	parentID := strings.TrimSpace(c.PostForm("parentId"))
	name := strings.TrimSpace(c.PostForm("name"))
	slug := strings.TrimSpace(c.PostForm("slug"))
	description := c.PostForm("description")
	image := strings.TrimSpace(c.PostForm("image"))
	sortValue := parseIntOr(c.PostForm("sort"), 0)
	// SEO 标题 / 描述**不提交**（req 里保持 nil = 不改）：库里可能有编辑者写过的
	// 历史值，不动它就不会丢数据；而它们已经不是读侧口径。
	req := &productdto.UpdateCategoryReq{
		ProjectID: projectID,
		ID:        c.PostForm("id"), ParentID: &parentID, Name: &name, Slug: &slug,
		Description: &description, Image: &image, Sort: &sortValue,
	}
	if _, err := h.products.UpdateCategory(c.Request.Context(), req); err != nil {
		h.categoryFormFail(c, "update", productErrMsg(c, err))
		return
	}
	categoryFormSuccess(c, projectID)
}

// ProductCategoriesDelete 删除分类（有子级或被商品引用时服务端拒绝）。
func (h *productPageHandle) ProductCategoriesDelete(c *gin.Context) {
	projectID := c.PostForm("projectId")
	if err := h.products.DeleteCategory(c.Request.Context(), &productdto.DeleteCategoryReq{ProjectID: projectID, ID: c.PostForm("id")}); err != nil {
		productCategoriesJump(c, false, productErrMsg(c, err))
		return
	}
	productCategoriesJump(c, true, productBulkDoneMsg(productCategoryBulkDone))
}

// ProductCategoriesBulkDelete 批量删除分类。
//
// 逐条走同一条删除路径：有子分类或被商品引用的那一条由服务端拒绝，其余照常删除 ——
// 批量操作不能因为一条失败就整批回滚（用户会以为「一条都没删」，然后反复重试）。
// 结果按「已删 N 个 / 跳过 M 个」渲染提示页，避免静默的部分成功。
func (h *productPageHandle) ProductCategoriesBulkDelete(c *gin.Context) {
	projectID := c.PostForm("projectId")
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		productCategoriesJump(c, false, productBulkIDsMsg(c, berr))
		return
	}
	deleted, skipped := 0, 0
	for _, id := range ids {
		if err := h.products.DeleteCategory(c.Request.Context(), &productdto.DeleteCategoryReq{ProjectID: projectID, ID: id}); err != nil {
			skipped++
			continue
		}
		deleted++
	}
	ok, msg := productBulkDeleteResult(deleted, skipped, productCategoryBulkPartial, productCategoryBulkDone)
	productCategoriesJump(c, ok, msg)
}

// ProductBrandsPage 品牌管理页：工程切换 + 筛选栏 + 品牌列表 + 内联新建表单。
func (h *productPageHandle) ProductBrandsPage(c *gin.Context) {
	ctx := c.Request.Context()
	// 取数失败**不在这里出口**：整页 GET 只有一条渲染路径（函数末尾那次 c.HTML），
	// 失败页与成功页渲染同一份模板、同一份页壳数据，差别只有状态码 / no-store / 失败槽
	//（契约见 docs/rules/template-boundary.md §整页 GET 的失败出口）。
	httpStatus := http.StatusOK
	var loadErr error
	fail := func(err error) {
		if loadErr == nil {
			loadErr = err
			// 取数没读到 = 500。参数不合法是另一档（400，见下面的工程校验）。
			httpStatus = http.StatusInternalServerError
		}
	}
	projects, err := h.projects.List(ctx)
	if err != nil {
		fail(err)
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	// 与商品列表页 / 属性页 / 标签页同一份判据（productProjectExists）：?project=<不存在的 id>
	// 渲染成「这个工程下没有品牌」是把「工程不存在」说成了「工程是空的」。
	// 判据是**本次请求已加载的工程集合**（h.projects.List 返回全量：契约「列出全部站点工程」，
	// 无 Page/Size 参数、model 只 Order 不 LIMIT/WHERE、projects 根表未开 RLS），且与同一份
	// pageData 里的工程选择器**同源**。工程被删除或并发变化会让旧链接里的工程失效 ——
	// 那时 400 正是准确的事实：参数指向的工程已不在可选集合里（不是「工程是空的」）。

	if loadErr == nil && selected != "" && !productProjectExists(projects, selected) {
		httpStatus = http.StatusBadRequest
		loadErr = errors.New(productenums.ErrInvalidParam)
	}
	keyword := strings.TrimSpace(c.Query("keyword"))
	// 品牌列表**分页下推到 service**（审计 D12 收口）：请求类型自带 Page/Size，总数由契约的
	// CountBrands 给出 —— handler 不再「全量取回再切片」，翻到第 N 页也只从库里取那一页。
	// 关键词过滤仍在查询里（与计数同一份过滤条件）。
	//
	// 顺序是**先计数再取页**：反过来（先取第 N 页再数总数）时越界页码会让 service 返回空页，
	// 而分页条按收敛后的页码渲染 —— 「表格为空、分页条却显示第 2 页」正是
	// product_list_paging_test.go 要挡的那种自相矛盾组合。两次查询的条数一样，不额外付代价。
	page := productPageNumber(c.Query("page"))
	total := int64(0)
	pageRows := []*productdto.BrandResp{}
	// 每条取数都套 loadErr == nil：主数据或参数一旦判定失败，后面的派生查询一条都不发
	//（过滤条件只构造一次：计数与列表各自复制、只给列表那份填 Page/Size，
	// 两处口径分叉（关键词只归一在一侧）在这里是不可能的 —— 与属性页同一手法）。
	var filterReq *productdto.ListBrandReq
	if loadErr == nil && selected != "" {
		filterReq = &productdto.ListBrandReq{ProjectID: selected, Keyword: keyword}
		if n, cerr := h.products.CountBrands(ctx, filterReq); cerr != nil {
			fail(cerr)
		} else {
			total = n
		}
	}
	if loadErr == nil && filterReq != nil {
		page = clampPageToTotal(page, productSubListPageSize, total)
		listReq := *filterReq
		listReq.Page, listReq.Size = page, productSubListPageSize
		if got, lerr := h.products.ListBrands(ctx, &listReq); lerr != nil {
			fail(lerr)
		} else {
			pageRows = got
		}
	}
	brands := make([]gin.H, 0, len(pageRows))
	for _, b := range pageRows {
		brands = append(brands, gin.H{
			"ID": b.ID, "Name": b.Name, "Slug": b.Slug, "Logo": b.Logo,
			"Sort": b.Sort, "Description": b.Description,
			"UpdatedAt": b.UpdatedAt,
			"EditForm":  brandDrawerData(c, "update", selected, b),
		})
	}
	data := gin.H{
		"title":           productenums.ProductBrandsTitle,
		"menu":            "product-brands",
		"Projects":        projects,
		"SelectedProject": selected,
		"Brands":          brands,
		"BrandCreateForm": brandDrawerData(c, "create", selected, nil),
		"FilterKeyword":   keyword,
		"Filtered":        keyword != "",
		// 写动作的结论不在本页回显（走 shell.RenderJump 渲染提示页，见 product_jump.go）；
		// ListQuery 是筛选上下文（表单 action 的 query）：写动作失败时由 shell.BackPath 读回。
		"ListQuery": productQueryFromRequest(c, productBrandsBackKeys...),
	}
	// 分页条：Go 只交事实，文案与链接由 admin/partials/pagination.html 渲染
	//（基地址带关键词与工程筛选，翻页不丢条件；单页/空数据不注入）。
	if pg := pagination.Facts(total, page, productSubListPageSize); pg.HasPagination() {
		pg.Base = productListBaseURL("/admin/product-brands", listFilterQuery(selected, keyword))
		data["Pagination"] = pg
	}
	// 出口只有这一处（页面成败都走它）：失败槽 + no-store 只在这一处加。
	if loadErr != nil {
		data["LoadFailed"] = true
		data["LoadError"] = productErrPageText(c, loadErr)
		c.Header("Cache-Control", "no-store")
	}
	c.HTML(httpStatus, "admin/product/product_brands.html", adminui.Prepare(c, data))
}

// ProductBrandsCreate 新建品牌。
func (h *productPageHandle) ProductBrandsCreate(c *gin.Context) {
	projectID := c.PostForm("projectId")
	// SEO 标题 / 描述不再从表单读（2026-09-30 字段合并）：品牌名即网页标题、
	// 品牌描述即 meta description（读侧统一映射，见 service 的 brandValues）。
	req := &productdto.CreateBrandReq{
		ProjectID:   projectID,
		Name:        strings.TrimSpace(c.PostForm("name")),
		Slug:        strings.TrimSpace(c.PostForm("slug")),
		Logo:        strings.TrimSpace(c.PostForm("logo")),
		Description: c.PostForm("description"),
		Sort:        parseIntOr(c.PostForm("sort"), 0),
	}
	if _, err := h.products.CreateBrand(c.Request.Context(), req); err != nil {
		h.brandFormFail(c, "create", productErrMsg(c, err))
		return
	}
	brandFormSuccess(c, projectID)
}

// ProductBrandsUpdate 修改品牌。
func (h *productPageHandle) ProductBrandsUpdate(c *gin.Context) {
	projectID := c.PostForm("projectId")
	name := strings.TrimSpace(c.PostForm("name"))
	slug := strings.TrimSpace(c.PostForm("slug"))
	logo := strings.TrimSpace(c.PostForm("logo"))
	description := c.PostForm("description")
	sortValue := parseIntOr(c.PostForm("sort"), 0)
	// SEO 标题 / 描述**不提交**（nil = 不改），理由同分类。
	req := &productdto.UpdateBrandReq{
		ProjectID: projectID,
		ID:        c.PostForm("id"), Name: &name, Slug: &slug, Logo: &logo,
		Description: &description, Sort: &sortValue,
	}
	if _, err := h.products.UpdateBrand(c.Request.Context(), req); err != nil {
		h.brandFormFail(c, "update", productErrMsg(c, err))
		return
	}
	brandFormSuccess(c, projectID)
}

// ProductBrandsDelete 删除品牌（被商品引用时服务端拒绝）。
func (h *productPageHandle) ProductBrandsDelete(c *gin.Context) {
	projectID := c.PostForm("projectId")
	if err := h.products.DeleteBrand(c.Request.Context(), &productdto.DeleteBrandReq{ProjectID: projectID, ID: c.PostForm("id")}); err != nil {
		productBrandsJump(c, false, productErrMsg(c, err))
		return
	}
	productBrandsJump(c, true, productBulkDoneMsg(productBrandBulkDone))
}

// ProductBrandsBulkDelete 批量删除品牌。
//
// 逐条走同一条删除路径：被商品引用的那一条由服务端拒绝，其余照常删除 ——
// 批量操作不能因为一条失败就整批回滚（用户会以为「一条都没删」，然后反复重试）。
// 结果按「已删 N 个 / 跳过 M 个」渲染提示页，避免静默的部分成功。
func (h *productPageHandle) ProductBrandsBulkDelete(c *gin.Context) {
	projectID := c.PostForm("projectId")
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		productBrandsJump(c, false, productBulkIDsMsg(c, berr))
		return
	}
	deleted, skipped := 0, 0
	for _, id := range ids {
		if err := h.products.DeleteBrand(c.Request.Context(), &productdto.DeleteBrandReq{ProjectID: projectID, ID: id}); err != nil {
			skipped++
			continue
		}
		deleted++
	}
	ok, msg := productBulkDeleteResult(deleted, skipped, productBrandBulkPartial, productBrandBulkDone)
	productBrandsJump(c, ok, msg)
}

// ProductsTaxonomySet 整体替换某商品挂的分类与品牌（issue #10）。
//
// 分类勾选框一个都没勾时浏览器不发该字段，而「一个都不勾」在这里是明确的
// 「解绑全部分类」，故把 nil 归一成空切片 —— 与其它表单的「整体替换」语义一致。
// 主分类由 select 提交（空值 = 不指定）；它在列表里时服务端自动纳入附属分类。
func (h *productPageHandle) ProductsTaxonomySet(c *gin.Context) {
	projectID := c.PostForm("projectId")
	categoryIDs := c.PostFormArray("categoryIds")
	if categoryIDs == nil {
		categoryIDs = []string{}
	}
	primary := strings.TrimSpace(c.PostForm("primaryCategoryId"))
	brand := strings.TrimSpace(c.PostForm("brandId"))
	req := &productdto.UpdateReq{
		ProjectID:         projectID,
		ID:                formProductID(c),
		CategoryIDs:       categoryIDs,
		PrimaryCategoryID: &primary,
		BrandID:           &brand,
	}
	if _, err := h.products.Update(c.Request.Context(), req); err != nil {
		productEditJump(c, false, projectID, req.ID, productErrMsg(c, err))
		return
	}
	productEditJump(c, true, projectID, req.ID, productActionDoneMsg)
}

// flatCategories 取某工程的分类树并摊平成 DFS 前序列表（工程为空时返回空列表）。
func (h *productPageHandle) flatCategories(ctx context.Context, projectID string) (out []*productdto.CategoryResp, err error) {
	out = []*productdto.CategoryResp{}
	if projectID == "" {
		return out, nil
	}
	tree, err := h.products.ListCategories(ctx, &productdto.ListCategoryReq{ProjectID: projectID})
	if err != nil {
		return nil, err
	}
	return flattenCategoryTree(tree), nil
}

// listBrands 取某工程的品牌列表。
func (h *productPageHandle) listBrands(ctx context.Context, projectID string) (out []*productdto.BrandResp, err error) {
	if projectID == "" {
		return []*productdto.BrandResp{}, nil
	}
	return h.products.ListBrands(ctx, &productdto.ListBrandReq{ProjectID: projectID})
}

// flattenCategoryTree 分类树 → DFS 前序扁平行（父在前、子紧随，模板按 Depth 缩进）。
func flattenCategoryTree(nodes []*productdto.CategoryResp) []*productdto.CategoryResp {
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

// categoryLabel 分类的展示标签（按层级缩进；全角空格在 <option> 与列表里都成立）。
func categoryLabel(node *productdto.CategoryResp) string {
	if node.Depth <= 0 {
		return node.Name
	}
	return strings.Repeat("　", node.Depth) + node.Name
}

// categoryPickOptions 父级下拉选项（Selected 由模板按行的 ParentID 比对得出）。
func categoryPickOptions(flat []*productdto.CategoryResp) []gin.H {
	out := make([]gin.H, 0, len(flat))
	for _, node := range flat {
		out = append(out, gin.H{"ID": node.ID, "Label": categoryLabel(node)})
	}
	return out
}

// checkedCategoryOptions 商品页的分类勾选框（勾选态由服务端算好，模板不做集合运算）。
func checkedCategoryOptions(flat []*productdto.CategoryResp, checked []string) []gin.H {
	picked := make(map[string]bool, len(checked))
	for _, id := range checked {
		picked[id] = true
	}
	out := make([]gin.H, 0, len(flat))
	for _, node := range flat {
		out = append(out, gin.H{
			"ID": node.ID, "Label": categoryLabel(node), "Checked": picked[node.ID],
		})
	}
	return out
}

// categoryNameByID 按 id 取分类名（找不到返回空串，不让悬空引用把页面打崩）。
func categoryNameByID(flat []*productdto.CategoryResp, id string) string {
	for _, node := range flat {
		if node.ID == id {
			return node.Name
		}
	}
	return ""
}

// brandNameByID 按 id 取品牌名（找不到返回空串）。
func brandNameByID(brands []*productdto.BrandResp, id string) string {
	for _, b := range brands {
		if b != nil && b.ID == id {
			return b.Name
		}
	}
	return ""
}

// filterCategoriesInProject 只保留属于 projectID 的分类。
//
// 判据是行自己的 ProjectID，**不依赖 RLS**：策略未切非超级角色时 ListCategories 会把别的
// 工程的分类一起返回，混进「本工程的父级候选」里 —— 分类是工程内实体，不该串门。
func filterCategoriesInProject(flat []*productdto.CategoryResp, projectID string) []*productdto.CategoryResp {
	out := make([]*productdto.CategoryResp, 0, len(flat))
	for _, node := range flat {
		if node.ProjectID == projectID {
			out = append(out, node)
		}
	}
	return out
}

// missingParentIDs 挑出本页里「父级不在候选列表」的那些父级 id（去重、保持出现顺序）。
//
// 纯逻辑部分单独成函数：候选集合的判定（不是 service 读）才是「谁会走模板那条额外分支」
// 的判据，单测在没有数据库时也能把它钉住。
func missingParentIDs(nodes []*productdto.CategoryResp, options []gin.H) []string {
	known := make(map[string]struct{}, len(options))
	for _, option := range options {
		if id, ok := option["ID"].(string); ok {
			known[id] = struct{}{}
		}
	}
	seen := make(map[string]struct{}, len(nodes))
	var ids []string
	for _, node := range nodes {
		parentID := strings.TrimSpace(node.ParentID)
		if parentID == "" {
			continue
		}
		if _, ok := known[parentID]; ok {
			continue
		}
		if _, ok := seen[parentID]; ok {
			continue
		}
		seen[parentID] = struct{}{}
		ids = append(ids, parentID)
	}
	return ids
}

// categoryParentInfo 「父级不在候选列表里」那一项的**事实**：名称是数据，跨工程是布尔。
//
// 拆成两个字段而不是一个拼好的串（原 categoryParentText 的形态）：跨工程那句说明是文案，
// 必须在渲染期按请求语言取词（en-US 后台下也该是英文）—— 控制器里拼串等于把它钉死在中文。
type categoryParentInfo struct {
	Name       string
	OutOfScope bool
}

// missingParentLabels 为「父级不在候选列表里」的分类补上父级事实（id → 名称 + 跨工程标记）。
//
// 候选列表是**本工程**分类的全集（见 filterCategoriesInProject），父级不在其中的情形有两种：
// 父级行已不存在，或父级属于别的工程（历史搬迁留下的坏数据）。两种都要让操作者看出来 ——
// 否则下拉里要么是一串裸 ID、要么看起来像个正常选项，用户以为层级没问题。
//
// 跨工程的那一类带上 OutOfScope：模板在名称后面补一句说明（categoryParentOutOfScopeKey 的词条）。
// 名称都读不到（父级行没了 / RLS 切角色后跨工程行不可见）时**不登记**这一项，模板退回显示原始 ID ——
// 显示裸 ID 也比编一个父级名安全。逐个父级查一次库：只对本页里真正缺失的那几个发生。
func (h *productPageHandle) missingParentLabels(c *gin.Context, projectID string, nodes []*productdto.CategoryResp, options []gin.H) map[string]categoryParentInfo {
	if h.products == nil {
		return nil
	}
	ids := missingParentIDs(nodes, options)
	if len(ids) == 0 {
		return nil
	}
	labels := make(map[string]categoryParentInfo, len(ids))
	for _, parentID := range ids {
		parent, err := h.products.GetCategory(c.Request.Context(), &productdto.GetCategoryReq{ProjectID: projectID, ID: parentID})
		if err != nil || parent == nil {
			continue
		}
		labels[parentID] = categoryParentInfo{Name: parent.Name, OutOfScope: parent.ProjectID != projectID}
	}
	return labels
}

func categoryDrawerData(c *gin.Context, mode, projectID string, row *productdto.CategoryResp, options []gin.H, parentLabels map[string]categoryParentInfo) gin.H {
	data := gin.H{"Mode": mode, "Project": projectID, "Options": options, "Csrf": adminui.Prepare(c, gin.H{})["csrf_token"],
		// ListQuery：表单 action 的筛选上下文（失败回跳时 shell.BackPath 读回）。
		"ListQuery": productQueryFromRequest(c, productCategoriesBackKeys...)}
	if row != nil {
		data["ID"], data["Name"], data["Slug"], data["ParentID"] = row.ID, row.Name, row.Slug, row.ParentID
		if row.ParentID != "" {
			parentMissing := true
			for _, option := range options {
				if option["ID"] == row.ParentID {
					parentMissing = false
					break
				}
			}
			data["ParentMissing"] = parentMissing
			// 父级在候选里时模板走 range 那一项；缺失时模板额外渲染一行，
			// 它的文案**必须**是这个键（模板写 isset(.ParentLabel) 才用，否则退回裸 ID）。
			//
			// 可达性（2026-10 实测，别按「死代码」删）：修「孤儿分类丢行」之前这条分支**不可达** ——
			// 悬空 parent_id 的分类既不是根（根条件是 parent_id IS NULL）也不在任何可见父节点的子树里，
			// 整行不进列表；跨工程父级则被 ListCategories 一起带进候选，于是 ParentMissing 恒为 false。
			// 现在两侧都收口了：根集合兜底读孤儿（model 的 categoryRootFilter）、候选只收本工程分类
			// （filterCategoriesInProject）—— 分支因此真正走到，文案里还带上「不属于本工程」。
			//
			// 保留理由：RLS 切到非超级角色后（docs/rls-role-cutover.md 的顺序），GetCategory 会因
			// 作用域读不到跨工程父级 → 名称取不到 → 这里退回模板的「原始 ID」出口。那是兜底，
			// 不是新功能；不要因为「本地构造不出名称」就把整段删掉。
			if parentMissing {
				// ParentLabel 是**名称**（数据），ParentOutOfScope 是**布尔事实** ——
				// 「（不属于本工程）」那句话由模板按当前语言取词（categoryParentOutOfScopeKey）。
				if info, ok := parentLabels[row.ParentID]; ok && strings.TrimSpace(info.Name) != "" {
					data["ParentLabel"] = info.Name
					data["ParentOutOfScope"] = info.OutOfScope
				}
			}
		}
		data["Sort"], data["Image"], data["Description"] = row.Sort, row.Image, row.Description
		data["SEOTitle"], data["SEODescription"] = row.SEOTitle, row.SEODescription
	} else {
		data["ID"], data["Name"], data["Slug"], data["ParentID"] = "", "", "", ""
		data["Sort"], data["Image"], data["Description"] = 0, "", ""
		data["SEOTitle"], data["SEODescription"] = "", ""
	}
	return adminui.PrepareBare(c, data)
}

func (h *productPageHandle) categoryFormFail(c *gin.Context, mode string, msg productMsg) {
	if !isHXRequest(c) {
		productCategoriesJump(c, false, msg)
		return
	}
	data := categoryDrawerData(c, mode, c.PostForm("projectId"), nil, nil, nil)
	if h.products != nil {
		projectID := c.PostForm("projectId")
		if page, err := h.products.ListCategoryPage(c.Request.Context(), &productdto.ListCategoryPageReq{ProjectID: projectID, Page: 1, Size: 100}); err == nil {
			// 列表读的是树，父级下拉要的是扁平行 —— 不摊平的话下拉里只剩顶级分类。
			options := flattenCategoryTree(page.Items)
			parentID := strings.TrimSpace(c.PostForm("parentId"))
			if parentID != "" {
				if parent, perr := h.products.GetCategory(c.Request.Context(), &productdto.GetCategoryReq{ProjectID: projectID, ID: parentID}); perr == nil {
					options = append(options, parent)
				}
			}
			data["Options"] = categoryPickOptions(options)
		}
	}
	data["FormEcho"] = rawDrawerEcho(c, []string{"projectId", "id", "name", "slug", "parentId", "sort", "image", "description", "seoTitle", "seoDescription"})
	// 错误槽收**事实**（productMsg），取词在 product_category_form.html 的 tf 里做。
	data["SubmitErr"] = msg
	c.HTML(http.StatusOK, "admin/product/product_category_form.html", data)
}

func categoryFormSuccess(c *gin.Context, projectID string) {
	productCategoriesJump(c, true, productActionDoneMsg)
}

func brandDrawerData(c *gin.Context, mode, projectID string, row *productdto.BrandResp) gin.H {
	data := gin.H{"Mode": mode, "Project": projectID, "Csrf": adminui.Prepare(c, gin.H{})["csrf_token"],
		// ListQuery：表单 action 的筛选上下文（失败回跳时 shell.BackPath 读回）。
		"ListQuery": productQueryFromRequest(c, productBrandsBackKeys...)}
	if row != nil {
		data["ID"], data["Name"], data["Slug"], data["Logo"] = row.ID, row.Name, row.Slug, row.Logo
		data["Sort"], data["Description"], data["SEOTitle"], data["SEODescription"] = row.Sort, row.Description, row.SEOTitle, row.SEODescription
	} else {
		data["ID"], data["Name"], data["Slug"], data["Logo"] = "", "", "", ""
		data["Sort"], data["Description"], data["SEOTitle"], data["SEODescription"] = 0, "", "", ""
	}
	return adminui.PrepareBare(c, data)
}

func (h *productPageHandle) brandFormFail(c *gin.Context, mode string, msg productMsg) {
	if !isHXRequest(c) {
		productBrandsJump(c, false, msg)
		return
	}
	data := brandDrawerData(c, mode, c.PostForm("projectId"), nil)
	data["FormEcho"] = rawDrawerEcho(c, []string{"projectId", "id", "name", "slug", "sort", "logo", "description", "seoTitle", "seoDescription"})
	// 错误槽收**事实**（productMsg），取词在 product_brand_form.html 的 tf 里做。
	data["SubmitErr"] = msg
	c.HTML(http.StatusOK, "admin/product/product_brand_form.html", data)
}

func brandFormSuccess(c *gin.Context, projectID string) {
	productBrandsJump(c, true, productActionDoneMsg)
}

func rawDrawerEcho(c *gin.Context, fields []string) gin.H {
	_ = c.Request.ParseMultipartForm(formEchoMemory)
	out := gin.H{}
	for _, key := range fields {
		out[key] = ""
		if values := c.Request.PostForm[key]; len(values) > 0 {
			out[key] = values[0]
		}
	}
	return out
}

// ==========================================================================
// 分类 / 品牌 / 标签的接口面：JSON 入参、pkg/response 出参、消息取 enums。
// 本层只做绑定与转发：规则校验、重算时机、引用校验、slug 派生与判环一律在 service。
// ==========================================================================
// CreateTag 新建标签（kind=manual 手工 / kind=rule 自动并带内置规则与参数）。
func (h *Handle) CreateTag(c *gin.Context) {
	req := &productdto.CreateTagReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.CreateTag(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgCreateSuccess, res)
}

// UpdateTag 修改标签（改名 / 换 slug / 换类型 / 改规则参数 / 排序）。
func (h *Handle) UpdateTag(c *gin.Context) {
	req := &productdto.UpdateTagReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.UpdateTag(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgUpdateSuccess, res)
}

// GetTag 标签详情（含命中商品列表）。
func (h *Handle) GetTag(c *gin.Context) {
	req := &productdto.GetTagReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.GetTag(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusNotFound, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDetailSuccess, res)
}

// ListTags 标签列表（工程内；每个标签带当前归属数量）。
func (h *Handle) ListTags(c *gin.Context) {
	req := &productdto.ListTagReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	list, err := h.svc.ListTags(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgListSuccess, list)
}

// ListTagProducts 某标签命中的商品（验收 4）。
func (h *Handle) ListTagProducts(c *gin.Context) {
	req := &productdto.ListTagProductsReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	list, err := h.svc.ListTagProducts(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgListSuccess, list)
}

// ListTagRuleTypes 内置规则类型清单（后台规则下拉的唯一来源）。
func (h *Handle) ListTagRuleTypes(c *gin.Context) {
	response.SuccessWithMessage(c, productenums.MsgListSuccess, h.svc.ListTagRuleTypes(c.Request.Context()))
}

// DeleteTag 删除标签（连同它在商品上的引用一起解绑）。
func (h *Handle) DeleteTag(c *gin.Context) {
	req := &productdto.DeleteTagReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	if err := h.svc.DeleteTag(c.Request.Context(), req); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDeleteSuccess, nil)
}

// RecalcTags 手动触发重算（重算时机之一：可按标签或按工程整体重算）。
func (h *Handle) RecalcTags(c *gin.Context) {
	req := &productdto.RecalcTagsReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.RecalcTags(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgUpdateSuccess, res)
}

// CreateCategory 新建分类（parentId 为空即顶级）。
func (h *Handle) CreateCategory(c *gin.Context) {
	req := &productdto.CreateCategoryReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.CreateCategory(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgCreateSuccess, res)
}

// UpdateCategory 修改分类（改名 / 换父级 / 排序 / SEO 字段）。
func (h *Handle) UpdateCategory(c *gin.Context) {
	req := &productdto.UpdateCategoryReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.UpdateCategory(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgUpdateSuccess, res)
}

// GetCategory 分类详情。
func (h *Handle) GetCategory(c *gin.Context) {
	req := &productdto.GetCategoryReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.GetCategory(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusNotFound, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDetailSuccess, res)
}

// ListCategories 分类树。
func (h *Handle) ListCategories(c *gin.Context) {
	req := &productdto.ListCategoryReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	list, err := h.svc.ListCategories(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgListSuccess, list)
}

// DeleteCategory 删除分类（有子级或被商品引用时拒绝）。
func (h *Handle) DeleteCategory(c *gin.Context) {
	req := &productdto.DeleteCategoryReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	if err := h.svc.DeleteCategory(c.Request.Context(), req); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDeleteSuccess, nil)
}

// CreateBrand 新建品牌。
func (h *Handle) CreateBrand(c *gin.Context) {
	req := &productdto.CreateBrandReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.CreateBrand(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgCreateSuccess, res)
}

// UpdateBrand 修改品牌。
func (h *Handle) UpdateBrand(c *gin.Context) {
	req := &productdto.UpdateBrandReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.UpdateBrand(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgUpdateSuccess, res)
}

// GetBrand 品牌详情。
func (h *Handle) GetBrand(c *gin.Context) {
	req := &productdto.GetBrandReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.GetBrand(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusNotFound, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDetailSuccess, res)
}

// ListBrands 品牌列表。
func (h *Handle) ListBrands(c *gin.Context) {
	req := &productdto.ListBrandReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	list, err := h.svc.ListBrands(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgListSuccess, list)
}

// DeleteBrand 删除品牌（被商品引用时拒绝）。
func (h *Handle) DeleteBrand(c *gin.Context) {
	req := &productdto.DeleteBrandReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	if err := h.svc.DeleteBrand(c.Request.Context(), req); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDeleteSuccess, nil)
}

// ==========================================================================
// 标签库（手工标签 + 自动标签）：列表、命中片段、抽屉与批量改挂。
//
// 自动标签的命中来自规则重算（ProductTagsRecalc）；命中预览是 HTMX 片段，
// 与列表共用同一份规则参数渲染（ruleParamText）。

// tagForm 标签表单的取值（创建与更新共用）。
// 与属性 / 分类 / 品牌 / 标签页同一模式：GET 渲染完整页，POST 处理完渲染整页提示
// （shell.RenderJump，原生表单 + csrf_token 隐藏域）。只用 GET/POST。
//
// 三个与其它后台页不同的地方，都是本票的验收要求：
//
//  1. **预览**（验收 4）：POST /admin/product-pricing/preview 直接把试算结果渲染回同一页
//     （200，不是提示页）—— 预览必须能看到明细，而明细只在这一次响应里；
//     表单值原样回填，用户接着点「应用调价」用的就是刚刚预览过的那份参数。
//  2. **应用**（验收 3）：POST /admin/product-pricing/apply 走 service 落库 + 留痕，
//     完成后渲染整页提示（成功回执含本次改动的变体数）。
//  3. **留痕**（验收 4）：页面下半部分是调价台账，每个批次可展开看到逐变体「原价 → 新价」。
//
// ==========================================================================
type tagForm struct {
	name     string
	slug     string
	kind     string
	ruleType string
	params   json.RawMessage
	sort     int
}

// ProductTagsPage 标签管理页：工程切换 + 筛选栏 + 新建表单 + 规则类型说明 + 标签列表（含命中商品）。
func (h *productPageHandle) ProductTagsPage(c *gin.Context) {
	ctx := c.Request.Context()
	// 取数失败**不在这里出口**：整页 GET 只有一条渲染路径（函数末尾那次 c.HTML），
	// 失败页与成功页渲染同一份模板、同一份页壳数据，差别只有状态码 / no-store / 失败槽
	//（契约见 docs/rules/template-boundary.md §整页 GET 的失败出口）。
	httpStatus := http.StatusOK
	var loadErr error
	fail := func(err error) {
		if loadErr == nil {
			loadErr = err
			// 取数没读到 = 500。参数不合法是另一档（400，见下面的工程校验）。
			httpStatus = http.StatusInternalServerError
		}
	}
	projects, err := h.projects.List(ctx)
	if err != nil {
		fail(err)
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	// 与商品列表页 / 属性页同一份判据（productProjectExists）：?project=<不存在的 id>
	// 渲染成「这个工程下没有标签」是把「工程不存在」说成了「工程是空的」。
	// 判据是**本次请求已加载的工程集合**（h.projects.List 返回全量：契约「列出全部站点工程」，
	// 无 Page/Size 参数、model 只 Order 不 LIMIT/WHERE、projects 根表未开 RLS），且与同一份
	// pageData 里的工程选择器**同源**。工程被删除或并发变化会让旧链接里的工程失效 ——
	// 那时 400 正是准确的事实：参数指向的工程已不在可选集合里（不是「工程是空的」）。

	if loadErr == nil && selected != "" && !productProjectExists(projects, selected) {
		httpStatus = http.StatusBadRequest
		loadErr = errors.New(productenums.ErrInvalidParam)
	}
	keyword := strings.TrimSpace(c.Query("keyword"))
	// 标签列表**分页下推到 service**（审计 D13 收口）：请求类型自带 Page/Size，总数由契约的
	// CountTags 给出（与 ListTags 同一份过滤条件），handler 不再「全量取回再切片」。
	// 命中数仍是 service 的一次批量聚合（审计 PERF-02）——分页后只聚合当页标签，
	// 页面 SQL 条数与标签总数无关这一条不变。
	//
	// 顺序是**先计数再取页**（理由同属性页 / 品牌页）：越界页码先收敛，否则会出现
	// 「表格为空、分页条却显示第 2 页」。
	page := productPageNumber(c.Query("page"))
	total := int64(0)
	rows := []gin.H{}
	var tagCreateForm gin.H
	// RuleTypes 全页只查一次：行级编辑片段、页面级新建片段与页面下拉共享同一份清单，
	// 逐行各查一遍是纯浪费（audit 同款：命中商品曾因每行一次 GetTag 被打回）。
	// 规则清单也套守卫：契约上它不带 error（每次查询只是把内置规则表读出来），
	// 但失败页不该再发任何查询 —— 「失败后零派生查询」因此是一条可断言的整页性质。
	var ruleTypes []*productdto.TagRuleTypeResp
	if loadErr == nil {
		ruleTypes = h.products.ListTagRuleTypes(ctx)
	}
	// 每条取数都套 loadErr == nil：主数据或参数一旦判定失败，后面的派生查询一条都不发
	//（过滤条件只构造一次：计数与列表各自复制、只给列表那份填 Page/Size）。
	var filterReq *productdto.ListTagReq
	list := []*productdto.TagResp{}
	if loadErr == nil && selected != "" {
		filterReq = &productdto.ListTagReq{ProjectID: selected, Keyword: keyword}
		if n, cerr := h.products.CountTags(ctx, filterReq); cerr != nil {
			fail(cerr)
		} else {
			total = n
		}
	}
	if loadErr == nil && filterReq != nil {
		page = clampPageToTotal(page, productSubListPageSize, total)
		listReq := *filterReq
		listReq.Page, listReq.Size = page, productSubListPageSize
		if got, lerr := h.products.ListTags(ctx, &listReq); lerr != nil {
			fail(lerr)
		} else {
			list = got
		}
	}
	if loadErr == nil && filterReq != nil {
		rows = make([]gin.H, 0, len(list))
		for _, t := range list {
			row := tagPageRow(t)
			row["EditForm"] = h.tagDrawerData(c, "update", selected, row, ruleTypes)
			rows = append(rows, row)
		}
		// 新建抽屉的片段数据与行级片段同源：同一份规则清单、同一份模板。
		tagCreateForm = h.tagDrawerData(c, "create", selected, nil, ruleTypes)
	}
	// 命中商品**不在这里取**（审计 PERF-02）：此前对每个标签再调一次 GetTag 拿命中商品，
	// 页面 SQL 条数随标签数线性增长；而「标签是个位数」只是当时的假设，协议没有使它成立。
	// 现在首屏只发「工程列表 + 标签总数 + 标签列表（当页）+ 一次批量计数」，命中商品由
	// 展开区按页拉片段（见 ProductTagHitsFragment）—— 1 / 100 / 1000 个标签的首屏 SQL 条数一样。
	// 注意不要用「开 goroutine 并发 N 次查询」来掩盖它：那是把 N 条 SQL 并行发出去，
	// 连接池压力与总条数都没变。
	//
	// 总数已由契约的 CountTags 给出（与 ListTags 同一份过滤条件：工程 + kind + 关键词）。
	// 注意它与「命中商品数」那一列是两回事：那一列是每个标签归属的商品数
	//（service 里一次批量聚合），本页面的分页只按标签条数算总页数。
	// withCSRF：注入 csrf_token（POST 表单隐藏域）+ 导航树 + 权限码 + 多语言，
	// 与其它后台页面同一渲染入口。
	data := gin.H{
		"title":           productenums.ProductTagsTitle,
		"menu":            "product-tags",
		"Projects":        projects,
		"SelectedProject": selected,
		"Tags":            rows,
		"TagCreateForm":   tagCreateForm,
		"RuleTypes":       ruleTypes,
		// 筛选回显（GET 表单的 value）+ 空态分档依据：见 product_taxonomy_page.go 的同一手法。
		"FilterKeyword": keyword,
		"Filtered":      keyword != "",
		// 写动作的结论不在本页回显（走 shell.RenderJump 渲染提示页，见 product_jump.go）；
		// ListQuery 是筛选上下文（表单 action 的 query）：写动作失败时由 shell.BackPath 读回。
		"ListQuery": productQueryFromRequest(c, productTagsBackKeys...),
	}
	// 分页条：Go 只交事实，文案与链接由 admin/partials/pagination.html 渲染
	//（基地址带关键词与工程筛选，翻页不丢条件；单页/空数据不注入）。
	if pg := pagination.Facts(total, page, productSubListPageSize); pg.HasPagination() {
		pg.Base = productListBaseURL("/admin/product-tags", listFilterQuery(selected, keyword))
		data["Pagination"] = pg
	}
	// 出口只有这一处（页面成败都走它）：失败槽 + no-store 只在这一处加。
	if loadErr != nil {
		data["LoadFailed"] = true
		data["LoadError"] = productErrPageText(c, loadErr)
		c.Header("Cache-Control", "no-store")
	}
	c.HTML(httpStatus, "admin/product/product_tags.html", adminui.Prepare(c, data))
}

// ProductTagHitsFragment 标签「命中商品」片段（审计 PERF-02 的展开区）。
//
// 为什么按需取：标签页首屏此前对每个标签取一次命中商品（GetTag），SQL 条数与标签数
// 成正比，且命中数据（每标签最多 500 行）会一起撑大页面。现在首屏只给数量，
// 展开某个标签时才发这一组查询（标签存在性 + 总数 + 本页行），与页面上有多少标签无关。
//
// 归属：挂在后台**页面组**（Session + CSRF），不叠加 Casbin 权限点 —— 与同组的
// /products/seo-score、/products/variant/preview 同一先例：它是只读渲染，不落任何库
// （写面仍然逐个挂 Casbin），因此不需要新权限点，也不会出现「有路由无权限点 ⇒ 含超管
// 全员 403」。渲染出的商品行只读，行内没有任何写入口。
//
// 失败也回 200 的片段（不是整页错误页）：它替换的是页面里的一小块，
// 回整页 HTML 会把展开区之外的内容一起换掉；错误文案过 productErrText 白名单。
func (h *productPageHandle) ProductTagHitsFragment(c *gin.Context) {
	req := &productdto.ListTagProductsPageReq{
		TagID:     strings.TrimSpace(c.Query("id")),
		ProjectID: strings.TrimSpace(c.Query("project")),
		Page:      parseIntOr(c.Query("page"), 1),
		Size:      parseIntOr(c.Query("size"), 0),
	}
	data := gin.H{
		"TagID":     req.TagID,
		"ProjectID": req.ProjectID,
	}
	res, err := h.products.ListTagProductsPage(c.Request.Context(), req)
	if err != nil {
		data["Err"] = productErrText(c, err)
		c.HTML(http.StatusOK, "admin/product/product_tag_hits.html", adminui.Prepare(c, data))
		return
	}
	data["TagName"] = res.TagName
	data["Total"] = res.Total
	data["Page"] = res.Page
	data["PageSize"] = res.PageSize
	data["TotalPage"] = res.TotalPage
	data["Items"] = tagProductRows(res.Items)
	// 分页交事实（总数 / 页码 / 每页条数 / 基地址），文案与翻页链接由
	// admin/product/product_tag_hits.html 用同一套通用模板函数渲染。
	//
	// 与五个整页站点的差别（有意为之）：这里**不**按 HasPagination() 设闸 —— 片段在
	// 单页非空时仍要渲染两个禁用按钮，「还有没有下一页」是页面上可读的事实；
	// 闸门只由模板的「有没有行」决定。
	//
	// KeepLimit=false：本端点只认 project/id/page/size 四个查询参数，翻页链接里不带 limit
	//（旧 tagHitsURL 也不带）。Base 用**请求里的** project 值（不是 service 解析后的工程）：
	// 链接必须原样回带用户点进来时的那一组参数，空值也保留成 `project=`。
	// 键序 id → page → project 与旧实现的 url.Values.Encode() 逐字节一致（字典序）。
	pg := pagination.Facts(int64(res.Total), res.Page, res.PageSize)
	pg.Base = "/admin/product-tags/hits"
	pg.PageParam = "page"
	pg.KeepLimit = false
	data["Pagination"] = pg
	c.HTML(http.StatusOK, "admin/product/product_tag_hits.html", adminui.Prepare(c, data))
}

// tagDrawerData 装配标签编辑片段数据（create 时 row 为 nil）。
// ruleTypes 由调用方传入：页面装配时取一次全行共享，失败分支自己取一次，
// 不在片段装配里重复查契约。取词走 adminui.PrepareBare（片段不经整份
// Prepare，但模板要 t/tf；漏注入不报错 —— 取词全变空串、200、日志干净）。
func (h *productPageHandle) tagDrawerData(c *gin.Context, mode, projectID string, row gin.H, ruleTypes []*productdto.TagRuleTypeResp) gin.H {
	csrf, _ := builtin.GetCSRFToken(c)
	data := gin.H{
		"Mode": mode, "Project": projectID, "Csrf": csrf,
		"RuleTypes": ruleTypes,
		"ID":        "", "Name": "", "Slug": "", "IsRule": false, "Sort": 0,
		"RuleType": "", "RuleDays": "", "RuleMinPrice": "", "RuleMaxPrice": "",
		// ListQuery：表单 action 的筛选上下文（失败回跳时 shell.BackPath 读回）。
		"ListQuery": productQueryFromRequest(c, productTagsBackKeys...),
	}
	if row != nil {
		for _, key := range []string{"ID", "Name", "Slug", "IsRule", "Sort", "RuleType", "RuleDays", "RuleMinPrice", "RuleMaxPrice"} {
			data[key] = row[key]
		}
	}
	return adminui.PrepareBare(c, data)
}

// tagFormFail 写失败分档：htmx 请求 200 + 片段自身（错误槽 + 原值回填），
// 原生提交渲染整页提示（取代原先的 302 + ?err= 回本页）—— 用户输入比错误文案贵，
// 两种档都不丢字段。
func (h *productPageHandle) tagFormFail(c *gin.Context, mode string, err error) {
	projectID := c.PostForm("projectId")
	msg := productErrMsg(c, err)
	if !isHXRequest(c) {
		productTagsJump(c, false, msg)
		return
	}
	data := h.tagDrawerData(c, mode, projectID, nil, h.products.ListTagRuleTypes(c.Request.Context()))
	data["FormEcho"] = rawDrawerEcho(c, []string{"projectId", "id", "name", "slug", "kind", "sort", "ruleType", "days", "minPrice", "maxPrice"})
	data["SubmitErr"] = msg
	c.HTML(http.StatusOK, "admin/product/product_tag_form.html", data)
}

// tagFormSuccess 写成功出口：整页提示（htmx 由 shell.RenderJump 输出 HX-Redirect）。
func tagFormSuccess(c *gin.Context, projectID string) {
	productTagsJump(c, true, productActionDoneMsg)
}

// ProductTagsCreate 新建标签（手工 / 自动；自动标签建好即按规则重算一次）。
func (h *productPageHandle) ProductTagsCreate(c *gin.Context) {
	projectID := c.PostForm("projectId")
	form := readTagForm(c)
	req := &productdto.CreateTagReq{
		ProjectID: projectID, Name: form.name, Slug: form.slug,
		Kind: form.kind, RuleType: form.ruleType, RuleParams: form.params, Sort: form.sort,
	}
	if _, err := h.products.CreateTag(c.Request.Context(), req); err != nil {
		h.tagFormFail(c, "create", err)
		return
	}
	tagFormSuccess(c, projectID)
}

// ProductTagsUpdate 修改标签（改名 / 换 slug / 换类型 / 改规则参数 / 排序）。
func (h *productPageHandle) ProductTagsUpdate(c *gin.Context) {
	projectID := c.PostForm("projectId")
	form := readTagForm(c)
	req := &productdto.UpdateTagReq{
		ProjectID: projectID,
		ID:        c.PostForm("id"), Name: &form.name, Slug: &form.slug,
		Kind: &form.kind, Sort: &form.sort,
	}
	// 只有自动标签才带规则定义：手工标签提交时规则字段一律不传，
	// 服务端据此把 rule→manual 的切换收敛成「清掉规则定义」。
	if form.kind == productenums.TagKindRule {
		req.RuleType = &form.ruleType
		req.RuleParams = form.params
	}
	if _, err := h.products.UpdateTag(c.Request.Context(), req); err != nil {
		h.tagFormFail(c, "update", err)
		return
	}
	tagFormSuccess(c, projectID)
}

// ProductTagsDelete 删除标签（服务端会把商品上的引用一起解绑）。
func (h *productPageHandle) ProductTagsDelete(c *gin.Context) {
	projectID := c.PostForm("projectId")
	if err := h.products.DeleteTag(c.Request.Context(), &productdto.DeleteTagReq{ProjectID: projectID, ID: c.PostForm("id")}); err != nil {
		productTagsJump(c, false, productErrMsg(c, err))
		return
	}
	productTagsJump(c, true, productBulkDoneMsg(productTagBulkDone))
}

// ProductTagsBulkDelete 批量删除标签（手工 / 自动一视同仁：删除即解绑商品上的该标签）。
//
// 逐条走同一条删除路径：失败的那一条由服务端拒绝，其余照常删除 ——
// 批量操作不能因为一条失败就整批回滚（用户会以为「一条都没删」，然后反复重试）。
// 结果按「已删 N 个 / 跳过 M 个」回带列表页，避免静默的部分成功。
func (h *productPageHandle) ProductTagsBulkDelete(c *gin.Context) {
	projectID := c.PostForm("projectId")
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		// 受控提示（一次最多操作 N 项）保持可见，但同样经归口助手判定来源。
		productTagsJump(c, false, productBulkIDsMsg(c, berr))
		return
	}
	deleted, skipped := 0, 0
	for _, id := range ids {
		if err := h.products.DeleteTag(c.Request.Context(), &productdto.DeleteTagReq{ProjectID: projectID, ID: id}); err != nil {
			skipped++
			continue
		}
		deleted++
	}
	ok, msg := productBulkDeleteResult(deleted, skipped, productTagBulkPartial, productTagBulkDone)
	productTagsJump(c, ok, msg)
}

// ProductTagsRecalc 手动重算（tagId 为空即重算该工程全部自动标签）。
func (h *productPageHandle) ProductTagsRecalc(c *gin.Context) {
	projectID := c.PostForm("projectId")
	req := &productdto.RecalcTagsReq{
		ProjectID: projectID,
		TagID:     strings.TrimSpace(c.PostForm("tagId")),
	}
	if _, err := h.products.RecalcTags(c.Request.Context(), req); err != nil {
		productTagsJump(c, false, productErrMsg(c, err))
		return
	}
	productTagsJump(c, true, productActionDoneMsg)
}

// ProductsTagsSet 整体替换某商品的手工标签（issue #11 验收 1 的后台入口）。
//
// 一个都不勾时浏览器不发该字段，而这里「一个都不勾」是明确的「解绑全部手工标签」，
// 故把 nil 归一成空切片（与分类的「整体替换」语义一致）。自动标签不在这份表单里：
// 它们的归属由重算维护，服务端也不会接受手工挂载。
func (h *productPageHandle) ProductsTagsSet(c *gin.Context) {
	projectID := c.PostForm("projectId")
	tagIDs := c.PostFormArray("tagIds")
	if tagIDs == nil {
		tagIDs = []string{}
	}
	req := &productdto.UpdateReq{ProjectID: projectID, ID: formProductID(c), TagIDs: tagIDs}
	if _, err := h.products.Update(c.Request.Context(), req); err != nil {
		productEditJump(c, false, projectID, req.ID, productErrMsg(c, err))
		return
	}
	productEditJump(c, true, projectID, req.ID, productActionDoneMsg)
}

// readTagForm 读标签表单。
//
// 规则参数只在 kind=rule 时构造：手工标签即使表单里残留了 days / 价格输入也不带过去，
// 否则「切回手工」会被参数校验挡住（服务端对手工标签带规则是明确拒绝的）。
func readTagForm(c *gin.Context) tagForm {
	form := tagForm{
		name: strings.TrimSpace(c.PostForm("name")),
		slug: strings.TrimSpace(c.PostForm("slug")),
		kind: strings.TrimSpace(c.PostForm("kind")),
		sort: parseIntOr(c.PostForm("sort"), 0),
	}
	if form.kind == "" {
		form.kind = productenums.TagKindManual
	}
	if form.kind != productenums.TagKindRule {
		return form
	}
	form.ruleType = strings.TrimSpace(c.PostForm("ruleType"))
	params, err := tagRuleParamsFromForm(c, form.ruleType)
	if err != nil {
		// 参数解析失败时给空对象：真正的校验（键 / 类型 / 取值范围）在 service，
		// 由它给出统一的业务错误，这里不重复一套规则。
		params = json.RawMessage("{}")
	}
	form.params = params
	return form
}

// tagRuleParamsFromForm 按规则类型从表单拼参数对象。
//
// 三个输入框（days / minPrice / maxPrice）与规则类型一一对应，服务端只取本类型用得到的键 ——
// 「自动标签只接受内置参数」在后台这一侧同样成立（多余输入不会进 params）。
// 非法数字（如 days=abc）返回错误，由调用方落成空对象交给 service 报参数错误。
func tagRuleParamsFromForm(c *gin.Context, ruleType string) (raw json.RawMessage, err error) {
	params := map[string]any{}
	switch ruleType {
	case productenums.TagRuleNewArrival:
		if v := strings.TrimSpace(c.PostForm("days")); v != "" {
			n, perr := strconv.Atoi(v)
			if perr != nil {
				return nil, errors.New(productenums.ErrTagRuleParamsInvalid)
			}
			params["days"] = n
		}
	case productenums.TagRulePriceRange:
		if v := strings.TrimSpace(c.PostForm("minPrice")); v != "" {
			f, perr := parseFloat(v)
			if perr != nil {
				return nil, errors.New(productenums.ErrTagRuleParamsInvalid)
			}
			params["minPrice"] = f
		}
		if v := strings.TrimSpace(c.PostForm("maxPrice")); v != "" {
			f, perr := parseFloat(v)
			if perr != nil {
				return nil, errors.New(productenums.ErrTagRuleParamsInvalid)
			}
			params["maxPrice"] = f
		}
	case productenums.TagRuleOnSale:
		// 无参数规则：空对象即可（给了也不会有其它键）。
	default:
		// 未知规则类型：参数留空，由 service 给出「规则类型不合法」。
	}
	return json.Marshal(params)
}

// tagPageRow 标签 → 模板行（规则描述与命中数都由服务端算好，模板不做第二套解释；
// 命中商品本身不在这里，展开时才由 ProductTagHitsFragment 给 —— 审计 PERF-02）。
//
// 文案槽给**事实**（key + 中文兜底），取词在 product_tags.html 的 tr 里做：控制器不取词。
// RecalcAt 是**数据**（已格式化的时间串，未重算时为空），「未重算过」那句话才是文案。
func tagPageRow(t *productdto.TagResp) gin.H {
	// 类型 → 文案事实的映射在 productenums.TagKindLabel（控制器不再挑文案）。
	kindKey, kindFallback := productenums.TagKindLabel(t.Kind)
	recalcAt, recalcKey, recalcFallback := recalcFact(t.RecalcAt)
	row := gin.H{
		"ID": t.ID, "Name": t.Name, "Slug": t.Slug, "Kind": t.Kind,
		"KindLabelKey": kindKey, "KindLabelFallback": kindFallback,
		"IsRule":   t.Kind == productenums.TagKindRule,
		"KindTone": tagKindTone(t.Kind),
		"RuleType": t.RuleType, "RuleLabel": t.RuleLabel,
		"RecalcAt":            recalcAt,
		"RecalcLabelKey":      recalcKey,
		"RecalcLabelFallback": recalcFallback,
		"ProductCount":        t.ProductCount, "Sort": t.Sort,
		"HasRuleParams": t.Kind == productenums.TagKindRule,
		"RuleDays":      ruleParamText(t.RuleParams, "days"),
		"RuleMinPrice":  ruleParamText(t.RuleParams, "minPrice"),
		"RuleMaxPrice":  ruleParamText(t.RuleParams, "maxPrice"),
	}
	row["KindRule"] = productenums.TagKindRule
	row["KindManual"] = productenums.TagKindManual
	return row
}

// tagProductRows 命中商品 → 模板行。
func tagProductRows(items []*productdto.TagProductResp) []gin.H {
	out := make([]gin.H, 0, len(items))
	for _, p := range items {
		out = append(out, gin.H{"ID": p.ID, "Name": p.Name, "Slug": p.Slug, "Status": p.Status})
	}
	return out
}

// tagKindTone 标签类型 → 徽标分档（规则型标签 → info，普通标签中性）。
func tagKindTone(kind string) string {
	if kind == productenums.TagKindRule {
		return "info"
	}
	return "mute"
}

// recalcFact 重算时间 → （数据串, 文案事实）。
//
// 输入是 RFC3339，这里压成「2006-01-02 15:04」：既好读，又不会因为那一长串
// 时间戳在窄屏折叠行里连成不可断行的 token 把卡片撑破。
// 时间是数据（格式固定，走第一个返回值），只有「没重算过」那句话是文案 ——
// 给 key + 中文兜底（productenums.TagRecalcLabel），取词在 product_tags.html 的 tr 里做。
func recalcFact(at string) (text, key, fallback string) {
	key, fallback = productenums.TagRecalcLabel(at)
	return recalcAtText(at), key, fallback
}

// recalcAtText 重算时间的数据串：RFC3339 → 「2006-01-02 15:04」。
//
// 空值给空串（模板据它渲染「—」那一档），不可解析时**原样返回** —— 至少把原值显示出来，
// 留白会让人以为「没有这个数据」。
func recalcAtText(at string) string {
	at = strings.TrimSpace(at)
	if at == "" {
		return ""
	}
	if t, err := time.Parse(time.RFC3339, at); err == nil {
		return t.Local().Format("2006-01-02 15:04")
	}
	return at
}

// ruleParamText 从规则参数里取某个键的文本（缺失返回空串，用于表单回填）。
func ruleParamText(params json.RawMessage, key string) string {
	if len(params) == 0 {
		return ""
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(params, &m); err != nil {
		return ""
	}
	raw, ok := m[key]
	if !ok {
		return ""
	}
	s := strings.TrimSpace(string(raw))
	s = strings.Trim(s, `"`)
	return s
}

// checkedTagOptions 商品页的手工标签勾选框（勾选态由服务端算好，模板不做集合运算）。
func checkedTagOptions(tags []*productdto.TagResp, attached []string) []gin.H {
	have := map[string]bool{}
	for _, id := range attached {
		have[id] = true
	}
	out := make([]gin.H, 0, len(tags))
	for _, t := range tags {
		if t == nil || t.Kind != productenums.TagKindManual {
			continue
		}
		out = append(out, gin.H{"ID": t.ID, "Name": t.Name, "Checked": have[t.ID]})
	}
	return out
}

// attachedAutoTags 商品已归属的自动标签（只读展示：归属由规则重算维护）。
func attachedAutoTags(tags []*productdto.TagResp, attached []string) []gin.H {
	have := map[string]bool{}
	for _, id := range attached {
		have[id] = true
	}
	out := make([]gin.H, 0, len(tags))
	for _, t := range tags {
		if t == nil || t.Kind != productenums.TagKindRule || !have[t.ID] {
			continue
		}
		out = append(out, gin.H{"ID": t.ID, "Name": t.Name, "RuleLabel": t.RuleLabel})
	}
	return out
}
