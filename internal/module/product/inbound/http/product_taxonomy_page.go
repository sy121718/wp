// product_taxonomy_handle.go — 后台商品分类 / 品牌管理页与商品挂载分类品牌（issue #10）。
//
// 与属性页同一模式：独立于 dashboard 的通用 Handle，只依赖 product 契约与 project 契约；
// GET 渲染完整页，POST 处理完 302 回列表，错误经 ?err= 回显（原生表单 + csrf_token 隐藏域）。
//
// 分类树在服务端已算好层级（CategoryResp.Depth），这里只按 DFS 前序摊平给模板 ——
// 父子规则只有 service 一份，模板不做第二套。
//
// 列表页形态（审计 02-M 的 D12 / D13）：分类页与品牌页都带关键词筛选栏、按页渲染，
// 并在空态区分「筛出来是空的」与「工程里本来就没有」。
//
// 分类后台页一律按**树根**分页：浏览态是顶级分类，搜索态是命中所属的根分类，
// 每页都把该页的整棵树一次读出来交给模板渲染。逐层点进去的导航与子级懒加载已退役，
// 旧 ListCategories 全树契约仅保留给其它调用方。
package producthttp

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/internal/web/shell"
)

// ProductCategoriesPage 分类管理页：工程切换 + 筛选栏 + 分类树 + 内联新建表单。
func (h *productPageHandle) ProductCategoriesPage(c *gin.Context) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		shell.PageError(c, "product_taxonomy", err)
		return
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	if selected != "" {
		found := false
		for _, project := range projects {
			if project.ID == selected {
				found = true
				break
			}
		}
		if !found {
			shell.PageError(c, "product_taxonomy", fmt.Errorf("unknown project"))
			return
		}
	}
	keyword := strings.TrimSpace(c.Query("keyword"))
	page := productPageNumber(c.Query("page"))
	pageRows := []*productdto.CategoryResp{}
	pickFlat := []*productdto.CategoryResp{}
	total, matchTotal := int64(0), int64(0)
	if selected != "" {
		query := &productdto.ListCategoryPageReq{ProjectID: selected, Keyword: keyword, Page: page, Size: productSubListPageSize}
		result, lerr := h.products.ListCategoryPage(ctx, query)
		if lerr != nil {
			shell.PageError(c, "product_taxonomy", lerr)
			return
		}
		total, matchTotal = result.Total, result.MatchTotal
		page = clampPageToTotal(page, productSubListPageSize, total)
		if page != query.Page {
			query.Page = page
			result, lerr = h.products.ListCategoryPage(ctx, query)
			if lerr != nil {
				shell.PageError(c, "product_taxonomy", lerr)
				return
			}
		}
		// 一页的树 → DFS 扁平行（父在前、子紧随，模板按 Depth 缩进）。
		pageRows = flattenCategoryTree(result.Items)
		// 父级下拉要的是**工程内全部分类**，不是当前这一页 ——
		// 树状列表之后没有面包屑兜底，缺项会直接表现为「新建子分类时选不到父级」。
		all, aerr := h.products.ListCategories(ctx, &productdto.ListCategoryReq{ProjectID: selected})
		if aerr != nil {
			shell.PageError(c, "product_taxonomy", aerr)
			return
		}
		pickFlat = flattenCategoryTree(all)
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
		"title":           shell.TranslateFor(c)(productenums.ProductCategoriesTitle, "商品分类"),
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
		// 读侧一律过白名单（product_err.go）：查询参数不是可信边界。
		"Err":  productPageErr(c),
		"Done": productPageDone(c),
	}
	// 分页条（shell 组件，服务端渲染）：基地址带当前筛选条件，翻页不丢条件。
	// 单页或空数据时 BuildPagination 返回 nil，TemplateKeys 给空 map，模板自然不渲染。
	for k, v := range shell.BuildPagination(total, page, productSubListPageSize,
		productListBaseURL("/admin/product-categories", filterQuery),
		shell.TranslateFor(c)).TemplateKeys() {
		data[k] = v
	}
	c.HTML(http.StatusOK, "admin/product/product_categories.html", shell.Prepare(c, data))
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
	c.HTML(http.StatusOK, "admin/product/product_category_parent_options.html", gin.H{"Options": options, "t": shell.TranslateFor(c)})
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
// parentLabels 是「父级不在候选列表里」的分类 → 父级名称（见 missingParentLabels），
// 只用于抽屉里那一项的回显。传 nil 表示没有这类行（新建表单就走这条路）。
//
// ParentOutOfScope 是列表行上的徽章：父级属于别的工程（或父级行已不存在）时置真 ——
// 这类行的 ParentID 指向本工程之外，操作者只看分类名看不出来，得有个可见标记。
func categoryTreeRows(c *gin.Context, projectID string, nodes []*productdto.CategoryResp, options []gin.H, searching bool, parentLabels map[string]string) []gin.H {
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
		h.categoryFormFail(c, "create", productErrText(c, err))
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
		h.categoryFormFail(c, "update", productErrText(c, err))
		return
	}
	categoryFormSuccess(c, projectID)
}

