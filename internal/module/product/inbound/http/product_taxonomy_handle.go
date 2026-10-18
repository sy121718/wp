package producthttp

import (
	"context"
	"errors"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productservice "go_wp/internal/module/product/service"
	"go_wp/internal/text"
	"go_wp/pkg/binding"
	"go_wp/pkg/pagination"
	"go_wp/pkg/response"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// product_taxonomy.go — 商品的两套「归类」：分类树与品牌（接口面 + 后台页面）。
//
// 两者在界面上分属两个菜单页，但它们是同一类东西：都只被商品**引用**、都参与筛选、
// 都有 slug / 引用守卫 / 批量删除，改一处口径另一处必须跟着看。
// 标签是第三种归类，但它有规则注册表与重算时机、改动频率完全不同，所以单独一份（product_tag.go）。

// 两页的筛选上下文键表：渲染时拼进表单 action，提交回来时按同一份表读回。
var (
	productCategoriesBackKeys = []string{"project", "keyword", "page", "limit"}
	productBrandsBackKeys     = []string{"project", "keyword", "page", "limit"}
)

func (h *ProductPageHandle) ProductCategoriesPage(c *gin.Context) (any, error) {
	// 读模型在装配期注入（见 ProductPageHandle.reader）：漏接 = 编译错误，
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
		view = &productdto.ProductCategoriesPageResp{
			Title: productenums.ProductCategoriesTitle, Menu: "product-categories",
			HTTPStatus: http.StatusInternalServerError,
		}
	}

	// —— 控制器负责的部分：行装配与抽屉 ——
	view.Title = productenums.ProductCategoriesTitle
	view.Menu = "product-categories"
	view.ListQuery = binding.ListQuery(c, productCategoriesBackKeys...)
	// 候选列表**只收本工程**的分类（判据与理由见 service.CategoriesInProject）。
	options := productservice.CategoryPickOptions(productservice.CategoriesInProject(view.PickFlat, view.SelectedProject))
	view.Options = options
	view.CategoryCreateForm = categoryDrawerData(c, "create", view.SelectedProject, nil, options, nil)
	// 行装配只在取数成功时做 —— 失败页渲染的是失败槽，装配出的行不会被读到。
	if err == nil {
		view.Categories = categoryTreeRows(c, view.SelectedProject, view.CategoryDetails, options, view.Filtered, view.ParentLabels)
	} else {
		view.Categories = []productdto.CategoryRowResp{}
	}
	// 分页条：Go 只交事实，文案与链接由 admin/partials/pagination.html 渲染。
	if pg := pagination.Facts(view.Total, view.Page, view.Limit); pg.HasPagination() {
		pg.Base = pagination.BaseURL("/admin/product-categories", pagination.Query("project", view.SelectedProject, "keyword", view.FilterKeyword))
		view.Pagination = pg
	}
	if err != nil {
		view.LoadFailed = true
		view.LoadError = response.MessageOf(c, "product", err)
	}
	return view, nil
}

// ProductCategoryParents is a bounded, project-scoped parent picker search.
func (h *ProductPageHandle) ProductCategoryParents(c *gin.Context) (any, error) {
	projectID := strings.TrimSpace(c.Query("project"))
	if !h.categoryProjectExists(c, projectID) {
		// 工程不存在是**参数**问题（下拉到底来自哪个工程），不是服务故障：400 + 文案。
		return nil, errors.New(productenums.ErrMissingProjectContext)
	}
	keyword := strings.TrimSpace(c.Query("keyword"))
	options := []gin.H{}
	if keyword != "" {
		page, err := h.products.ListCategoryPage(c.Request.Context(), &productdto.ListCategoryPageReq{
			ProjectID: projectID, Keyword: keyword, Page: productPageNumber(c.Query("page")), Size: productservice.ProductSubListPageSize,
		})
		if err != nil {
			return nil, err
		}
		for _, item := range productservice.FlattenCategoryTree(page.Items) {
			if item.Matched {
				options = append(options, gin.H{"ID": item.ID, "Label": productservice.CategoryLabel(item)})
			}
		}
	}
	return gin.H{"Options": options}, nil
}

