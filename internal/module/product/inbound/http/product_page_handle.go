package producthttp

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"

	contentcontract "go_wp/internal/module/content/contract"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	pagecontract "go_wp/internal/module/page/contract"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	inventorycontract "go_wp/internal/module/product/inventory/contract"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/web/shell"
)

// product_handle.go — 后台商品管理页（issue #5 / T3a）。

//

// 独立于 dashboard 的通用 Handle：只依赖 product 契约与 project 契约，

// 避免把商品依赖掺进 dashboard 的通用装配。

//

// 交互遵循后台规范：GET 渲染完整页，POST 处理完 302 回列表（原生表单 + csrf_token 隐藏域）。

// variantSelectionPrefix 组合生成表单里「属性组 → 勾选值」的字段名前缀。
//
// 字段名形如 attr:<属性组 id>（多选 checkbox），一个属性组一组值；
// 提交时按前缀收拢成 service 的 selections。
const variantSelectionPrefix = "attr:"

// productPageHandle 商品后台页处理器。
type productPageHandle struct {
	products productcontract.ProductService
	projects projectcontract.ProjectService
	// templates / instances 供「详情页模板」页（issue #14）消费：模板列表与版本、
	// 商品发布实例的模板绑定与预览渲染。经 SetDetailTemplateDeps 注入 ——
	// 未注入时该页给出装配提示，不影响商品列表页与既有测试的构造签名。
	templates contenttemplatecontract.ContentTemplateService
	instances ProductDetailTemplatePort
	// inventories 仓库清单（issue #15）：变体新增表单的「归属仓」下拉，
	// 「不选」即兜底该工程的默认仓。经 SetInventoryDeps 注入 —— 未注入时
	// 表单不带仓库下拉（商品页其余功能一字不变，既有测试构造签名也不受影响）。
	inventories inventorycontract.InventoryService
	// seoPages / seoContents 编辑期 title 唯一性检查的另外两个数据源（审计 SEO-018）：
	// 页面草稿的 SEO 标题与文章标题都算「同站点已存在的内容」，只比商品域会漏掉
	// 跨内容的重复。经 SetSeoTitleSources 注入 —— 可空，未注入时索引退化为商品域。
	seoPages    pagecontract.PageService
	seoContents contentcontract.ContentService
}

// NewProductPageHandle 构造。
func NewProductPageHandle(products productcontract.ProductService, projects projectcontract.ProjectService) *productPageHandle {
	return &productPageHandle{products: products, projects: projects}
}

// SetInventoryDeps 注入仓库清单依赖（issue #15，装配期调用）。
// 未注入时商品页不渲染「归属仓」下拉，变体创建按「不指定仓库」处理。
func (h *productPageHandle) SetInventoryDeps(inventory inventorycontract.InventoryService) {
	h.inventories = inventory
}