// ProductCategoriesDelete 删除分类（有子级或被商品引用时服务端拒绝）。
func (h *productPageHandle) ProductCategoriesDelete(c *gin.Context) {
	projectID := c.PostForm("projectId")
	if err := h.products.DeleteCategory(c.Request.Context(), &productdto.DeleteCategoryReq{ProjectID: projectID, ID: c.PostForm("id")}); err != nil {
		c.Redirect(http.StatusFound, "/admin/product-categories?project="+projectID+"&err="+url.QueryEscape(productErrText(c, err)))
		return
	}
	c.Redirect(http.StatusFound, "/admin/product-categories?project="+projectID)
}

// ProductCategoriesBulkDelete 批量删除分类。
//
// 逐条走同一条删除路径：有子分类或被商品引用的那一条由服务端拒绝，其余照常删除 ——
// 批量操作不能因为一条失败就整批回滚（用户会以为「一条都没删」，然后反复重试）。
// 结果按「已删 N 个 / 跳过 M 个」回带列表页，避免静默的部分成功。
func (h *productPageHandle) ProductCategoriesBulkDelete(c *gin.Context) {
	projectID := c.PostForm("projectId")
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		c.Redirect(http.StatusFound, "/admin/product-categories?project="+url.QueryEscape(projectID)+
			"&err="+url.QueryEscape(productErrText(c, berr)))
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
	target := "/admin/product-categories?project=" + url.QueryEscape(projectID)
	switch {
	case skipped > 0:
		target += "&err=" + url.QueryEscape(fmt.Sprintf(productBulkTextOf(c, productCategoryBulkPartial),
			strconv.Itoa(deleted), strconv.Itoa(skipped)))
	case deleted > 0:
		target += "&done=" + url.QueryEscape(fmt.Sprintf(productBulkTextOf(c, productCategoryBulkDone), strconv.Itoa(deleted)))
	}
	c.Redirect(http.StatusFound, target)
}

// ProductBrandsPage 品牌管理页：工程切换 + 筛选栏 + 品牌列表 + 内联新建表单。
func (h *productPageHandle) ProductBrandsPage(c *gin.Context) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		shell.PageError(c, "product_taxonomy", err)
		return
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
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
	if selected != "" {
		// 过滤条件只构造一次：计数与列表各自复制、只给列表那份填 Page/Size，
		// 两处口径分叉（关键词只归一在一侧）在这里是不可能的 —— 与属性页同一手法。
		filterReq := &productdto.ListBrandReq{ProjectID: selected, Keyword: keyword}
		n, cerr := h.products.CountBrands(ctx, filterReq)
		if cerr != nil {
			shell.PageError(c, "product_taxonomy", cerr)
			return
		}
		total = n
		page = clampPageToTotal(page, productSubListPageSize, total)
		listReq := *filterReq
		listReq.Page, listReq.Size = page, productSubListPageSize
		list, lerr := h.products.ListBrands(ctx, &listReq)
		if lerr != nil {
			shell.PageError(c, "product_taxonomy", lerr)
			return
		}
		pageRows = list
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
		"title":           shell.TranslateFor(c)(productenums.ProductBrandsTitle, "商品品牌"),
		"menu":            "product-brands",
		"Projects":        projects,
		"SelectedProject": selected,
		"Brands":          brands,
		"BrandCreateForm": brandDrawerData(c, "create", selected, nil),
		"FilterKeyword":   keyword,
		"Filtered":        keyword != "",
		"Err":             productPageErr(c),
		"Done":            productPageDone(c),
	}
	for k, v := range shell.BuildPagination(total, page, productSubListPageSize,
		productListBaseURL("/admin/product-brands", listFilterQuery(selected, keyword)),
		shell.TranslateFor(c)).TemplateKeys() {
		data[k] = v
	}
	c.HTML(http.StatusOK, "admin/product/product_brands.html", shell.Prepare(c, data))
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
		h.brandFormFail(c, "create", productErrText(c, err))
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
		h.brandFormFail(c, "update", productErrText(c, err))
		return
	}
	brandFormSuccess(c, projectID)
}