func (h *ProductPageHandle) categoryProjectExists(c *gin.Context, projectID string) bool {
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
func categoryOptionIDs(options []productdto.PickOptionResp) map[string]struct{} {
	known := make(map[string]struct{}, len(options))
	for _, option := range options {
		if id := option.Value; id != "" {
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
func categoryTreeRows(c *gin.Context, projectID string, nodes []*productdto.CategoryResp, options []productdto.PickOptionResp, searching bool, parentLabels map[string]productdto.CategoryParentInfoResp) []productdto.CategoryRowResp {
	// 判据取 missingParentIDs（父级不在候选列表）而**不是** parentLabels：后者只收
	// 「名称读得到」的那些父级，RLS 切非超级角色后跨工程父级读不到名称会被它漏掉，
	// 而那时恰恰最该让操作者看见异常。两者共用同一个判定函数，不另起一份候选集合逻辑。
	outOfScope := make(map[string]struct{})
	for _, parentID := range productservice.MissingParentIDs(nodes, categoryOptionIDs(options)) {
		outOfScope[parentID] = struct{}{}
	}
	rows := make([]productdto.CategoryRowResp, 0, len(nodes))
	for _, node := range nodes {
		_, flagged := outOfScope[node.ParentID]
		rows = append(rows, productdto.CategoryRowResp{
			ID: node.ID, Name: node.Name, Slug: node.Slug, Label: node.Name,
			ParentID: node.ParentID, Sort: node.Sort, Depth: node.Depth,
			HasChildren:      node.HasChildren,
			Matched:          node.Matched,
			SearchMode:       searching,
			ParentOutOfScope: flagged,
			EditForm:         categoryDrawerData(c, "update", projectID, node, options, parentLabels),
		})
	}
	return rows
}

// ProductBrandsPage 品牌管理页：工程切换 + 筛选栏 + 品牌列表 + 内联新建表单。
func (h *ProductPageHandle) ProductBrandsPage(c *gin.Context) (any, error) {
	// 读模型在装配期注入（见 ProductPageHandle.reader）：漏接 = 编译错误，
	// 不再有「请求期断言失败 → 500 兜底」这条路径。
	reader := h.reader
	view, err := reader.ProductBrandsPage(c.Request.Context(), &productdto.ProductBrandsPageReq{
		ProjectID: strings.TrimSpace(c.Query("project")),
		Keyword:   strings.TrimSpace(c.Query("keyword")),
		Page:      productPageNumber(c.Query("page")),
	})
	if view == nil {
		view = &productdto.ProductBrandsPageResp{
			Title: productenums.ProductBrandsTitle, Menu: "product-brands",
			HTTPStatus: http.StatusInternalServerError,
		}
	}

	// —— 控制器负责的部分：行装配（每行的编辑抽屉要 csrf 与取词函数）——
	view.Title = productenums.ProductBrandsTitle
	view.Menu = "product-brands"
	view.ListQuery = binding.ListQuery(c, productBrandsBackKeys...)
	brands := make([]productdto.BrandRowResp, 0, len(view.BrandDetails))
	if err == nil {
		for _, b := range view.BrandDetails {
			brands = append(brands, productdto.BrandRowResp{
				ID: b.ID, Name: b.Name, Slug: b.Slug, Logo: b.Logo,
				Sort: b.Sort, Description: b.Description,
				UpdatedAt: b.UpdatedAt,
			})
		}
	}
	view.Brands = brands
	if pg := pagination.Facts(view.Total, view.Page, view.Limit); pg.HasPagination() {
		pg.Base = pagination.BaseURL("/admin/product-brands", pagination.Query("project", view.SelectedProject, "keyword", view.FilterKeyword))
		view.Pagination = pg
	}
	if err != nil {
		view.LoadFailed = true
		view.LoadError = response.MessageOf(c, "product", err)
	}
	return view, nil
}

// missingParentIDs 挑出本页里「父级不在候选列表」的那些父级 id（去重、保持出现顺序）。
//
// 纯逻辑部分单独成函数：候选集合的判定（不是 service 读）才是「谁会走模板那条额外分支」
// 的判据，单测在没有数据库时也能把它钉住。
// productdto.CategoryParentInfoResp 「父级不在候选列表里」那一项的**事实**：名称是数据，跨工程是布尔。
//
// 拆成两个字段而不是一个拼好的串（原 categoryParentText 的形态）：跨工程那句说明是文案，
// 必须在渲染期按请求语言取词（en-US 后台下也该是英文）—— 控制器里拼串等于把它钉死在中文。
func categoryDrawerData(c *gin.Context, mode, projectID string, row *productdto.CategoryResp, options []productdto.PickOptionResp, parentLabels map[string]productdto.CategoryParentInfoResp) gin.H {
	data := gin.H{"Mode": mode, "Project": projectID, "Options": options, "Csrf": binding.CSRFToken(c),
		// ListQuery：表单 action 的筛选上下文（抽屉里的表单提交要带着它回去）。
		"ListQuery": binding.ListQuery(c, productCategoriesBackKeys...)}
	if row != nil {
		data["ID"], data["Name"], data["Slug"], data["ParentID"] = row.ID, row.Name, row.Slug, row.ParentID
		if row.ParentID != "" {
			parentMissing := true
			for _, option := range options {
				if option.Value == row.ParentID {
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
	return text.Inject(c, data)
}

// ==========================================================================
// 分类 / 品牌的接口面：JSON 入参、pkg/response 出参、消息取 enums。
// 本层只做绑定与转发：规则校验、引用校验、slug 派生与判环一律在 service。
// ==========================================================================

func (h *ProductHandle) CreateCategory(c *gin.Context) {
	req, ok := binding.JSON[productdto.CreateCategoryReq](c)
	if !ok {
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
func (h *ProductHandle) UpdateCategory(c *gin.Context) {
	req, ok := binding.JSON[productdto.UpdateCategoryReq](c)
	if !ok {
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
func (h *ProductHandle) GetCategory(c *gin.Context) {
	req, ok := binding.Query[productdto.GetCategoryReq](c)
	if !ok {
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
func (h *ProductHandle) ListCategories(c *gin.Context) {
	req, ok := binding.Query[productdto.ListCategoryReq](c)
	if !ok {
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
func (h *ProductHandle) DeleteCategory(c *gin.Context) {
	req, ok := binding.JSON[productdto.DeleteCategoryReq](c)
	if !ok {
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
func (h *ProductHandle) BulkDeleteCategories(c *gin.Context) {
	h.bulkDeleteByIDs(c, h.svc.BulkDeleteProductCategories)
}

// BulkDeleteBrands 批量删除品牌。
func (h *ProductHandle) BulkDeleteBrands(c *gin.Context) {
	h.bulkDeleteByIDs(c, h.svc.BulkDeleteProductBrands)
}

// **超限整批拒绝，不静默截断** —— 截断会让操作者以为「勾的全删了」，而实际只删了前 200 条。
func (h *ProductHandle) bulkDeleteByIDs(c *gin.Context, del func(context.Context, *productdto.BulkDeleteReq) (*productdto.BulkResultResp, error)) {
	req, ok := binding.JSON[productdto.BulkDeleteReq](c)
	if !ok {
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
func (h *ProductHandle) CreateBrand(c *gin.Context) {
	req, ok := binding.JSON[productdto.CreateBrandReq](c)
	if !ok {
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
func (h *ProductHandle) UpdateBrand(c *gin.Context) {
	req, ok := binding.JSON[productdto.UpdateBrandReq](c)
	if !ok {
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
func (h *ProductHandle) GetBrand(c *gin.Context) {
	req, ok := binding.Query[productdto.GetBrandReq](c)
	if !ok {
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
func (h *ProductHandle) ListBrands(c *gin.Context) {
	req, ok := binding.Query[productdto.ListBrandReq](c)
	if !ok {
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
func (h *ProductHandle) DeleteBrand(c *gin.Context) {
	req, ok := binding.JSON[productdto.DeleteBrandReq](c)
	if !ok {
		return
	}
	if err := h.svc.DeleteBrand(c.Request.Context(), req); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDeleteSuccess, nil)
}