// ProductsPage 商品管理页：工程切换 + 商品列表 + 每个商品的变体面板 + 内联新建表单。
func (h *productPageHandle) ProductsPage(c *gin.Context) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		shell.PageError(c, "product", err)
		return
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	rows := make([]gin.H, 0, 50)
	// 分类树与品牌列表一次取好：每个商品行都要渲染「挂哪些分类 / 主分类 / 品牌」，
	// 放在循环里取会变成 2×N 次查询。
	flat, ferr := h.flatCategories(ctx, selected)
	if ferr != nil {
		shell.PageError(c, "product", ferr)
		return
	}
	brands, berr := h.listBrands(ctx, selected)
	if berr != nil {
		shell.PageError(c, "product", berr)
		return
	}
	// 标签一次取好（issue #11）：每个商品行要渲染「挂哪些手工标签 / 命中了哪些自动标签」，
	// 放在循环里取会变成 N 次查询。
	tags, terr := h.listTags(ctx, selected)
	if terr != nil {
		shell.PageError(c, "product", terr)
		return
	}
	if selected != "" {
		list, lerr := h.products.List(ctx, &productdto.ListReq{ProjectID: selected, Size: 100})
		if lerr != nil {
			shell.PageError(c, "product", lerr)
			return
		}
		for _, p := range list {
			// 列表项不含变体明细，逐个取详情（上限 100，后台页可接受）。
			detail, derr := h.products.Get(ctx, &productdto.GetReq{ID: p.ID})
			if derr != nil {
				continue
			}
			rows = append(rows, h.productRow(ctx, selected, flat, brands, tags, detail))
		}
	}
	// 归属仓下拉（issue #15）：变体的归属仓在这里选（不选 = 默认仓）。
	warehouseOptions, werr := h.warehouseOptions(ctx, selected)
	if werr != nil {
		shell.PageError(c, "product", werr)
		return
	}
	// withCSRF：注入 csrf_token（POST 表单隐藏域）+ 导航树 + 权限码 + 多语言，
	// 与其它后台页面同一渲染入口（缺 token 时表单提交会被 CSRF 中间件挡下）。
	c.HTML(http.StatusOK, "admin/products.html", shell.Prepare(c, gin.H{
		"title":            MsgProductsTitle,
		"menu":             "products",
		"Projects":         projects,
		"SelectedProject":  selected,
		"WarehouseOptions": warehouseOptions,
		"Products":         rows,
		// 上一步的错误（上限拒绝 / 参数错误）经查询串回显 —— 同属性页的做法。
		"Err": strings.TrimSpace(c.Query("err")),
		// 批量删除的结果回带（?done=）：部分失败仍走 err（见 ProductsBulkDelete）。
		"Done": strings.TrimSpace(c.Query("done")),
	}))
}

// productRow 组装单个商品的页面视图数据（列表页与详情页共用）。
//
// 键分两段：汇总段（VariantCount / PriceMin / PriceMax / HasRating / RatingAvg /
// RatingCount / CategoryCell / CategoryOthers / BrandName）只回答「有哪些商品」，
// 列表页只用这一段；明细段（Variants / Ratings / CategoryChecks / TagChecks /
// AutoTags / VariationAttributes …）是某个商品的子资源，只有详情页用。
//
// 两页共用同一份组装：同一个值如果在两处各算一遍，分叉时没有任何东西会报错
// （与「同一概念两处口径」是同一类问题）。
func (h *productPageHandle) productRow(ctx context.Context, selected string,
	flat []*productdto.CategoryResp, brands []*productdto.BrandResp, tags []*productdto.TagResp,
	detail *productdto.ProductResp) gin.H {
	// 评分读一次：明细 + 投影值（issue #33）。读不到按「没有评分」处理，页面照常渲染。
	rating := ratingOf(h.products, ctx, selected, detail.ID)
	ratingRows := make([]gin.H, 0, len(rating.Items))
	for _, it := range rating.Items {
		ratingRows = append(ratingRows, gin.H{
			"ID": it.ID, "Score": formatScore(it.Score),
			"Source": it.Source, "CreatedAt": it.CreatedAt,
		})
	}
	categoryCell, categoryOthers := categoryCellLabel(flat, detail.CategoryIDs, detail.PrimaryCategoryID)
	// 标签与自动标签各算一次（列表列的取值与详情页的勾选态共用同一份结果）。
	tagChecks := checkedTagOptions(tags, detail.TagIDs)
	autoTags := attachedAutoTags(tags, detail.TagIDs)
	return gin.H{
		"ID": detail.ID, "Name": detail.Name, "Slug": detail.Slug,
		"Status":   detail.Status,
		"PriceMin": formatAmount(detail.PriceMin), "PriceMax": formatAmount(detail.PriceMax),
		"VariantCount": detail.VariantCount,
		// 变体行带「规格」列：option_values 翻成可读文本，生成后能直接核对组合。
		"Variants": variantRows(detail),
		// 引用的属性组（issue #7）：同一属性组可被多个商品共用，
		// 这里只展示引用与属性值，编辑入口在 /admin/product-attributes。
		"AttributeIDs":    detail.AttributeIDs,
		"AttributeIDsCSV": strings.Join(detail.AttributeIDs, ","),
		"Attributes":      detail.Attributes,
		// 组合生成面板（issue #8）只列参与变体的组。
		"VariationAttributes": variationAttributes(detail.Attributes),
		// 分类与品牌（issue #10）：勾选态 / 选中态都由服务端算好，
		// 模板只做展示；分类下拉带层级缩进（层级真源在 service 的树组装）。
		// CategoryCell / CategoryOthers 是**列表列**要的「一个值 + 其余数量」：
		// 一列一个概念，单元格放值，不把「挂载 N 个分类 · 主分类 X」拼成一句话。
		"CategoryIDs":         detail.CategoryIDs,
		"CategoryChecks":      checkedCategoryOptions(flat, detail.CategoryIDs),
		"CategoryCell":        categoryCell,
		"CategoryOthers":      categoryOthers,
		"PrimaryOptions":      primaryCategoryOptions(flat, detail.PrimaryCategoryID),
		"BrandOptions":        brandPickOptions(brands, detail.BrandID),
		"PrimaryCategoryName": categoryNameByID(flat, detail.PrimaryCategoryID),
		"BrandName":           brandNameByID(brands, detail.BrandID),
		// 标签（issue #11）：手工标签勾选挂载（勾选态服务端算好）；自动标签只读展示 ——
		// 归属由规则重算维护，手工改会被下一次重算覆盖，故不提供勾选框。
		"TagIDs":    detail.TagIDs,
		"TagChecks": tagChecks,
		"AutoTags":  autoTags,
		// TagLabel 是**列表列**要的单个值：这个商品挂了哪些标签（手工 + 自动合并的标签名）。
		"TagLabel": tagCellLabel(tagChecks, autoTags),
		// 评分（issue #30 / #33）：明细 + 投影值。评分是独立表，这里读的是
		// ListRatings 算出的平均值与条数；**没有评分时 HasRating=false** ——
		// 空态与「评分 0」是两回事，模板据它给出不同文案。
		"Ratings":     ratingRows,
		"HasRating":   rating.HasRating,
		"RatingAvg":   formatScore(rating.Rating),
		"RatingCount": rating.RatingCount,
	}
}

