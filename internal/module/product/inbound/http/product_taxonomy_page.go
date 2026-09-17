// product_taxonomy_handle.go — 后台商品分类 / 品牌管理页与商品挂载分类品牌（issue #10）。
//
// 与属性页同一模式：独立于 dashboard 的通用 Handle，只依赖 product 契约与 project 契约；
// GET 渲染完整页，POST 处理完 302 回列表，错误经 ?err= 回显（原生表单 + csrf_token 隐藏域）。
//
// 分类树在服务端已算好层级（CategoryResp.Depth），这里只按 DFS 前序摊平给模板 ——
// 父子规则只有 service 一份，模板不做第二套。
package producthttp

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	productdto "go_wp/internal/module/product/dto"
	"go_wp/internal/web/shell"
)

// ProductCategoriesPage 分类管理页：工程切换 + 分类树 + 内联新建表单。
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
	flat, err := h.flatCategories(ctx, selected)
	if err != nil {
		shell.PageError(c, "product_taxonomy", err)
		return
	}
	rows := make([]gin.H, 0, len(flat))
	for _, node := range flat {
		rows = append(rows, gin.H{
			"ID": node.ID, "Name": node.Name, "Slug": node.Slug, "Label": categoryLabel(node),
			"ParentID": node.ParentID, "Sort": node.Sort, "Depth": node.Depth,
			"Description": node.Description, "Image": node.Image,
			"SEOTitle": node.SEOTitle, "SEODescription": node.SEODescription,
		})
	}
	// withCSRF：注入 csrf_token（POST 表单隐藏域）+ 导航树 + 权限码 + 多语言。
	c.HTML(http.StatusOK, "admin/product_categories.html", shell.Prepare(c, gin.H{
		"title":           "商品分类",
		"menu":            "product-categories",
		"Projects":        projects,
		"SelectedProject": selected,
		"Categories":      rows,
		// 父级下拉选项：扁平列表 + 缩进标签（模板里排除自身，避免明显的自环提交）。
		"Options": categoryPickOptions(flat),
		"Err":     strings.TrimSpace(c.Query("err")),
	}))
}

// ProductCategoriesCreate 新建分类。
func (h *productPageHandle) ProductCategoriesCreate(c *gin.Context) {
	projectID := c.PostForm("projectId")
	req := &productdto.CreateCategoryReq{
		ProjectID:      projectID,
		ParentID:       strings.TrimSpace(c.PostForm("parentId")),
		Name:           strings.TrimSpace(c.PostForm("name")),
		Slug:           strings.TrimSpace(c.PostForm("slug")),
		Description:    c.PostForm("description"),
		Image:          strings.TrimSpace(c.PostForm("image")),
		SEOTitle:       c.PostForm("seoTitle"),
		SEODescription: c.PostForm("seoDescription"),
		Sort:           parseIntOr(c.PostForm("sort"), 0),
	}
	if _, err := h.products.CreateCategory(c.Request.Context(), req); err != nil {
		c.Redirect(http.StatusFound, "/admin/product-categories?project="+projectID+"&err="+err.Error())
		return
	}
	c.Redirect(http.StatusFound, "/admin/product-categories?project="+projectID)
}

// ProductCategoriesUpdate 修改分类（改名 / 换父级 / 排序 / SEO 字段）。
func (h *productPageHandle) ProductCategoriesUpdate(c *gin.Context) {
	projectID := c.PostForm("projectId")
	parentID := strings.TrimSpace(c.PostForm("parentId"))
	name := strings.TrimSpace(c.PostForm("name"))
	slug := strings.TrimSpace(c.PostForm("slug"))
	description := c.PostForm("description")
	image := strings.TrimSpace(c.PostForm("image"))
	seoTitle := c.PostForm("seoTitle")
	seoDescription := c.PostForm("seoDescription")
	sortValue := parseIntOr(c.PostForm("sort"), 0)
	req := &productdto.UpdateCategoryReq{
		ID: c.PostForm("id"), ParentID: &parentID, Name: &name, Slug: &slug,
		Description: &description, Image: &image,
		SEOTitle: &seoTitle, SEODescription: &seoDescription, Sort: &sortValue,
	}
	if _, err := h.products.UpdateCategory(c.Request.Context(), req); err != nil {
		c.Redirect(http.StatusFound, "/admin/product-categories?project="+projectID+"&err="+err.Error())
		return
	}
	c.Redirect(http.StatusFound, "/admin/product-categories?project="+projectID)
}