// ProductBrandsDelete 删除品牌（被商品引用时服务端拒绝）。
func (h *productPageHandle) ProductBrandsDelete(c *gin.Context) {
	projectID := c.PostForm("projectId")
	if err := h.products.DeleteBrand(c.Request.Context(), &productdto.DeleteBrandReq{ProjectID: projectID, ID: c.PostForm("id")}); err != nil {
		c.Redirect(http.StatusFound, "/admin/product-brands?project="+projectID+"&err="+url.QueryEscape(productErrText(c, err)))
		return
	}
	c.Redirect(http.StatusFound, "/admin/product-brands?project="+projectID)
}

// ProductBrandsBulkDelete 批量删除品牌。
//
// 逐条走同一条删除路径：被商品引用的那一条由服务端拒绝，其余照常删除 ——
// 批量操作不能因为一条失败就整批回滚（用户会以为「一条都没删」，然后反复重试）。
// 结果按「已删 N 个 / 跳过 M 个」回带列表页，避免静默的部分成功。
func (h *productPageHandle) ProductBrandsBulkDelete(c *gin.Context) {
	projectID := c.PostForm("projectId")
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		c.Redirect(http.StatusFound, "/admin/product-brands?project="+url.QueryEscape(projectID)+
			"&err="+url.QueryEscape(productErrText(c, berr)))
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
	target := "/admin/product-brands?project=" + url.QueryEscape(projectID)
	switch {
	case skipped > 0:
		target += "&err=" + url.QueryEscape(fmt.Sprintf(productBulkTextOf(c, productBrandBulkPartial),
			strconv.Itoa(deleted), strconv.Itoa(skipped)))
	case deleted > 0:
		target += "&done=" + url.QueryEscape(fmt.Sprintf(productBulkTextOf(c, productBrandBulkDone), strconv.Itoa(deleted)))
	}
	c.Redirect(http.StatusFound, target)
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
		c.Redirect(http.StatusFound, productEditLocation(projectID, req.ID, productErrText(c, err)))
		return
	}
	c.Redirect(http.StatusFound, productEditLocation(projectID, req.ID, ""))
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