// categoryCellLabel 商品行「分类」列要显示的单个值（一列一个概念：单元格是值，不是句子）。
//
// 取值优先级：主分类 > 挂载分类里的第一个 —— 挂了分类却没设主分类时显示空值是误导
// （用户会以为这个商品没有分类）。都没有才返回空串，由模板渲染成 —。
// others 是「其余分类数」，模板用 +N 徽章跟在值后面，不把三个信号拼成一句话。
func categoryCellLabel(flat []*productdto.CategoryResp, attached []string, primaryID string) (label string, others int) {
	if name := categoryNameByID(flat, primaryID); name != "" {
		return name, maxInt(len(attached)-1, 0)
	}
	for _, node := range flat {
		if node == nil || !containsString(attached, node.ID) {
			continue
		}
		return node.Name, maxInt(len(attached)-1, 0)
	}
	return "", 0
}

// tagCellLabel 商品行「标签」列要显示的单个值（一列回答一个问题：这个商品挂什么标签）。
//
// 列的是**标签名**（手工挂载的 + 命中规则的自动标签，合并成一个名字列表），
// 不做「手工 N · 自动 M」那种来源分解 —— 读这一列的人要知道「挂了哪些标签」，
// 而不是「这些标签里有几个是手工建的」（那是标签管理页的问题）；两个数都是 0 时，
// 那种分解更是纯噪声。名字多于 maxTagNames 时退化成数量，避免单元格被撑成一屏。
func tagCellLabel(tagChecks, autoTags []gin.H) string {
	const maxTagNames = 3
	names := make([]string, 0, len(tagChecks)+len(autoTags))
	for _, t := range tagChecks {
		checked, _ := t["Checked"].(bool)
		if !checked {
			continue
		}
		if name, ok := t["Name"].(string); ok && name != "" {
			names = append(names, name)
		}
	}
	for _, t := range autoTags {
		if name, ok := t["Name"].(string); ok && name != "" {
			names = append(names, name)
		}
	}
	switch {
	case len(names) == 0:
		return ""
	case len(names) > maxTagNames:
		return strconv.Itoa(len(names)) + " 个"
	default:
		return strings.Join(names, "、")
	}
}

