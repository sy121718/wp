package producthttp

import (
	"context"
	"go_wp/internal/adminui"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productservice "go_wp/internal/module/product/service"
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/pagination"
	"go_wp/pkg/response"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// product_taxonomy.go — 商品的三套「归类」：分类树、品牌、标签（接口面 + 后台页面）。
//
// 三者在界面上分属三个菜单页，但它们是同一类东西：都只被商品**引用**、都参与筛选、
// 都有 slug / 引用守卫 / 批量删除，改一处口径另两处必须跟着看（用户否掉了「按实体拆文件」）。

// 三页的模板名：出口只有一处（页面成败都走它）。
const productCategoriesPageTemplate = "admin/product/product_categories.html"
const productBrandsPageTemplate = "admin/product/product_brands.html"
const productTagsPageTemplate = "admin/product/product_tags.html"

func (h *productPageHandle) ProductCategoriesPage(c *gin.Context) {
	// 读模型在装配期注入（见 productPageHandle.reader）：漏接 = 编译错误，
	// 不再有「请求期断言失败 → 500 兜底」这条路径。
	reader := h.reader
	// 取参数 → 调读模型（取数编排在 service：取哪些、按什么顺序、哪条失败算 400 /
	// 哪条只降级，都是业务结论。见 service.ProductCategoriesPage）。
	view, err := reader.ProductCategoriesPage(c.Request.Context(), &productdto.ProductCategoriesPageReq{
		ProjectID: strings.TrimSpace(c.Query("project")),
		Keyword:   strings.TrimSpace(c.Query("keyword")),
		Page:      productPageNumber(c.Query("page")),
	})
	if view == nil {
		view = &productdto.ProductCategoriesPageView{
			Title: productenums.ProductCategoriesTitle, Menu: "product-categories",
			HTTPStatus: http.StatusInternalServerError,
		}
	}

	// —— 控制器负责的部分：行装配与抽屉 ——
	view.Title = productenums.ProductCategoriesTitle
	view.Menu = "product-categories"
	view.ListQuery = productQueryFromRequest(c, productCategoriesBackKeys...)
	// 候选列表**只收本工程**的分类（判据与理由见 service.CategoriesInProject）。
	options := productservice.CategoryPickOptions(productservice.CategoriesInProject(view.PickFlat, view.SelectedProject))
	view.Options = options
	view.CategoryCreateForm = categoryDrawerData(c, "create", view.SelectedProject, nil, options, nil)
	// 行装配只在取数成功时做 —— 失败页渲染的是失败槽，装配出的行不会被读到。
	if err == nil {
		view.Categories = categoryTreeRows(c, view.SelectedProject, view.CategoryDetails, options, view.Filtered, view.ParentLabels)
	} else {
		view.Categories = []map[string]any{}
	}
	// 分页条：Go 只交事实，文案与链接由 admin/partials/pagination.html 渲染。
	if pg := pagination.Facts(view.Total, view.Page, view.Limit); pg.HasPagination() {
		pg.Base = productListBaseURL("/admin/product-categories", listFilterQuery(view.SelectedProject, view.FilterKeyword))
		view.Pagination = pg
	}
	if err != nil {
		view.LoadFailed = true
		view.LoadError = productErrPageText(c, err)
	}
	adminui.RenderPage(c, productCategoriesPageTemplate, view)
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
		for _, item := range productservice.FlattenCategoryTree(page.Items) {
			if item.Matched {
				options = append(options, gin.H{"ID": item.ID, "Label": productservice.CategoryLabel(item)})
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

// categoryOptionIDs 父级候选下拉里出现过的分类 id（MissingParentIDs 的「已知集合」）。
func categoryOptionIDs(options []map[string]any) map[string]struct{} {
	known := make(map[string]struct{}, len(options))
	for _, option := range options {
		if id, ok := option["ID"].(string); ok {
			known[id] = struct{}{}
		}
	}
	return known
}

// categoryTreeRows 分类树 → 列表行；每行带自己的编辑抽屉数据。
//
// parentLabels 是「父级不在候选列表里」的分类 → 父级事实（见 service.categoryParentLabels），
// 只用于抽屉里那一项的回显。传 nil 表示没有这类行（新建表单就走这条路）。
//
// ParentOutOfScope 是列表行上的徽章：父级属于别的工程（或父级行已不存在）时置真 ——
// 这类行的 ParentID 指向本工程之外，操作者只看分类名看不出来，得有个可见标记。
func categoryTreeRows(c *gin.Context, projectID string, nodes []*productdto.CategoryResp, options []map[string]any, searching bool, parentLabels map[string]productdto.CategoryParentInfo) []map[string]any {
	// 判据取 missingParentIDs（父级不在候选列表）而**不是** parentLabels：后者只收
	// 「名称读得到」的那些父级，RLS 切非超级角色后跨工程父级读不到名称会被它漏掉，
	// 而那时恰恰最该让操作者看见异常。两者共用同一个判定函数，不另起一份候选集合逻辑。
	outOfScope := make(map[string]struct{})
	for _, parentID := range productservice.MissingParentIDs(nodes, categoryOptionIDs(options)) {
		outOfScope[parentID] = struct{}{}
	}
	rows := make([]map[string]any, 0, len(nodes))
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

// ProductBrandsPage 品牌管理页：工程切换 + 筛选栏 + 品牌列表 + 内联新建表单。
func (h *productPageHandle) ProductBrandsPage(c *gin.Context) {
	// 读模型在装配期注入（见 productPageHandle.reader）：漏接 = 编译错误，
	// 不再有「请求期断言失败 → 500 兜底」这条路径。
	reader := h.reader
	view, err := reader.ProductBrandsPage(c.Request.Context(), &productdto.ProductBrandsPageReq{
		ProjectID: strings.TrimSpace(c.Query("project")),
		Keyword:   strings.TrimSpace(c.Query("keyword")),
		Page:      productPageNumber(c.Query("page")),
	})
	if view == nil {
		view = &productdto.ProductBrandsPageView{
			Title: productenums.ProductBrandsTitle, Menu: "product-brands",
			HTTPStatus: http.StatusInternalServerError,
		}
	}

	// —— 控制器负责的部分：行装配（每行的编辑抽屉要 csrf 与取词函数）——
	view.Title = productenums.ProductBrandsTitle
	view.Menu = "product-brands"
	view.ListQuery = productQueryFromRequest(c, productBrandsBackKeys...)
	brands := make([]map[string]any, 0, len(view.BrandDetails))
	if err == nil {
		for _, b := range view.BrandDetails {
			brands = append(brands, gin.H{
				"ID": b.ID, "Name": b.Name, "Slug": b.Slug, "Logo": b.Logo,
				"Sort": b.Sort, "Description": b.Description,
				"UpdatedAt": b.UpdatedAt,
			})
		}
	}
	view.Brands = brands
	if pg := pagination.Facts(view.Total, view.Page, view.Limit); pg.HasPagination() {
		pg.Base = productListBaseURL("/admin/product-brands", listFilterQuery(view.SelectedProject, view.FilterKeyword))
		view.Pagination = pg
	}
	if err != nil {
		view.LoadFailed = true
		view.LoadError = productErrPageText(c, err)
	}
	adminui.RenderPage(c, productBrandsPageTemplate, view)
}

// missingParentIDs 挑出本页里「父级不在候选列表」的那些父级 id（去重、保持出现顺序）。
//
// 纯逻辑部分单独成函数：候选集合的判定（不是 service 读）才是「谁会走模板那条额外分支」
// 的判据，单测在没有数据库时也能把它钉住。
// productdto.CategoryParentInfo 「父级不在候选列表里」那一项的**事实**：名称是数据，跨工程是布尔。
//
// 拆成两个字段而不是一个拼好的串（原 categoryParentText 的形态）：跨工程那句说明是文案，
// 必须在渲染期按请求语言取词（en-US 后台下也该是英文）—— 控制器里拼串等于把它钉死在中文。
func categoryDrawerData(c *gin.Context, mode, projectID string, row *productdto.CategoryResp, options []map[string]any, parentLabels map[string]productdto.CategoryParentInfo) gin.H {
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
			// （service.CategoriesInProject）—— 分支因此真正走到，文案里还带上「不属于本工程」。
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

// —— 批量删除（JSON 端点）——
//
// 四套归类（分类 / 品牌 / 标签 / 属性组）的批量删除形状逐字相同：身体是 {ids, projectId}、
// 权限点复用各自的单删、回执走同一个 productBulkDeletedView。所以只有一个入口
// bulkDeleteByIDs，差别只在传给用例的**方法值**。
//
// 为什么抽公共入口而不是写三遍：三条路的每一行都必须同口径（归一函数、上限、回执键名），
// 分开写迟早有一处漏了 bulkids.Normalize —— 症状是超限那批被静默截断，而操作者以为
// 「勾的全删了」。这是本仓已发生过的一次真实故障（页面侧 shell.BulkIDs 与 API 侧各写一份）。

// BulkDeleteCategories 批量删除分类（页面 /admin/product-categories/bulk-delete 的承接方）。
func (h *Handle) BulkDeleteCategories(c *gin.Context) {
	h.bulkDeleteByIDs(c, h.svc.BulkDeleteProductCategories)
}

// BulkDeleteBrands 批量删除品牌。
func (h *Handle) BulkDeleteBrands(c *gin.Context) {
	h.bulkDeleteByIDs(c, h.svc.BulkDeleteProductBrands)
}

// BulkDeleteTags 批量删除标签（手工与自动一视同仁，口径由用例决定）。
func (h *Handle) BulkDeleteTags(c *gin.Context) {
	h.bulkDeleteByIDs(c, h.svc.BulkDeleteProductTags)
}

// bulkDeleteByIDs 四套归类（分类 / 品牌 / 标签 / 属性组）的共用入口：解析 → 归一 → 调用例 → 映射回执。
//
// 归一口径与商品批量删除同一个纯函数（bulkids.Normalize）：去空白 / 去重 / 上限 MaxIDs，
// **超限整批拒绝，不静默截断** —— 截断会让操作者以为「勾的全删了」，而实际只删了前 200 条。
func (h *Handle) bulkDeleteByIDs(c *gin.Context, del func(context.Context, *productdto.BulkDeleteReq) (*productdto.BulkResult, error)) {
	req := &productdto.BulkDeleteReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	ids, ok := productBulkIDs(c, req.IDs)
	if !ok {
		return
	}
	req.IDs = ids
	res, err := del(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDeleteSuccess, productBulkDeletedView(c, res))
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
// 与属性 / 分类 / 品牌 / 标签页同一模式：GET 渲染完整页。只用 GET/POST。
//
// 三个与其它后台页不同的地方，都是本票的验收要求：
//
//  1. **预览**（验收 4）：POST /admin/product-pricing/preview 直接把试算结果渲染回同一页
//     （200，不是提示页）—— 预览必须能看到明细，而明细只在这一次响应里；
//     表单值原样回填，用户接着点「应用调价」用的就是刚刚预览过的那份参数。
//     它是**只读**的，所以仍是页面 POST（原生表单 + csrf_token 隐藏域）。
//  2. **应用**（验收 3）：POST /api/product/pricing/apply 走 service 落库 + 留痕，
//     由 ui/json_write.js 提交，成功回执含本次改动的变体数（页面上是 flash 结论行）。
//  3. **留痕**（验收 4）：页面下半部分是调价台账，每个批次可展开看到逐变体「原价 → 新价」。
//
// ==========================================================================
//
//	· 建手工标签并挂到商品（挂载表单在商品**详情页**的「商品标签」区块 ——
//	  标签归属是商品的属性，列表页只回答「有哪些商品」，见 admin-ui-logic §1）；
//	· 自动标签只给内置规则类型 + 白名单参数（规则类型下拉来自 service 的注册表，
//	  参数输入框固定三格：days / minPrice / maxPrice，服务端按规则类型取值并严格校验）；
//	· 标签列表用标准表格（标签 / URL 段 / 类型 / 规则 / 命中 / 重算时间 / 操作），
//	  规则标签额外显示规则描述与重算时间；「命中的商品」按需加载（审计 PERF-02）：
//	  首屏只给数量，展开某个标签才按页取片段，见 ProductTagHitsFragment。
//
// 重算时机在页面上写明（商品/变体写操作后、标签定义变更后、这里的「重算」按钮）。

// ProductTagsPage 标签管理页：工程切换 + 筛选栏 + 新建表单 + 规则类型说明 + 标签列表（含命中商品）。
func (h *productPageHandle) ProductTagsPage(c *gin.Context) {
	// 读模型在装配期注入（见 productPageHandle.reader）：漏接 = 编译错误，
	// 不再有「请求期断言失败 → 500 兜底」这条路径。
	reader := h.reader
	view, err := reader.ProductTagsPage(c.Request.Context(), &productdto.ProductTagsPageReq{
		ProjectID: strings.TrimSpace(c.Query("project")),
		Keyword:   strings.TrimSpace(c.Query("keyword")),
		Page:      productPageNumber(c.Query("page")),
	})
	if view == nil {
		view = &productdto.ProductTagsPageView{
			Title: productenums.ProductTagsTitle, Menu: "product-tags",
			HTTPStatus: http.StatusInternalServerError,
		}
	}

	// —— 控制器负责的部分：行装配与两个标签抽屉（要 csrf 与取词函数）——
	view.Title = productenums.ProductTagsTitle
	view.Menu = "product-tags"
	view.ListQuery = productQueryFromRequest(c, productTagsBackKeys...)
	rows := []map[string]any{}
	tagCreateForm := gin.H{}
	if err == nil {
		rows = make([]map[string]any, 0, len(view.TagDetails))
		for _, tg := range view.TagDetails {
			row := tagPageRow(tg)
			row["EditForm"] = h.tagDrawerData(c, "update", view.SelectedProject, row, view.RuleTypes)
			rows = append(rows, row)
		}
		// 新建抽屉的片段数据与行级片段同源：同一份规则清单、同一份模板。
		tagCreateForm = h.tagDrawerData(c, "create", view.SelectedProject, nil, view.RuleTypes)
	}
	view.Tags = rows
	view.TagCreateForm = tagCreateForm
	if pg := pagination.Facts(view.Total, view.Page, view.Limit); pg.HasPagination() {
		pg.Base = productListBaseURL("/admin/product-tags", listFilterQuery(view.SelectedProject, view.FilterKeyword))
		view.Pagination = pg
	}
	if err != nil {
		view.LoadFailed = true
		view.LoadError = productErrPageText(c, err)
	}
	adminui.RenderPage(c, productTagsPageTemplate, view)
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
	data := gin.H{
		"Mode": mode, "Project": projectID,
		"RuleTypes": ruleTypes,
		"ID":        "", "Name": "", "Slug": "", "IsRule": false, "Sort": 0,
		"RuleType": "", "RuleDays": "", "RuleMinPrice": "", "RuleMaxPrice": "",
	}
	if row != nil {
		for _, key := range []string{"ID", "Name", "Slug", "IsRule", "Sort", "RuleType", "RuleDays", "RuleMinPrice", "RuleMaxPrice"} {
			data[key] = row[key]
		}
	}
	return adminui.PrepareBare(c, data)
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