// primaryCategoryOptions 商品页的主分类下拉（含「不指定」空项）。
func primaryCategoryOptions(tr func(key, fallback string) string, flat []*productdto.CategoryResp, selected string) []gin.H {
	out := make([]gin.H, 0, len(flat)+1)
	out = append(out, gin.H{"ID": "", "Label": tr(productenums.ProductsOptionNoPrimaryCategory, "（不指定主分类）"), "Selected": selected == ""})
	for _, node := range flat {
		out = append(out, gin.H{
			"ID": node.ID, "Label": categoryLabel(node), "Selected": node.ID == selected,
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

// brandPickOptions 商品页的品牌下拉（含「不指定」空项）。
func brandPickOptions(tr func(key, fallback string) string, brands []*productdto.BrandResp, selected string) []gin.H {
	out := make([]gin.H, 0, len(brands)+1)
	out = append(out, gin.H{"ID": "", "Label": tr(productenums.ProductsOptionNoBrand, "（不指定品牌）"), "Selected": selected == ""})
	for _, b := range brands {
		if b == nil {
			continue
		}
		out = append(out, gin.H{"ID": b.ID, "Label": b.Name, "Selected": b.ID == selected})
	}
	return out
}

// ---------------------------------------------------------------------------
// 商品域后台子列表（属性 / 分类 / 品牌 / 标签）共用的分页与取址助手。
//
// 为什么落在这个文件里：本批的独占文件清单只含三个 handler 与四个模板，共享助手所在的
// product_page_util.go 不在其中（有并行任务在同包改别的页面，动它必然冲突）；同包内位置
// 不影响可用性，四页都能直接调用。
// ---------------------------------------------------------------------------

// productSubListPageSize 商品域后台子列表的每页条数。
//
// 20 与商品列表（productListPageSize）同一量级：这四页的表格都带行内抽屉与批量勾选，
// 一页塞太多等于把「翻页」换成「滚动回去找刚才那一行」。
const productSubListPageSize = 20

// clampPageToTotal 把页码收敛到实际总页数以内（total=0 时收敛到第 1 页）。
//
// 为什么在取数**之前**收敛：越界页码（手输 URL、书签失效、上一次筛选后的页码）传给
// 取数层时，service 会老老实实返回一个空页，而分页条按收敛后的页码渲染 ——
// 「表格为空、分页条却显示第 2 页」这种自相矛盾的组合就是这样产生的
// （product_list_paging_test.go 的 TestListPageSlice 钉的是同一条判据）。
//
// 与 shell.BuildPagination 内部的收敛同一条规则（总页数由 total 与 size 算出），
// 差别只在于这里发生在取数之前。
func clampPageToTotal(page, size int, total int64) int {
	if size < 1 {
		size = productSubListPageSize
	}
	if page < 1 {
		page = 1
	}
	pages := int((total + int64(size) - 1) / int64(size))
	if pages < 1 {
		return 1
	}
	if page > pages {
		return pages
	}
	return page
}

// listFilterQuery 列表页筛选条件的查询串（筛选表单与分页基地址共用同一份口径）。
func listFilterQuery(projectID, keyword string) url.Values {
	q := url.Values{}
	if v := strings.TrimSpace(projectID); v != "" {
		q.Set("project", v)
	}
	if v := strings.TrimSpace(keyword); v != "" {
		q.Set("keyword", v)
	}
	return q
}

// productListBaseURL 列表页的分页基地址（**不含** page/limit：分页组件自己拼）。
//
// 把筛选条件拼进基地址，翻页时关键词才不会丢；反过来说，基地址里塞了 page 就会出现两个
// page 参数（浏览器取第一个），翻页看起来「点了没反应」—— 与 productListFilterURL
// （商品列表专用，参数固定为 keyword + status）同一条理由。
func productListBaseURL(path string, q url.Values) string {
	if enc := q.Encode(); enc != "" {
		return path + "?" + enc
	}
	return path
}

// listPageSlice 切出「第 page 页」，并返回**收敛后**的页码。
//
// 保留旧分页助手供同包既有测试使用；分类页现走受限分类读。
//
// 页码收敛与 BuildPagination 同一条规则：page=999 时若不先收敛，会出现「表格为空、
// 分页条却显示第 999 页」这种自相矛盾的组合（BuildPagination 拿到的 total 与 page
// 不同源时就会这样）。
func listPageSlice[T any](all []T, page, size int) (rows []T, current int) {
	if size < 1 {
		size = productSubListPageSize
	}
	if page < 1 {
		page = 1
	}
	pages := (len(all) + size - 1) / size
	if pages < 1 {
		pages = 1
	}
	if page > pages {
		page = pages
	}
	from := (page - 1) * size
	if from > len(all) {
		from = len(all)
	}
	to := from + size
	if to > len(all) {
		to = len(all)
	}
	return all[from:to], page
}

// categoryParentOutOfScopeKey 「上级不在本工程」的文案 key。
//
// 一处定义、两处消费：抽屉里拼在父级名称后面（categoryParentText），列表行上的徽章
// （product_categories.html）用同一个 key 取词 —— 两处是同一句话，不能各写一份常量。
// 词条门禁（scripts/check-i18n-keys-seeded.sh）要求「用了 key 就必须同批 seed」，
// 迁移 508 已补中英成对；基线只降不升，所以 key 与词条必须同批落地。
const categoryParentOutOfScopeKey = "admin.product_categories.parent.out_of_scope"

// categoryParentOutOfScopeFallback key 未命中时的兜底原文（与迁移 508 的 zh-CN 值一致）。
const categoryParentOutOfScopeFallback = "（不属于本工程）"

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

// missingParentLabels 为「父级不在候选列表里」的分类补上父级文案（id → 文案）。
//
// 候选列表是**本工程**分类的全集（见 filterCategoriesInProject），父级不在其中的情形有两种：
// 父级行已不存在，或父级属于别的工程（历史搬迁留下的坏数据）。两种都要让操作者看出来 ——
// 否则下拉里要么是一串裸 ID、要么看起来像个正常选项，用户以为层级没问题。
//
// 文案形态：本工程的父级只给名称；跨工程的补一句说明（categoryParentOutOfScopeKey）。
// 名称都读不到（父级行没了 / RLS 切角色后跨工程行不可见）时留空，模板退回显示原始 ID ——
// 显示裸 ID 也比编一个父级名安全。逐个父级查一次库：只对本页里真正缺失的那几个发生。
func (h *productPageHandle) missingParentLabels(c *gin.Context, projectID string, nodes []*productdto.CategoryResp, options []gin.H) map[string]string {
	if h.products == nil {
		return nil
	}
	ids := missingParentIDs(nodes, options)
	if len(ids) == 0 {
		return nil
	}
	labels := make(map[string]string, len(ids))
	for _, parentID := range ids {
		parent, err := h.products.GetCategory(c.Request.Context(), &productdto.GetCategoryReq{ProjectID: projectID, ID: parentID})
		if err != nil || parent == nil {
			continue
		}
		labels[parentID] = categoryParentText(c, parent, projectID)
	}
	return labels
}

// categoryParentText 父级那一项的文案：属本工程只给名称，跨工程补一句说明。
//
// 说明文字走 i18n（categoryParentOutOfScopeKey）：这条文案在抽屉下拉与列表行徽章上
// 各出现一次，en-US 后台下两处都该是英文。
func categoryParentText(c *gin.Context, parent *productdto.CategoryResp, projectID string) string {
	if parent.ProjectID == projectID {
		return parent.Name
	}
	return parent.Name + shell.TranslateFor(c)(categoryParentOutOfScopeKey, categoryParentOutOfScopeFallback)
}

func categoryDrawerData(c *gin.Context, mode, projectID string, row *productdto.CategoryResp, options []gin.H, parentLabels map[string]string) gin.H {
	data := gin.H{"Mode": mode, "Project": projectID, "Options": options, "Csrf": shell.Prepare(c, gin.H{})["csrf_token"], "t": shell.TranslateFor(c)}
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
				if label := strings.TrimSpace(parentLabels[row.ParentID]); label != "" {
					data["ParentLabel"] = label
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
	return data
}

func (h *productPageHandle) categoryFormFail(c *gin.Context, mode, msg string) {
	if !isHXRequest(c) {
		c.Redirect(http.StatusFound, "/admin/product-categories?project="+url.QueryEscape(c.PostForm("projectId"))+"&err="+url.QueryEscape(msg))
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
	data["SubmitErr"] = msg
	c.HTML(http.StatusOK, "admin/product/product_category_form.html", data)
}

func categoryFormSuccess(c *gin.Context, projectID string) {
	redirectWhere(c, "/admin/product-categories?project="+url.QueryEscape(projectID))
}

func brandDrawerData(c *gin.Context, mode, projectID string, row *productdto.BrandResp) gin.H {
	data := gin.H{"Mode": mode, "Project": projectID, "Csrf": shell.Prepare(c, gin.H{})["csrf_token"], "t": shell.TranslateFor(c)}
	if row != nil {
		data["ID"], data["Name"], data["Slug"], data["Logo"] = row.ID, row.Name, row.Slug, row.Logo
		data["Sort"], data["Description"], data["SEOTitle"], data["SEODescription"] = row.Sort, row.Description, row.SEOTitle, row.SEODescription
	} else {
		data["ID"], data["Name"], data["Slug"], data["Logo"] = "", "", "", ""
		data["Sort"], data["Description"], data["SEOTitle"], data["SEODescription"] = 0, "", "", ""
	}
	return data
}

func (h *productPageHandle) brandFormFail(c *gin.Context, mode, msg string) {
	if !isHXRequest(c) {
		c.Redirect(http.StatusFound, "/admin/product-brands?project="+url.QueryEscape(c.PostForm("projectId"))+"&err="+url.QueryEscape(msg))
		return
	}
	data := brandDrawerData(c, mode, c.PostForm("projectId"), nil)
	data["FormEcho"] = rawDrawerEcho(c, []string{"projectId", "id", "name", "slug", "sort", "logo", "description", "seoTitle", "seoDescription"})
	data["SubmitErr"] = msg
	c.HTML(http.StatusOK, "admin/product/product_brand_form.html", data)
}

func brandFormSuccess(c *gin.Context, projectID string) {
	redirectWhere(c, "/admin/product-brands?project="+url.QueryEscape(projectID))
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