// maxInt 取两者较大值（其余分类数不能为负：主分类不在挂载列表里时 len-1 会是 -1）。
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ProductDetailPage GET /admin/products/detail：单个商品的详情页。
//
// 为什么是独立页，而不是列表页里的第二、三张表（admin-ui-logic §1）：列表页只该回答
// 「有哪些商品」；变体与评分是**某个商品的子资源**，属于该商品的详情。原来三张表平铺在
// 列表页上，等于让列表页承载实体详情 —— 商品一多，变体表与评分表就是两份与商品表错位的
// 长表，改一个商品要跨三处找入口。
//
// 本页承接：原编辑抽屉的四个商品级表单（属性引用 / 分类与品牌 / 标签 / SEO 检查）
// + 原变体表与它的两个抽屉 + 原评分表与它的抽屉。
//
// 商品不存在（含没给 product 参数）渲染 .empty-state + 返回列表链接，不 500：
// 手输 URL、书签失效、商品刚被删都会走到这里，500 什么也说明不了。
func (h *productPageHandle) ProductDetailPage(c *gin.Context) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		shell.PageError(c, "product", err)
		return
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	// 归属仓下拉（issue #15）：「新建变体 / 生成组合」两个抽屉都要它。
	warehouseOptions, werr := h.warehouseOptions(ctx, selected)
	if werr != nil {
		shell.PageError(c, "product", werr)
		return
	}
	productID := strings.TrimSpace(c.Query("product"))
	hasProduct := false
	product := gin.H{}
	if productID != "" {
		// 只取目标商品那一条：详情页与列表页不同，不需要为整页商品各读一次评分 / 分类。
		if detail, derr := h.products.Get(ctx, &productdto.GetReq{ID: productID}); derr == nil && detail != nil {
			flat, ferr := h.flatCategories(ctx, selected)
			if ferr != nil {
				shell.PageError(c, "product", ferr)
				return
			}
			brands, berr := h.listBrands(ctx, selected)
			if berr != nil {
				shell.PageError(c, "product", berr)
				return
			}
			tags, terr := h.listTags(ctx, selected)
			if terr != nil {
				shell.PageError(c, "product", terr)
				return
			}
			hasProduct = true
			product = h.productRow(ctx, selected, flat, brands, tags, detail)
		}
	}
	data := gin.H{
		"title": "商品详情", "menu": "products",
		"Projects": projects, "SelectedProject": selected,
		"ProductID": productID, "HasProduct": hasProduct, "Product": product,
		"WarehouseOptions": warehouseOptions,
		// 返回列表带上工程上下文：回到列表时不会掉回默认工程。
		"BackURL": "/admin/products?project=" + selected,
		"Err":     strings.TrimSpace(c.Query("err")),
	}
	c.HTML(http.StatusOK, "admin/product_detail.html", shell.Prepare(c, data))
}