// ProductCategoriesDelete 删除分类（有子级或被商品引用时服务端拒绝）。
func (h *productPageHandle) ProductCategoriesDelete(c *gin.Context) {
	projectID := c.PostForm("projectId")
	if err := h.products.DeleteCategory(c.Request.Context(), &productdto.DeleteCategoryReq{ID: c.PostForm("id")}); err != nil {
		c.Redirect(http.StatusFound, "/admin/product-categories?project="+projectID+"&err="+err.Error())
		return
	}
	c.Redirect(http.StatusFound, "/admin/product-categories?project="+projectID)
}

// ProductBrandsPage 品牌管理页：工程切换 + 品牌列表 + 内联新建表单。
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
	brands, err := h.listBrands(ctx, selected)
	if err != nil {
		shell.PageError(c, "product_taxonomy", err)
		return
	}
	c.HTML(http.StatusOK, "admin/product_brands.html", shell.Prepare(c, gin.H{
		"title":           "商品品牌",
		"menu":            "product-brands",
		"Projects":        projects,
		"SelectedProject": selected,
		"Brands":          brands,
		"Err":             strings.TrimSpace(c.Query("err")),
	}))
}

// ProductBrandsCreate 新建品牌。
func (h *productPageHandle) ProductBrandsCreate(c *gin.Context) {
	projectID := c.PostForm("projectId")
	req := &productdto.CreateBrandReq{
		ProjectID:      projectID,
		Name:           strings.TrimSpace(c.PostForm("name")),
		Slug:           strings.TrimSpace(c.PostForm("slug")),
		Logo:           strings.TrimSpace(c.PostForm("logo")),
		Description:    c.PostForm("description"),
		SEOTitle:       c.PostForm("seoTitle"),
		SEODescription: c.PostForm("seoDescription"),
		Sort:           parseIntOr(c.PostForm("sort"), 0),
	}
	if _, err := h.products.CreateBrand(c.Request.Context(), req); err != nil {
		c.Redirect(http.StatusFound, "/admin/product-brands?project="+projectID+"&err="+err.Error())
		return
	}
	c.Redirect(http.StatusFound, "/admin/product-brands?project="+projectID)
}

// ProductBrandsUpdate 修改品牌。
func (h *productPageHandle) ProductBrandsUpdate(c *gin.Context) {
	projectID := c.PostForm("projectId")
	name := strings.TrimSpace(c.PostForm("name"))
	slug := strings.TrimSpace(c.PostForm("slug"))
	logo := strings.TrimSpace(c.PostForm("logo"))
	description := c.PostForm("description")
	seoTitle := c.PostForm("seoTitle")
	seoDescription := c.PostForm("seoDescription")
	sortValue := parseIntOr(c.PostForm("sort"), 0)
	req := &productdto.UpdateBrandReq{
		ID: c.PostForm("id"), Name: &name, Slug: &slug, Logo: &logo,
		Description: &description, SEOTitle: &seoTitle,
		SEODescription: &seoDescription, Sort: &sortValue,
	}
	if _, err := h.products.UpdateBrand(c.Request.Context(), req); err != nil {
		c.Redirect(http.StatusFound, "/admin/product-brands?project="+projectID+"&err="+err.Error())
		return
	}
	c.Redirect(http.StatusFound, "/admin/product-brands?project="+projectID)
}

// ProductBrandsDelete 删除品牌（被商品引用时服务端拒绝）。
func (h *productPageHandle) ProductBrandsDelete(c *gin.Context) {
	projectID := c.PostForm("projectId")
	if err := h.products.DeleteBrand(c.Request.Context(), &productdto.DeleteBrandReq{ID: c.PostForm("id")}); err != nil {
		c.Redirect(http.StatusFound, "/admin/product-brands?project="+projectID+"&err="+err.Error())
		return
	}
	c.Redirect(http.StatusFound, "/admin/product-brands?project="+projectID)
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
		ID:                c.PostForm("id"),
		CategoryIDs:       categoryIDs,
		PrimaryCategoryID: &primary,
		BrandID:           &brand,
	}
	if _, err := h.products.Update(c.Request.Context(), req); err != nil {
		c.Redirect(http.StatusFound, "/admin/products?project="+projectID+"&err="+err.Error())
		return
	}
	c.Redirect(http.StatusFound, "/admin/products?project="+projectID)
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
func primaryCategoryOptions(flat []*productdto.CategoryResp, selected string) []gin.H {
	out := make([]gin.H, 0, len(flat)+1)
	out = append(out, gin.H{"ID": "", "Label": "（不指定主分类）", "Selected": selected == ""})
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
func brandPickOptions(brands []*productdto.BrandResp, selected string) []gin.H {
	out := make([]gin.H, 0, len(brands)+1)
	out = append(out, gin.H{"ID": "", "Label": "（不指定品牌）", "Selected": selected == ""})
	for _, b := range brands {
		if b == nil {
			continue
		}
		out = append(out, gin.H{"ID": b.ID, "Label": b.Name, "Selected": b.ID == selected})
	}
	return out
}