// ProductsVariantGenerate 按勾选的属性值批量生成变体组合（issue #8）。
//
// mode=all：不勾选任何值，按商品全部参与变体的属性组 × 全部启用值生成
// （service 的无表单路径，导入 / 接口走同一条）。
// 其它情况按勾选生成；一个都没勾选时直接退回并提示 —— 不静默退化成「全部生成」，
// 那是最容易一次误造出上百个变体的路径。
func (h *productPageHandle) ProductsVariantGenerate(c *gin.Context) {
	projectID := c.PostForm("projectId")
	productID := c.PostForm("productId")
	req := &productdto.GenerateVariantsReq{
		ProductID:   productID,
		WarehouseID: strings.TrimSpace(c.PostForm("warehouseId")),
	}
	if strings.TrimSpace(c.PostForm("mode")) != "all" {
		_ = c.Request.ParseForm()
		keys := make([]string, 0, len(c.Request.PostForm))
		for key := range c.Request.PostForm {
			if strings.HasPrefix(key, variantSelectionPrefix) {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		for _, key := range keys {
			attrID := strings.TrimPrefix(key, variantSelectionPrefix)
			if attrID == "" {
				continue
			}
			req.Selections = append(req.Selections, productdto.VariantSelectionReq{
				AttributeID: attrID, ValueIDs: c.Request.PostForm[key],
			})
		}
		if len(req.Selections) == 0 {
			c.Redirect(http.StatusFound, productDetailLocation(projectID, productID, productenums.ErrVariationSelectionEmpty))
			return
		}
	}
	if _, err := h.products.GenerateVariants(c.Request.Context(), req); err != nil {
		c.Redirect(http.StatusFound, productDetailLocation(projectID, productID, err.Error()))
		return
	}
	c.Redirect(http.StatusFound, productDetailLocation(projectID, productID, ""))
}

// ProductsCreate 新建商品（自动生成首个变体），完成后回到列表。
func (h *productPageHandle) ProductsCreate(c *gin.Context) {
	req := &productdto.CreateReq{
		ProjectID:    c.PostForm("projectId"),
		Name:         c.PostForm("name"),
		Slug:         c.PostForm("slug"),
		AttributeIDs: splitIDs(c.PostForm("attributeIds")),
		// 归属仓（issue #15）：空值即兜底该工程的默认仓。
		WarehouseID: strings.TrimSpace(c.PostForm("warehouseId")),
	}
	if price := strings.TrimSpace(c.PostForm("defaultPrice")); price != "" {
		if v, perr := parseFloat(price); perr == nil {
			req.DefaultPrice = &v
		}
	}
	if _, err := h.products.Create(c.Request.Context(), req); err != nil {
		c.Redirect(http.StatusFound, "/admin/products?project="+req.ProjectID+"&err="+err.Error())
		return
	}
	c.Redirect(http.StatusFound, "/admin/products?project="+req.ProjectID)
}

// ProductsVariantCreate 为商品新增变体（未填字段继承商品级默认值）。
func (h *productPageHandle) ProductsVariantCreate(c *gin.Context) {
	req := &productdto.CreateVariantReq{
		ProductID: c.PostForm("productId"),
		SKUCode:   strings.TrimSpace(c.PostForm("skuCode")),
		// 归属仓（issue #15）：空值即兜底默认仓；无论选没选都会在归属仓生成库存记录。
		WarehouseID: strings.TrimSpace(c.PostForm("warehouseId")),
	}
	if price := strings.TrimSpace(c.PostForm("price")); price != "" {
		if v, perr := parseFloat(price); perr == nil {
			req.Price = &v
		}
	}
	projectID := c.PostForm("projectId")
	if _, err := h.products.CreateVariant(c.Request.Context(), req); err != nil {
		c.Redirect(http.StatusFound, productDetailLocation(projectID, req.ProductID, err.Error()))
		return
	}
	c.Redirect(http.StatusFound, productDetailLocation(projectID, req.ProductID, ""))
}

// ProductsVariantDelete 删除变体。
//
// 表单里的 id 是**变体 id**，回详情的商品 id 只能取 productId 隐藏域（不给 id 回落的机会）。
func (h *productPageHandle) ProductsVariantDelete(c *gin.Context) {
	projectID := c.PostForm("projectId")
	productID := c.PostForm("productId")
	if err := h.products.DeleteVariant(c.Request.Context(), &productdto.DeleteVariantReq{ID: c.PostForm("id")}); err != nil {
		c.Redirect(http.StatusFound, productDetailLocation(projectID, productID, err.Error()))
		return
	}
	c.Redirect(http.StatusFound, productDetailLocation(projectID, productID, ""))
}

// ProductsRatingAdd 补录一条商品评分（issue #33）。
//
// 评分是**独立明细表**（#30），所以这是「加一条记录」而不是「改商品字段」——
// 分值范围由 service 与数据库 CHECK 双重兜底，这里只把非数字提前拦下并给出可读文案。
func (h *productPageHandle) ProductsRatingAdd(c *gin.Context) {
	projectID := c.PostForm("projectId")
	productID := c.PostForm("productId")
	score, perr := strconv.ParseFloat(strings.TrimSpace(c.PostForm("score")), 64)
	if perr != nil {
		c.Redirect(http.StatusFound, productDetailLocation(projectID, productID, "评分必须是 0~5 的数字"))
		return
	}
	if _, err := h.products.AddRating(c.Request.Context(), &productdto.AddRatingReq{
		ProductID:  productID,
		Score:      score,
		OperatorID: builtin.GetUsername(c),
	}); err != nil {
		c.Redirect(http.StatusFound, productDetailLocation(projectID, productID, err.Error()))
		return
	}
	c.Redirect(http.StatusFound, productDetailLocation(projectID, productID, ""))
}

// ProductsRatingDelete 删掉一条评分（issue #33）：录错了能撤掉。
//
// 表单里的 id 是**评分 id**，回详情的商品 id 只能取 productId 隐藏域（不给 id 回落的机会）。
func (h *productPageHandle) ProductsRatingDelete(c *gin.Context) {
	projectID := c.PostForm("projectId")
	productID := c.PostForm("productId")
	if err := h.products.DeleteRating(c.Request.Context(), &productdto.DeleteRatingReq{
		ID: c.PostForm("id"),
		// 工程显式回传（DB-009）：product_ratings 有 FORCE 策略，删一条评分要在工程作用域里。
		ProjectID: projectID,
	}); err != nil {
		c.Redirect(http.StatusFound, productDetailLocation(projectID, productID, err.Error()))
		return
	}
	c.Redirect(http.StatusFound, productDetailLocation(projectID, productID, ""))
}

// ProductsAttributesSet 整体替换某商品引用的属性组（issue #7）。
//
// 引用的组必须是同一工程内真实存在的组（service 校验）；提交空数组即解绑全部。
//
// 表单的隐藏域是 id（值就是商品 id），formProductID 优先认 productId。
func (h *productPageHandle) ProductsAttributesSet(c *gin.Context) {
	projectID := c.PostForm("projectId")
	req := &productdto.UpdateReq{
		ID:           formProductID(c),
		AttributeIDs: splitIDs(c.PostForm("attributeIds")),
	}
	if _, err := h.products.Update(c.Request.Context(), req); err != nil {
		c.Redirect(http.StatusFound, productDetailLocation(projectID, req.ID, err.Error()))
		return
	}
	c.Redirect(http.StatusFound, productDetailLocation(projectID, req.ID, ""))
}

// ProductsDelete 删除商品（连带变体）。
//
// **例外：只有这个端点成功后仍回列表页** —— 商品已经不存在了，回详情页只会看到一个
// 「商品不存在」的空态，用户还得再点一次返回列表。
func (h *productPageHandle) ProductsDelete(c *gin.Context) {
	projectID := c.PostForm("projectId")
	if err := h.products.Delete(c.Request.Context(), &productdto.DeleteReq{ID: c.PostForm("id")}); err != nil {
		c.Redirect(http.StatusFound, "/admin/products?project="+projectID+"&err="+err.Error())
		return
	}
	c.Redirect(http.StatusFound, "/admin/products?project="+projectID)
}

// ProductsBulkDelete 批量删除商品（连同其全部变体，由 service 保证）。
//
// 逐条走同一条删除路径：失败的那条（已不存在 / 被其它数据引用）由服务端拒绝，其余照常删除
// —— 批量操作不能因为一条失败就整批回滚（用户会以为「一条都没删」，然后反复重试）。
// 结果按「已删 N 个 / 跳过 M 个」回带列表页，避免静默的部分成功。
func (h *productPageHandle) ProductsBulkDelete(c *gin.Context) {
	projectID := c.PostForm("projectId")
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		c.Redirect(http.StatusFound, "/admin/products?project="+url.QueryEscape(projectID)+"&err="+url.QueryEscape(berr.Error()))
		return
	}
	deleted, skipped := 0, 0
	for _, id := range ids {
		if err := h.products.Delete(c.Request.Context(), &productdto.DeleteReq{ID: id}); err != nil {
			skipped++
			continue
		}
		deleted++
	}
	target := "/admin/products?project=" + url.QueryEscape(projectID)
	switch {
	case skipped > 0:
		target += "&err=" + url.QueryEscape(fmt.Sprintf("已删除 %d 个，%d 个未能删除（商品不存在或被其它数据引用）", deleted, skipped))
	case deleted > 0:
		target += "&done=" + url.QueryEscape(fmt.Sprintf("已删除 %d 个商品（连同其全部变体）", deleted))
	}
	c.Redirect(http.StatusFound, target)
}
