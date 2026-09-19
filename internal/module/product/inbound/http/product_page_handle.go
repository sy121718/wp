package producthttp

import (
	"context"
	"encoding/json"
	"errors"
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
contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	presentationdto "go_wp/internal/module/presentation/dto"
		productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	inventorycontract "go_wp/internal/module/product/inventory/contract"
	inventoryenums "go_wp/internal/module/product/inventory/enums"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/web/shell"
	"go_wp/pkg/logger"
)

// product_handle.go — 后台商品管理页（issue #5 / T3a）。

//

// 独立于 dashboard 的通用 Handle：只依赖 product 契约与 project 契约，

// 避免把商品依赖掺进 dashboard 的通用装配。

//

// 交互遵循后台规范：GET 渲染完整页，POST 处理完 302 回列表（原生表单 + csrf_token 隐藏域）。

// variantSelectionPrefix 组合生成表单里「属性组 → 勾选值」的字段名前缀。

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
	// 归属仓下拉（issue #15）与多仓勾选：变体 / 首建商品的归属仓在这里选（不选 = 默认仓）。
	// **必须在商品行之前取好**：列表「库存」列的分仓明细要按工程仓库清单补齐未入库的仓。
	warehouseOptions, werr := h.warehouseOptions(ctx, selected)
	if werr != nil {
		shell.PageError(c, "product", werr)
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
			rows = append(rows, h.productRow(ctx, selected, flat, brands, tags, warehouseOptions, detail))
		}
	}
	// 属性组勾选列表（本批）：新建抽屉引用属性组时不再手打 UUID —— 可选值由服务端给出
	// （与归属仓同一手法），用户看到的是一排「颜色（color）」，而不是一个暗示 UUID 的输入框。
	attributeOptions, aerr := h.attributeOptions(ctx, selected)
	if aerr != nil {
		shell.PageError(c, "product", aerr)
		return
	}
	// 「从仓库选」的候选（docs/14 §1.1 入口 A，迁移 251）：按仓分组的仓库 SKU 清单。
	// 前端只是便捷入口 —— 服务端在 Create 里会带着仓库 id 再复核一遍（不信任前端）。
	warehouseSKUGroups, wserr := h.warehouseSKUOptions(ctx, selected, warehouseOptions)
	if wserr != nil {
		shell.PageError(c, "product", wserr)
		return
	}
	// 批量改价抽屉的规则清单与表单初值：三样全部取自定价工具（ListPricingRuleTypes /
	// ListPricingRoundingOptions / defaultPricingForm）—— 抽屉与独立定价页共用同一个
	// 规则表单片段（partials/pricing_rule_fields.html），键名或取值来源分叉会让其中
	// 一处渲染成空下拉。
	pricingRules := h.products.ListPricingRuleTypes(ctx)
	pricingRoundings := h.products.ListPricingRoundingOptions(ctx)
	// withCSRF：注入 csrf_token（POST 表单隐藏域）+ 导航树 + 权限码 + 多语言，
	// 与其它后台页面同一渲染入口（缺 token 时表单提交会被 CSRF 中间件挡下）。
	c.HTML(http.StatusOK, "admin/products.html", shell.Prepare(c, gin.H{
		"title":            MsgProductsTitle,
		"menu":             "products",
		"Projects":         projects,
		"SelectedProject":  selected,
		"WarehouseOptions": warehouseOptions,
		"AttributeOptions": attributeOptions,
		// 可选键：未接库存契约 / 该工程的仓库里还没有货时是空数组，模板据 isset + len
		// 整块跳过（直接渲染模板的单测不带这个键，缺键会让整页在此中断）。
		"WarehouseSKUOptions": warehouseSKUGroups,
		// 建表单片段（partials/product_create_form.html）在列表页是抽屉形态：
		// 渲染「取消」按钮关闭抽屉；新建整页不设该键。
		"InDrawer": true,
		"Rules":               pricingRules,
		"Roundings":           pricingRoundings,
		"Form":                defaultPricingForm(pricingRules, pricingRoundings),
		"Products":            rows,
		// 上一步的错误（上限拒绝 / 参数错误）经查询串回显 —— 读侧一律过白名单
		//（product_err.go）：查询参数不是可信边界。
		"Err": productPageErr(c),
		// 批量删除的结果回带（?done=）：部分失败仍走 err（见 ProductsBulkDelete）。
		"Done": productPageDone(c),
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
	warehouses []gin.H, detail *productdto.ProductResp) gin.H {
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
		// 主体 SKU（products.sku_code）：详情页基本信息区只读展示 + 一句话说明唯一性范围。
		"SKUCode":  detail.SKUCode,
		"Status":   detail.Status,
		"PriceMin": formatAmount(detail.PriceMin), "PriceMax": formatAmount(detail.PriceMax),
		"VariantCount": detail.VariantCount,
		// 变体**清单**行（docs/14 §8）：种子是库里已有的变体（不是重算笛卡尔积），
		// 行上带可编辑的 SKU 与规范化后的 option_values（保存时原样回传）。
		"Variants": variantListRows(detail),
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
		// 库存列（docs/14 §1.4）：三态 + 分仓明细。真源是 inventory_stocks，
		// 这里只是把服务端算好的聚合结果整理成页面要的形状（服务端返回的是**有行**的仓，
		// 未入库的仓由工程仓库清单补齐）。
		"Stock": productStockCell(detail, warehouses),
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
			product = h.productRow(ctx, selected, flat, brands, tags, warehouseOptions, detail)
			// 捆绑容器（type=bundle）在详情页多一块「捆绑构成」编辑区：成员只作为选项与
			// 履约明细，容器价才是套餐价。type=variant 时**不给这个键**，模板据 isset 整块跳过
			// —— 区块的入口就在详情页，不该再要求运营去另一个菜单页找它。
			if isBundleProduct(detail.Type) {
				product["Bundle"] = h.bundlePanel(c, selected, detail.ID)
			}
		}
	}
	data := gin.H{
		"title": "商品详情", "menu": "products",
		"Projects": projects, "SelectedProject": selected,
		"ProductID": productID, "HasProduct": hasProduct, "Product": product,
		"WarehouseOptions": warehouseOptions,
		// 返回列表带上工程上下文：回到列表时不会掉回默认工程。
		"BackURL": "/admin/products?project=" + selected,
		// 读侧一律过白名单（product_err.go 的 productFacingNotice）。
		"Err": productPageErr(c),
		// 变体清单保存的结论（?done=）：新增 / 改 SKU / 删除的计数与逐条跳过原因。
		// 与 ?err= 分开是必须的 —— 保存**成功但有跳过**时两条都要能看见。
		"Done": productPageDone(c),
	}
	// 详情页模板面板（docs/04-C-instance-override.md §5）：当前绑定与可视化自定义入口。
// 三态：未绑定（引导首次发布）/ 已绑定（预览 + 进入自定义）/ 能力未装配（降级提示）。
tplPanel := gin.H{"Avail": false}
if hasProduct && h.templates != nil && h.instances != nil {
	tplPanel["Avail"] = true
	if inst, ierr := h.instances.GetByEntity(ctx, &presentationdto.GetByEntityReq{
		EntityType: productEntityType, EntityID: productID, ProjectID: selected,
	}); ierr == nil && inst != nil {
		tplPanel["InstanceID"] = inst.ID
		tplPanel["TemplateID"] = inst.TemplateID
		tplPanel["Published"] = inst.Status == "published" || inst.Status == "active"
		tplPanel["URLPath"] = inst.URLPath
		tplPanel["PreviewQS"] = "template=" + inst.TemplateID + "&entityType=product&entityId=" +
			productID + "&projectId=" + selected
	}
	if rows, terr := h.templates.List(ctx, &contenttemplatedto.ListReq{EntityType: productEntityType}); terr == nil {
		tplPanel["Templates"] = rows
	}
}
data["TplPanel"] = tplPanel
c.HTML(http.StatusOK, "admin/product_detail.html", shell.Prepare(c, data))
}

// variantSelectionFromForm 收「生成组合」表单里按前缀提交的勾选（attr:<属性组 id> → 值 id）。
//
// 生成（落库路径）、预览（不落库）两条 handler 共用它：字段名与收拢规则只有一份，
// 两处各写一遍必然分叉（一个认 attr: 前缀、另一个漏读，表现为「预览有行、保存却没数据」）。
func variantSelectionFromForm(c *gin.Context) []productdto.VariantSelectionReq {
	_ = c.Request.ParseForm()
	keys := make([]string, 0, len(c.Request.PostForm))
	for key := range c.Request.PostForm {
		if strings.HasPrefix(key, variantSelectionPrefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	out := make([]productdto.VariantSelectionReq, 0, len(keys))
	for _, key := range keys {
		attrID := strings.TrimPrefix(key, variantSelectionPrefix)
		if attrID == "" {
			continue
		}
		out = append(out, productdto.VariantSelectionReq{
			AttributeID: attrID, ValueIDs: c.Request.PostForm[key],
		})
	}
	return out
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
		req.Selections = variantSelectionFromForm(c)
		if len(req.Selections) == 0 {
			// 裸 enums key 铺到页面上只会显示 ErrVariationSelectionEmpty —— 走取词助手拿中文。
			c.Redirect(http.StatusFound, productDetailLocation(projectID, productID,
				productErrText(c, errors.New(productenums.ErrVariationSelectionEmpty))))
			return
		}
	}
	if _, err := h.products.GenerateVariants(c.Request.Context(), req); err != nil {
		// 非业务错误的原文（PG / 构建器）只进日志，对外给归口文案。
		c.Redirect(http.StatusFound, productDetailLocation(projectID, productID, productErrText(c, err)))
		return
	}
	c.Redirect(http.StatusFound, productDetailLocation(projectID, productID, ""))
}

// variantPreviewRow 预览行的 JSON 形状：前端拿它渲染清单里的一行。
//
// Spec 是**服务端**拼的可读规格文本（与详情页表格同一份 specLabel），
// 前端不自己拼属性名 —— 属性值改名后前端那份就会显示旧名字。
type variantPreviewRow struct {
	SKUCode      string          `json:"skuCode"`
	OptionValues json.RawMessage `json:"optionValues"`
	OptionKey    string          `json:"optionKey"`
	Spec         string          `json:"spec"`
}

// ProductsVariantPreview 组合生成的**预览**（POST /admin/products/variant/preview，不落库）。
//
// 用户口径（docs/14 §8）：「生产只是显示，并不会存入数据库，必须保存才行」。
// 因此本端点只回答「点这次生成会往前端清单里追加哪些行」：
//   - 笛卡尔积、维度与数量上限、与既有组合的去重全在 service（与落库路径同一套规则）；
//   - SKU 由服务端按变体 SKU 规则生成（前端不复刻这套算法），运营可在清单里逐行改写；
//   - 响应是 JSON 而不是 HTMX 片段：前端要把这些行**追加**进已渲染的清单 DOM，
//     而清单的初始行由服务端渲染（库里已有变体），两者是同一种行结构。
//
// 失败回 400 + 可读文案（走 productErrText 取词，不把 enums 裸 key 或 PG 报错铺给前端）。
func (h *productPageHandle) ProductsVariantPreview(c *gin.Context) {
	ctx := c.Request.Context()
	productID := strings.TrimSpace(c.PostForm("productId"))
	req := &productdto.PreviewVariantReq{
		ProductID:   productID,
		ProjectID:   strings.TrimSpace(c.PostForm("projectId")),
		WarehouseID: strings.TrimSpace(c.PostForm("warehouseId")),
		Selections:  variantSelectionFromForm(c),
	}
	// 清单里已有的组合原样回传（每行一条）：由服务端归一去重，前端不需要懂 optionKey。
	for _, raw := range c.PostFormArray("optionValues") {
		if trimmed := strings.TrimSpace(raw); trimmed != "" {
			req.ExistingOptionValues = append(req.ExistingOptionValues, json.RawMessage(trimmed))
		}
	}
	res, err := h.products.PreviewVariantCombinations(ctx, req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "message": productErrText(c, err)})
		return
	}
	attrs := []*productdto.AttributeResp{}
	if detail, derr := h.products.Get(ctx, &productdto.GetReq{ID: productID}); derr == nil && detail != nil {
		attrs = detail.Attributes
	}
	rows := make([]variantPreviewRow, 0, len(res.Rows))
	for _, row := range res.Rows {
		if row == nil {
			continue
		}
		rows = append(rows, variantPreviewRow{
			SKUCode: row.SKUCode, OptionValues: row.OptionValues,
			OptionKey: row.OptionKey, Spec: specLabel(row.OptionValues, attrs),
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"ok": true, "total": res.Total, "skipped": res.Skipped, "rows": rows,
	})
}

// ProductsVariantSave 以清单为准保存变体（POST /admin/products/variant/save）——**唯一的落库动作**。
//
// 表单里的 rows 是一段 JSON（前端清单的全部行，顺序即期望顺序）：行上只有
// variantId（既有变体；新增行为空）、可编辑的 skuCode 与 optionValues。
// 服务端逐条重算（见 service.SaveVariantList），前端提交的形状一律不作数。
//
// 结果回详情页：成功走 ?done=（信息条，含「新增 / 改 SKU / 删除」与逐条跳过原因），
// 失败走 ?err= —— 与详情页其它写动作同一套 PRG 出口，不新增渲染通道。
func (h *productPageHandle) ProductsVariantSave(c *gin.Context) {
	ctx := c.Request.Context()
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	productID := strings.TrimSpace(c.PostForm("productId"))
	req := &productdto.SaveVariantListReq{
		ProductID:   productID,
		ProjectID:   projectID,
		WarehouseID: strings.TrimSpace(c.PostForm("warehouseId")),
		// 操作人取自会话，表单字段不作数（变体留痕的操作人不可伪造）。
		OperatorID: shell.CurrentUserIDText(c),
	}
	if raw := strings.TrimSpace(c.PostForm("rows")); raw != "" {
		if uerr := json.Unmarshal([]byte(raw), &req.Rows); uerr != nil {
			// 前端拼错了 JSON：这属于调用方参数错误，给可读提示而不是 PG / JSON 原始报错。
			redirectWhere(c, productDetailLocation(projectID, productID,
				productErrText(c, errors.New(productenums.ErrInvalidParam))))
			return
		}
	}
	// 这里不再预取商品详情（只为拼「哪个 SKU 被跳过」）：回带文案只报原因枚举，
	// 逐行 SKU 由详情页本身呈现（见 variantSaveNotice 的说明），因此少一次 DB 往返。
	res, err := h.products.SaveVariantList(ctx, req)
	if err != nil {
		redirectWhere(c, productDetailLocation(projectID, productID, productErrText(c, err)))
		return
	}
	redirectWhere(c, productDetailLocationWith(projectID, productID, "", variantSaveNotice(c, res)))
}

// variantSkipFallbacks 清单外删除被跳过时的中文兜底（key 见 productenums.VariantSkip*）。
var variantSkipFallbacks = map[string]string{
	productenums.VariantSkipHasStock:       "仍有库存，未删除（请先处理库存或改为停用）",
	productenums.VariantSkipReferenced:     "被 BOM 清单引用，未删除（请先解除引用）",
	productenums.VariantSkipDuplicated:     "与清单里前面的行重复，已忽略",
	productenums.VariantSkipVariantMissing: "这一行对应的变体已不存在（可能已被别处删除）",
	// 本批新增的两个引用面（迁移 259）：被捆绑成员引用 / 有过库存流水。
	productenums.VariantSkipBundleReferenced: "被捆绑成员引用，未删除（请先在捆绑配置里解除引用）",
	productenums.VariantSkipHasMovement:      "有过库存流水（已被订单或库存变动用过），未删除（请改为停用）",
}

// variantSaveNotice 保存结果的回带文案：计数 + 逐条跳过原因。
//
// 「一条都没变」与「跳过若干」必须能区分：都写成「保存成功」时，用户无法判断是
// 清单本来就与库里一致，还是有行被拦下了（被拦下的行下一次还会出现在清单里）。
func variantSaveNotice(c *gin.Context, res *productdto.SaveVariantListResp) string {
	tr := shell.TranslateFor(c)
	if res == nil {
		return tr(shell.MsgInternalError, productErrInternalFallback)
	}
	if res.Created == 0 && res.Updated == 0 && res.Deleted == 0 && len(res.Skipped) == 0 {
		return productVariantSaveNoChange
	}
	parts := []string{fmt.Sprintf(productVariantSaveSaved, res.Created, res.Updated, res.Deleted)}
	if len(res.Skipped) > 0 {
		// 只报**原因**，不再把「哪个 SKU / 什么规格」拼进 URL：
		// 这条文案经 ?done= 回到详情页再渲染，而 URL 不是可信边界 ——
		// 受控形态下（每条原因都来自 variantSkipFallbacks 的枚举）读侧才能逐段校验
		//（见 product_err.go 的 productVariantNoticeMatches）。
		// 被跳过的行本来就留在清单里，逐行 SKU 由页面本身呈现。
		items := make([]string, 0, len(res.Skipped))
		for _, skip := range res.Skipped {
			items = append(items, tr(skip.Reason, variantSkipFallbacks[skip.Reason]))
		}
		parts = append(parts, fmt.Sprintf(productVariantSaveSkipped, len(res.Skipped), strings.Join(items, "；")))
	}
	return strings.Join(parts, " ")
}

// ProductsCreate 新建商品。
//
// 两条本批确定的语义：
//
//  1. **type 必须读**（迁移 238 的商品类型）：字段一旦漏读，service 会把空串兜底成
//     variant —— 建「捆绑容器」得到的是一个带自己首个变体的常规商品，而且返回 302 成功、
//     列表页也看得见，不留任何错误线索。
//  2. **成功去商品详情页**，不是回列表：抽屉只有名称 / 类型 / URL 段 / 默认价 / 归属仓 /
//     属性组几个字段，而 CreateReq 有 40+ 个字段（描述、图片、分类、品牌、标签、SEO…），
//     商品详情页才是全量录入的地方。bundle 更明显 —— 它没有变体，回列表看不出任何进展。
//     失败仍回列表页（抽屉所在的页）并把中文结论经 ?err= 带回：用户就在那里重填。
func (h *productPageHandle) ProductsCreate(c *gin.Context) {
	req := &productdto.CreateReq{
		ProjectID: c.PostForm("projectId"),
		Name:      c.PostForm("name"),
		// 商品类型：variant（常规变体商品）｜bundle（捆绑容器，只有容器价）。
		Type: strings.TrimSpace(c.PostForm("type")),
		Slug: c.PostForm("slug"),
		// 主体 SKU 编码（表单 name=sku，规则见 docs/14）：留空即派生（变体商品取 URL 段、
		// 捆绑取 <商品段>_B）；填了且选了仓库 → 变体商品自动加仓库码前缀。
		// **必须透传**：漏读等于运营填了个寂寞 —— 服务端会静默按 URL 段派生另一个编码，
		// 建完看不出任何异常，直到与仓库对不上号。
		SKUCode: strings.TrimSpace(c.PostForm("sku")),
		// SKU 来源（docs/14 §1.1 的两条入口，迁移 251）：自己创建 / 从仓库选。
		// **必须透传**：漏读就等于运营在抽屉里选了「从仓库选」却拿到一个按 URL 段
		// 派生的商品，建完看不出任何异常 —— 与上面 sku 漏读是同一类静默失败。
		SKUSource:    strings.TrimSpace(c.PostForm("skuSource")),
		WarehouseSKU: strings.TrimSpace(c.PostForm("warehouseSku")),
		ExternalSKU:  strings.TrimSpace(c.PostForm("externalSku")),
		// 属性组：抽屉是勾选列表（同名多值），保留了逗号分隔单值的兼容形态。
		AttributeIDs: attributeIDsFromForm(c),
		// 归属仓：空值即兜底该工程的默认仓（单值形态保留，接口 / 脚本路径仍可用）。
		WarehouseID: strings.TrimSpace(c.PostForm("warehouseId")),
		// 多仓勾选（2026-09-19 口径）：勾了哪些仓就在哪些仓各建一行（裸码），
		// 第一个仓是**认领仓**（决定主体 SKU 的仓码前缀）。同名多值 → PostFormArray，
		// 一个都没勾时浏览器不提交这个字段（服务端按「默认仓」处理）。
		WarehouseIDs: c.PostFormArray("warehouseIds"),
	}
	if price := strings.TrimSpace(c.PostForm("defaultPrice")); price != "" {
		if v, perr := parseFloat(price); perr == nil {
			req.DefaultPrice = &v
		}
	}
	// 数量（迁移 261 的口径）：**不填数量 = 不跟踪 = 无限**。模板是一个「跟踪数量」开关
	// 加一个数量框：开关没勾时数量框禁用、不提交 ⇒ 这里读不到值 ⇒ Quantity 保持 nil。
	// 开关勾了而数量框留空 ⇒ 写 0（0 是「明确没货」，与「没填」是两回事）。
	if strings.TrimSpace(c.PostForm("trackQuantity")) != "" {
		qty := 0
		if raw := strings.TrimSpace(c.PostForm("quantity")); raw != "" {
			v, qerr := strconv.Atoi(raw)
			if qerr != nil || v < 0 {
				c.Redirect(http.StatusFound, productListURL(req.ProjectID, listErrMark, productQuantityInvalidText))
				return
			}
			qty = v
		}
		req.Quantity = &qty
	}
	created, err := h.products.Create(c.Request.Context(), req)
	if err != nil {
		// 业务错误的 Error() 是 enums 常量（= i18n key），直接铺到页面上就是
		// 「列表页显示 ErrBundlePriceRequired」的来源：统一经 productErrText 取词。
		c.Redirect(http.StatusFound, productListURL(req.ProjectID, listErrMark, productErrText(c, err)))
		return
	}
	if created != nil && created.ID != "" {
		c.Redirect(http.StatusFound, productDetailLocation(req.ProjectID, created.ID, ""))
		return
	}
	// 兜底：拿到商品 id 才谈得上「进详情继续编辑」，否则回列表而不是构造一个空详情页。
	c.Redirect(http.StatusFound, productListURL(req.ProjectID, listDoneMark, ""))
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
		c.Redirect(http.StatusFound, productDetailLocation(projectID, req.ProductID, productErrText(c, err)))
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
		c.Redirect(http.StatusFound, productDetailLocation(projectID, productID, productErrText(c, err)))
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
		c.Redirect(http.StatusFound, productDetailLocation(projectID, productID, productErrText(c, err)))
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
		c.Redirect(http.StatusFound, productDetailLocation(projectID, productID, productErrText(c, err)))
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
		c.Redirect(http.StatusFound, productDetailLocation(projectID, req.ID, productErrText(c, err)))
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
		// 离开详情页的特殊端点也不手拼 URL：统一走 productListURL（工程与文案都做 URL 编码）。
		c.Redirect(http.StatusFound, productListURL(projectID, listErrMark, productErrText(c, err)))
		return
	}
	c.Redirect(http.StatusFound, productListURL(projectID, "", ""))
}

// —— 商品列表页的批量「按规则改价」与错误文案 ——

// 列表页的两个结论标记：err 走警告条（需要处理），done 走信息条（仅供参考）。
const (
	listErrMark  = "err"
	listDoneMark = "done"
)

// bulkPricingNothingSelected 没勾选就提交的意见（与批量删除同一口径：说清怎么继续）。
const bulkPricingNothingSelected = "批量改价：没有勾选任何商品，请先勾选左侧复选框再执行。"

// productListURL 商品列表页的回跳地址（PRG）：保留工程上下文并带上一条结论文案。
//
// 列表页只有 ?err=（警告）与 ?done=（信息）两个渲染位：handler 不另开一条「把内部错误
// 铺到页面上」的通道 —— 直出 err.Error() 的结果是页面上出现 ErrBundlePriceRequired
// 这类裸 key（enums 常量即 i18n key）。与 block 模块的 blockListURL 同一形状。
func productListURL(projectID, mark, text string) string {
	q := url.Values{}
	if p := strings.TrimSpace(projectID); p != "" {
		q.Set("project", p)
	}
	if e := strings.TrimSpace(text); e != "" && strings.TrimSpace(mark) != "" {
		q.Set(mark, e)
	}
	if enc := q.Encode(); enc != "" {
		return "/admin/products?" + enc
	}
	return "/admin/products"
}

// redirectWhere 页面写动作的 PRG 出口。
//
// 原生表单（列表页的批量删除、新建抽屉）走 302；HTMX 请求（批量改价抽屉里的 hx-post）
// 走 **HX-Redirect**：htmx 的 XHR 会自己跟随 302，最终响应里已经读不到 Location，
// 只有响应头上的 HX-Redirect 能让它整页跳转（否则会把整页 HTML 塞进抽屉里）。
// 两条路的终点是同一个 URL，页面壳与提示位完全一致。
func redirectWhere(c *gin.Context, target string) {
	if strings.EqualFold(strings.TrimSpace(c.GetHeader("HX-Request")), "true") {
		c.Header("HX-Redirect", target)
		c.Status(http.StatusOK)
		return
	}
	c.Redirect(http.StatusFound, target)
}

// productErrInternalFallback 非业务错误的兜底文案（兼作取词兜底）。
const productErrInternalFallback = "系统内部错误，请稍后重试"

// productErrSentinels 会回带到后台页面的商品域业务错误（= 词条 key 的来源）。
//
// 商品域的业务错误是 enums 常量（string）经 errors.New 造出来的，服务端还会用
// fmt.Errorf("%s：补充说明", key) 把 key 拼进整句话 —— 两种形态都在 productErrKey 里认。
var productErrSentinels = []string{
	productenums.ErrInvalidParam,
	productenums.ErrNotFound,
	productenums.ErrSlugTaken,
	productenums.ErrSkuTaken,
	productenums.ErrNameRequired,
	productenums.ErrAttrNotFound,
	productenums.ErrAttrProjectMismatch,
	productenums.ErrProductTypeInvalid,
	productenums.ErrProductTypeImmutable,
	productenums.ErrBundlePriceRequired,
	// 主体 SKU 编码（迁移 248 / 249 配套）：三条都是**可行动**的提示（显式填一个编码 /
	// 把 URL 段改成 ASCII / 给捆绑商品填一个主体编码）。漏登记就会被当成非业务错误，
	// 用户只看到「系统内部错误」，运营拿不到任何下一步线索。
	productenums.ErrSkuContainerMissing,
	productenums.ErrContainerSkuInvalid,
	productenums.ErrBundleSKURequired,
	// 主体 SKU 在工程内被别的商品占用（迁移 246 的偏唯一索引）：预检与唯一索引兜底
	// 都映射到这一条。漏登记就会退回「系统内部错误」，运营拿不到「换一个编码」这个下一步。
	productenums.ErrContainerSKUTaken,
	// 变体清单的「保存」（迁移 253 配套）：两条都是**可行动**的提示（清单某行的 SKU 为空 /
	// 某行的规格组合不合法）。漏登记会被当成非业务错误，运营只看到「系统内部错误」，
	// 而清单里有问题的那一行根本指不出来。
	productenums.ErrVariantSKUEmpty,
	productenums.ErrVariantOptionsInvalid,
	// 变体删除守卫的四个引用面（迁移 259 配套）：三条新原因（捆绑成员引用 / 有过库存流水）
	// 与之共用的自引用判定都是**可行动**的提示（先去解绑捆绑、或改为停用）。
	// 单条删除路径经 ?err= 回带，漏登记就只剩「系统内部错误」。
	productenums.VariantSkipBundleReferenced,
	productenums.VariantSkipHasMovement,
	productenums.ErrVariantHasStock,
	// 捆绑成员来源解析（迁移 259 配套）：两个请求级错误 + 自引用 ——
	// 解析端点是 JSON 接口，错误文案经 productErrText 取词后回给前端。
	productenums.ErrBundleSourceInvalid,
	productenums.ErrBundleSourceProductRequired,
	productenums.ErrBundleSourceWarehouseRequired,
	productenums.ErrBundleSelfReference,
	// 仓库 SKU 与外部编码（迁移 251 / 词条 252）：新建抽屉的「从仓库选」入口住在商品列表页，
	// 它抛出的错误必须在**这一页**变成可读中文，否则用户只看到「系统内部错误」。
	inventoryenums.ErrSKUSourceInvalid,
	inventoryenums.ErrWarehouseSKURequired,
	inventoryenums.ErrWarehouseSKUNotFound,
	inventoryenums.ErrWarehouseSKUBundleNotAllowed,
	inventoryenums.ErrWarehouseSKUCodeTaken,
	inventoryenums.ErrExternalSKUInvalid,
	inventoryenums.ErrExternalSKUProductConflict,
	// 归属仓解析（既有 key / 词条 242）：「从仓库选」要先解析出仓库，仓库不存在 / 已停用 /
	// 不属于本工程都是运营能自己修的（换一个仓），不该被当成内部错误。
	inventoryenums.ErrWarehouseNotFound,
	inventoryenums.ErrWarehouseDisabled,
	inventoryenums.ErrWarehouseProjectMismatch,
	inventoryenums.ErrWarehouseDefaultMissing,
	// 定价（批量改价抽屉逐商品回带的原因都出自这里）
	productenums.ErrPricingRuleTypeInvalid,
	productenums.ErrPricingRuleParamsInvalid,
	productenums.ErrPricingRoundingInvalid,
	productenums.ErrPricingScopeInvalid,
	productenums.ErrPricingTargetRequired,
	productenums.ErrPricingTargetNotFound,
	productenums.ErrPricingFilterEmpty,
	productenums.ErrPricingNothingChanged,
	productenums.ErrPricingTargetTooMany,
	productenums.ErrPricingAdjustmentNotFound,
	// —— 后台页全部写操作改走 productErrText 后的**完整性补齐**（第三波 CQ-009）——
	//
	// 上面那批是「按需登记」（某个入口先暴露了才补一条）。一旦所有页面写操作的
	// err.Error() 都改走 productErrText，白名单就从「补充」变成了**唯一出口**：
	// 漏登记一条 = 运营看到「系统内部错误」而原来（裸 key）至少还能对上号。
	// 因此这里把本模块 enums 里全部业务错误一次补齐（属性 / 分类 / 品牌 / 标签 /
	// 捆绑配置与选择 / 变体组合），与 internal/module/product/enums/product_enums.go 对齐。
	productenums.ErrAttrInUse,
	productenums.ErrAttrKeyRequired,
	productenums.ErrAttrKeyTaken,
	productenums.ErrAttrNameRequired,
	productenums.ErrBrandInUse,
	productenums.ErrBrandNameRequired,
	productenums.ErrBrandNotFound,
	productenums.ErrBrandProjectMismatch,
	productenums.ErrBrandSlugTaken,
	productenums.ErrBundleMaxOptionsInvalid,
	productenums.ErrBundleNotConfigured,
	productenums.ErrBundleOptionRequired,
	productenums.ErrBundleOptionsExceeded,
	productenums.ErrBundleQtyAboveMax,
	productenums.ErrBundleQtyAboveStock,
	productenums.ErrBundleQtyBelowMin,
	productenums.ErrBundleQtyInvalid,
	productenums.ErrBundleQtyRangeInvalid,
	productenums.ErrBundleShapeInvalid,
	productenums.ErrBundleStockUnavailable,
	productenums.ErrBundleTotalAboveMax,
	productenums.ErrBundleTotalBelowMin,
	productenums.ErrBundleTotalRangeInvalid,
	productenums.ErrBundleTotalUnreachable,
	productenums.ErrBundleVariantDuplicated,
	productenums.ErrBundleVariantNotFound,
	productenums.ErrBundleVariantNotInConfig,
	productenums.ErrBundleVariantProjectMismatch,
	productenums.ErrBundleVariantRequired,
	productenums.ErrCategoryCycle,
	productenums.ErrCategoryHasChildren,
	productenums.ErrCategoryInUse,
	productenums.ErrCategoryNameRequired,
	productenums.ErrCategoryNotFound,
	productenums.ErrCategoryParentMismatch,
	productenums.ErrCategoryProjectMismatch,
	productenums.ErrCategorySlugTaken,
	productenums.ErrCollectionFilterInvalid,
	productenums.ErrCollectionSourceInvalid,
	productenums.ErrInvalidField,
	productenums.ErrInvalidType,
	productenums.ErrTagKindInvalid,
	productenums.ErrTagNameRequired,
	productenums.ErrTagNotFound,
	productenums.ErrTagNotManual,
	productenums.ErrTagProjectMismatch,
	productenums.ErrTagRuleNotAllowed,
	productenums.ErrTagRuleParamsInvalid,
	productenums.ErrTagRuleTypeInvalid,
	productenums.ErrTagSlugTaken,
	productenums.ErrVariationAttributeInvalid,
	productenums.ErrVariationCountLimit,
	productenums.ErrVariationDimensionLimit,
	productenums.ErrVariationNoDimension,
	productenums.ErrVariationValueInvalid,
	// ErrVariationSelectionEmpty 只在 handler 侧产生（「一个属性值都没勾选就别生成」），
	// service 里看不到它 —— 按「service 用到的 key」做完整性对账时会漏掉这一条，
	// 漏了它页面上就从「裸 key」直接掉成「系统内部错误」。
	productenums.ErrVariationSelectionEmpty,
	productenums.VariantSkipDuplicated,
	productenums.VariantSkipHasStock,
	productenums.VariantSkipReferenced,
	productenums.VariantSkipVariantMissing,
}

// productErrFallbacks 上述业务错误的中文兜底（i18n 未初始化或该 key 没有词条时用）。
//
// 已 seed 的 key（如 ErrBundlePriceRequired 见迁移 239）在真实请求里取 sys_i18n 的文案，
// 这里的兜底与 seed 保持一致，避免「同一句话在测试与线上不一样」。
var productErrFallbacks = map[string]string{
	productenums.ErrInvalidParam:                   "请求参数无效",
	productenums.ErrNotFound:                       "商品或变体不存在",
	productenums.ErrSlugTaken:                      "该 URL 段在本工程已被占用",
	productenums.ErrSkuTaken:                       "该 SKU 编码在本商品下已被占用",
	productenums.ErrNameRequired:                   "商品名称必填",
	productenums.ErrAttrNotFound:                   "属性组不存在",
	productenums.ErrAttrProjectMismatch:            "属性组不属于该商品所在工程",
	productenums.ErrProductTypeInvalid:             "商品类型不合法（仅支持变体商品或捆绑商品）",
	productenums.ErrProductTypeImmutable:           "商品类型在创建后不可更改",
	productenums.ErrBundlePriceRequired:            "捆绑商品必须自定价：容器价需大于 0",
	productenums.ErrSkuContainerMissing:            "SKU 编码无法生成：商品 URL 段不含 ASCII 字符，请显式填写 SKU 编码",
	productenums.ErrContainerSkuInvalid:            "主体 SKU 不合法：必须是非空字符串",
	productenums.ErrBundleSKURequired:              "捆绑商品必须填写主体 SKU 编码（新建抽屉已给出建议值，可直接修改）",
	productenums.ErrContainerSKUTaken:              "该主体 SKU 在本工程已被其它商品占用（主体编码在工程内唯一）",
	productenums.ErrVariantSKUEmpty:                "变体的 SKU 编码不能为空：请填写一个编码（或改回系统生成的编码）",
	productenums.ErrVariantOptionsInvalid:          "变体的规格组合不合法：属性组或属性值不属于该商品",
	productenums.ErrVariantHasStock:                "变体仍有库存，不能删除（请改为停用）",
	productenums.VariantSkipBundleReferenced:       "该变体被捆绑成员引用，不能删除（请先在捆绑配置里解除引用）",
	productenums.VariantSkipHasMovement:            "该变体有过库存流水（已被订单或库存变动用过），不能删除（请改为停用）",
	productenums.ErrBundleSourceInvalid:            "成员来源不合法：只支持「从商品导入」「从仓库选」「自选属性值组合」",
	productenums.ErrBundleSourceProductRequired:    "该来源必须先选一个来源商品",
	productenums.ErrBundleSourceWarehouseRequired:  "从仓库选时必须指定仓库并至少勾选一条仓库 SKU",
	productenums.ErrBundleSelfReference:            "捆绑成员不能引用捆绑主体自己（自引用）",
	inventoryenums.ErrSKUSourceInvalid:             "SKU 来源取值不合法：只支持「自己创建」与「从仓库选」",
	inventoryenums.ErrWarehouseSKURequired:         "从仓库选 SKU 时必须先选仓库并指明该仓的那条仓库 SKU",
	inventoryenums.ErrWarehouseSKUNotFound:         "该仓库里没有这条仓库 SKU：请确认仓库选对了，或先在该仓建好这条货的库存记录",
	inventoryenums.ErrWarehouseSKUBundleNotAllowed: "捆绑商品不存在于仓库，不能用「从仓库选」建主体 SKU，请改用自己的编码",
	inventoryenums.ErrWarehouseSKUCodeTaken:        "该仓已有同一条 SKU 编码的库存行（我们自己的 SKU 在仓内唯一），请改用另一个仓库或另一个仓库 SKU",
	inventoryenums.ErrExternalSKUInvalid:           "外部编码不合法：长度需在 128 个字符以内且不含控制字符",
	inventoryenums.ErrExternalSKUProductConflict:   "该外部编码在本仓已挂在另一个商品上：同一个外部编码在同一仓库内只能属于同一个商品（同一商品的多个口味可以共用一个外部编码）",
	inventoryenums.ErrWarehouseNotFound:            "仓库不存在",
	inventoryenums.ErrWarehouseDisabled:            "已停用的仓库不能作为归属仓",
	inventoryenums.ErrWarehouseProjectMismatch:     "仓库不属于该工程",
	inventoryenums.ErrWarehouseDefaultMissing:      "该工程没有默认仓可兜底，请先建一个仓库并设为默认仓",
	productenums.ErrPricingRuleTypeInvalid:         "定价规则类型不是内置类型",
	productenums.ErrPricingRuleParamsInvalid:       "定价规则参数不合法（参数键 / 类型 / 取值范围）",
	productenums.ErrPricingRoundingInvalid:         "尾数处理不是内置选项",
	productenums.ErrPricingScopeInvalid:            "定价作用范围不合法",
	productenums.ErrPricingTargetRequired:          "该作用范围必须指定目标（变体 id 或商品 id）",
	productenums.ErrPricingTargetNotFound:          "商品或变体不存在、没有可改价的变体（捆绑容器只有容器价，不能按规则改价）",
	productenums.ErrPricingFilterEmpty:             "筛选集至少要给一个筛选条件",
	productenums.ErrPricingNothingChanged:          "按该规则算下来没有任何价格变化",
	productenums.ErrPricingTargetTooMany:           "命中的商品数超过单批上限，请收紧筛选条件",
	productenums.ErrPricingAdjustmentNotFound:      "调价批次不存在",
	// 同上：完整性补齐后的中文兜底（i18n 未初始化或该 key 没有词条时用）。
	productenums.ErrAttrInUse:                    "属性组已被商品引用，不能删除",
	productenums.ErrAttrKeyRequired:              "属性组标识不能改为空",
	productenums.ErrAttrKeyTaken:                 "该属性组标识在本工程已被占用",
	productenums.ErrAttrNameRequired:             "属性组名称必填",
	productenums.ErrBrandInUse:                   "品牌已被商品引用，不能删除",
	productenums.ErrBrandNameRequired:            "品牌名称必填",
	productenums.ErrBrandNotFound:                "品牌不存在",
	productenums.ErrBrandProjectMismatch:         "品牌不属于该商品所在工程",
	productenums.ErrBrandSlugTaken:               "该品牌 URL 段在本工程已被占用",
	productenums.ErrBundleMaxOptionsInvalid:      "捆绑选项数量上限不合法（允许范围 1~20）",
	productenums.ErrBundleNotConfigured:          "该商品没有配置捆绑选项（不是捆绑商品）",
	productenums.ErrBundleOptionRequired:         "漏填了捆绑的必选项",
	productenums.ErrBundleOptionsExceeded:        "捆绑选项数超过上限",
	productenums.ErrBundleQtyAboveMax:            "某一项的数量超过该项最大数量",
	productenums.ErrBundleQtyAboveStock:          "某一项的数量超过该仓可用库存",
	productenums.ErrBundleQtyBelowMin:            "某一项的数量低于该项最小数量",
	productenums.ErrBundleQtyInvalid:             "数量不合法（负数 / 非整数 / 超过硬上限）",
	productenums.ErrBundleQtyRangeInvalid:        "某一项的数量区间自相矛盾（最小 > 默认、最大 < 最小等）",
	productenums.ErrBundleShapeInvalid:           "捆绑配置形状非法（既不是空值，也不是 JSON 对象）",
	productenums.ErrBundleStockUnavailable:       "读不到库存可用量（库存能力未装配），捆绑校验拒绝通过",
	productenums.ErrBundleTotalAboveMax:          "整单总件数超过最大总件数",
	productenums.ErrBundleTotalBelowMin:          "整单总件数低于最小总件数",
	productenums.ErrBundleTotalRangeInvalid:      "整单件数区间自相矛盾（最大 < 最小，或低于必选项最小量之和）",
	productenums.ErrBundleTotalUnreachable:       "整单下限高于所有选项能加到的上限（永远无法满足）",
	productenums.ErrBundleVariantDuplicated:      "同一个 SKU 在配置里或同一次选择里出现多次",
	productenums.ErrBundleVariantNotFound:        "选项引用的 SKU 不存在（可能已被删除）",
	productenums.ErrBundleVariantNotInConfig:     "选择里出现了配置之外的 SKU",
	productenums.ErrBundleVariantProjectMismatch: "选项引用的 SKU 不属于该商品所在工程",
	productenums.ErrBundleVariantRequired:        "捆绑选项缺少 SKU",
	productenums.ErrCategoryCycle:                "不能把分类挂到自身或自己的后代下",
	productenums.ErrCategoryHasChildren:          "分类仍有子级，不能删除",
	productenums.ErrCategoryInUse:                "分类已被商品引用，不能删除",
	productenums.ErrCategoryNameRequired:         "分类名称必填",
	productenums.ErrCategoryNotFound:             "分类不存在",
	productenums.ErrCategoryParentMismatch:       "父分类不属于该分类所在工程",
	productenums.ErrCategoryProjectMismatch:      "分类不属于该商品所在工程",
	productenums.ErrCategorySlugTaken:            "该分类 URL 段在本工程已被占用",
	productenums.ErrCollectionFilterInvalid:      "过滤维度不在集合源白名单内",
	productenums.ErrCollectionSourceInvalid:      "集合源标识不合法（不是本模块实现的源）",
	productenums.ErrInvalidField:                 "提交了不支持的字段（不在商品字段白名单内）",
	productenums.ErrInvalidType:                  "实体类型不合法",
	productenums.ErrTagKindInvalid:               "标签类型不是 manual / rule",
	productenums.ErrTagNameRequired:              "标签名称必填",
	productenums.ErrTagNotFound:                  "标签不存在",
	productenums.ErrTagNotManual:                 "自动标签的归属由重算维护，不能手工挂载",
	productenums.ErrTagProjectMismatch:           "标签不属于该商品所在工程",
	productenums.ErrTagRuleNotAllowed:            "手工标签不能带自动规则",
	productenums.ErrTagRuleParamsInvalid:         "规则参数不合法（键 / 类型 / 取值范围）",
	productenums.ErrTagRuleTypeInvalid:           "规则类型不是内置类型（不接受自由表达式）",
	productenums.ErrTagSlugTaken:                 "该标签 URL 段在本工程已被占用",
	productenums.ErrVariationAttributeInvalid:    "勾选的属性组未参与该商品的变体",
	productenums.ErrVariationCountLimit:          "变体组合数超过上限",
	productenums.ErrVariationDimensionLimit:      "参与变体的属性维度超过上限",
	productenums.ErrVariationNoDimension:         "没有可参与变体的属性值",
	productenums.ErrVariationValueInvalid:        "勾选的属性值不属于该属性组或已停用",
	productenums.ErrVariationSelectionEmpty:      "未勾选任何属性值：请至少勾选一个属性值，或改用「生成全部组合」",
	productenums.VariantSkipDuplicated:           "清单里重复的规格组合（只保留第一行）",
	productenums.VariantSkipHasStock:             "该变体仍有非零库存",
	productenums.VariantSkipReferenced:           "该变体被 BOM 清单引用",
	productenums.VariantSkipVariantMissing:       "清单引用的既有变体已不存在（可能已被并发删除）",
}

// productErrKey 业务错误 → 词条 key（非业务错误返回空串）。
//
// 不能用 errors.Is：商品域的业务错误是字符串常量，且服务端会用
// fmt.Errorf("%s：补充说明", key) 把 key 拼进整句话。两种形态都按「整串等于 key」
// 或「以 key：开头」识别，取 key 部分去查词条，补充说明原样保留。
func productErrKey(msg string) (key, tail string) {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return "", ""
	}
	for _, sentinel := range productErrSentinels {
		if msg == sentinel {
			return sentinel, ""
		}
		if strings.HasPrefix(msg, sentinel+"：") {
			return sentinel, strings.TrimSpace(strings.TrimPrefix(msg, sentinel+"："))
		}
	}
	return "", ""
}

// productErrScene 日志场景名（与 product 模块其它 logger.Scene("product") 一致）。
const productErrScene = "product"

// productErrControlledMessages 受控提示的前缀白名单。
//
// 它们**不是** enums key（因此进不了 productErrSentinels），但整句都由本仓库自己拼出：
// 不含表名 / SQLSTATE / 文件路径，且带着运营照着做的数字。目前只有一条 ——
// shell.BulkIDs 的上限拒绝「一次最多操作 N 项，当前 M 项，请分批进行」（internal/web/shell/bulk.go）。
//
// 为什么按**前缀**判而不是「调用点知道来源就直接透出」：来源受控这件事会随上游改变。
// 前缀命中不了（BulkIDs 将来改成上抛别的错误）时这里自动退回「记日志 + 归口文案」，
// 不会把不认识的原文顺出去。
var productErrControlledMessages = []string{
	fmt.Sprintf("一次最多操作 %d 项", shell.MaxBulkIDs),
}

// productErrControlled 受控提示 → 原样透出（保留可行动信息）；未命中返回空串。
func productErrControlled(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	for _, prefix := range productErrControlledMessages {
		if strings.HasPrefix(raw, prefix) {
			return raw
		}
	}
	return ""
}

// productInternalText 未命中任何白名单时的统一出口（错误文案三件套的第三件）：
// 原文只进日志（场景 + user_id + 原始错误），对外只给归口文案。
//
// 页面上出现 "pq: duplicate key value violates unique constraint" 既看不懂，
// 也把库表结构泄了出去 —— 响应（含 ?err= 回带与模板数据）不是可信边界。
func productInternalText(c *gin.Context, err error) string {
	if err != nil {
		logger.Scene(productErrScene).
			With("user_id", shell.CurrentUserID(c)).
			Error(err, "商品后台页操作失败（非业务错误，只对外给归口文案）")
	}
	return shell.TranslateFor(c)(shell.MsgInternalError, productErrInternalFallback)
}

// productErrText 业务错误 → 当前语言文案（非业务错误只给通用提示并留日志）。
//
// 与 block 模块的 blockErrText 同一形状：页面上的错误必须是能读的一句话，
// 而不是 ErrBundlePriceRequired 这样的裸 key，也不是 GORM / PG 的原始报错。
func productErrText(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	raw := strings.TrimSpace(err.Error())
	// 受控提示（shell.BulkIDs 的上限拒绝）优先：它本身就是可读中文，查词条只会把它吞掉。
	if msg := productErrControlled(raw); msg != "" {
		return msg
	}
	key, tail := productErrKey(raw)
	if key == "" {
		return productInternalText(c, err)
	}
	text := shell.TranslateFor(c)(key, productErrFallbacks[key])
	if tail != "" {
		text += "：" + tail
	}
	return text
}

// attributeIDsFromForm 收商品表单里的属性组引用。
//
// 抽屉是勾选列表（同名多值，浏览器逐项提交），同时兼容老式的「逗号分隔单值」形态
// （接口 / 脚本路径）—— 两种形态都归一到 splitIDs 解析（分隔符与去空白只有一份规则）。
func attributeIDsFromForm(c *gin.Context) []string {
	values := c.PostFormArray("attributeIds")
	if len(values) == 0 {
		return splitIDs(c.PostForm("attributeIds"))
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, raw := range values {
		for _, id := range splitIDs(raw) {
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// attributeOptions 新建抽屉的属性组勾选列表（本批：不再让人手打 UUID）。
//
// 与归属仓下拉同一手法：可选值由服务端给出，模板只做展示。名字后带上标识 key ——
// 商品详情页与属性页都按 key 指代属性组，只给名字会在同名属性组之间选错。
func (h *productPageHandle) attributeOptions(ctx context.Context, projectID string) (out []gin.H, err error) {
	out = []gin.H{}
	if strings.TrimSpace(projectID) == "" {
		return out, nil
	}
	list, lerr := h.products.ListAttributes(ctx, &productdto.ListAttributeReq{ProjectID: projectID, Size: 200})
	if lerr != nil {
		return nil, lerr
	}
	for _, a := range list {
		if a == nil {
			continue
		}
		label := a.Name
		if key := strings.TrimSpace(a.Key); key != "" {
			label = a.Name + "（" + key + "）"
		}
		out = append(out, gin.H{"ID": a.ID, "Label": label, "IsVariation": a.IsVariation})
	}
	return out, nil
}

// ProductsBulkPricing 批量「按规则改价」（POST /admin/products/bulk-pricing）。
//
// 入口为什么搬到商品列表：定价工具的作用对象本来就是「筛选出的商品集 / 标签 / SKU」，
// 而运营是在列表上圈出要改的那几个的；独立页 /admin/product-pricing 保留可用
// （它能对单个 SKU / 筛选集改价，也是唯一能试算的地方），本端点补上「按勾选批量应用」。
//
// 语义与其它批量操作一致（见 internal/module/CLAUDE.md §inbound/http）：
//
//	· id 一律经 shell.BulkIDs（去空白 / 去重 / 上限），超限**整批拒绝**并说明原因；
//	· 逐条走**单商品的应用路径**（scope=product），一条失败不中断整批；
//	· 结论按「改价 N 个变体 / 跳过 M 个商品」回带列表页，不做静默的部分成功。
//
// 规则解析与校验完全复用定价工具那一份（pricingRuleReqFromForm → readPricingForm /
// pricingParamsFromForm）：复制一份就会出现「抽屉填的 amount 被当成 multiplier」这类
// 只在一个入口发生的错。
func (h *productPageHandle) ProductsBulkPricing(c *gin.Context) {
	ctx := c.Request.Context()
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		// 受控提示（一次最多操作 N 项）保持可见，但同样经归口助手判定来源。
		redirectWhere(c, productListURL(projectID, listErrMark, productErrText(c, berr)))
		return
	}
	if len(ids) == 0 {
		redirectWhere(c, productListURL(projectID, listErrMark, bulkPricingNothingSelected))
		return
	}
	note := strings.TrimSpace(c.PostForm("note"))
	// 操作人取自会话，表单字段不作数（调价留痕的操作人不可伪造）。
	operator := shell.CurrentUserIDText(c)
	changedVariants, unchangedProducts, skipped := 0, 0, 0
	skipReason := ""
	for _, id := range ids {
		rule := pricingRuleReqFromForm(c, productenums.PricingScopeProduct, id)
		res, aerr := h.products.ApplyPricing(ctx, &productdto.PricingApplyReq{
			PricingRuleReq: *rule, Note: note, OperatorID: operator,
		})
		switch {
		case aerr == nil:
			if res != nil {
				changedVariants += res.ChangedCount
			}
		case strings.TrimSpace(aerr.Error()) == productenums.ErrPricingNothingChanged:
			// 这个商品本来就在目标价上：不是失败（service 不写空台账），单独计数。
			unchangedProducts++
		default:
			skipped++
			if skipReason == "" {
				skipReason = productErrText(c, aerr)
			}
		}
	}
	mark := listDoneMark
	if skipped > 0 || changedVariants == 0 {
		// 有跳过或一个变体都没改：走警告条（更显眼），用户下次会去看剩下的那些。
		mark = listErrMark
	}
	redirectWhere(c, productListURL(projectID, mark,
		bulkPricingResultMsg(changedVariants, unchangedProducts, skipped, skipReason)))
}

// bulkPricingResultMsg 批量改价的结论文案（四类结果各一句话）。
//
// 「一个都没改」与「跳过若干」必须能区分：两者都写成「没有变化」时，用户无法判断是
// 规则填错了、商品本来就在目标价上，还是这批商品根本没有变体。
func bulkPricingResultMsg(changedVariants, unchangedProducts, skipped int, reason string) string {
	if strings.TrimSpace(reason) == "" {
		reason = productErrFallbacks[productenums.ErrPricingTargetNotFound]
	}
	// 模板取自 product_err.go：那里同时按这些模板（与原因枚举组合）生成读侧候选文案，
	// 写侧改措辞时读侧跟着变，不会静默失配成归口文案。
	switch {
	case skipped == 0 && changedVariants == 0:
		return fmt.Sprintf(productPricingNoChange, unchangedProducts)
	case skipped == 0:
		return fmt.Sprintf(productPricingApplied, changedVariants, unchangedProducts)
	case changedVariants == 0:
		return fmt.Sprintf(productPricingAllSkip, skipped, reason)
	default:
		return fmt.Sprintf(productPricingPartial, changedVariants, skipped, reason)
	}
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
		c.Redirect(http.StatusFound, productListURL(projectID, listErrMark, productErrText(c, berr)))
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
		target += "&err=" + url.QueryEscape(fmt.Sprintf(productBulkPartial, deleted, skipped))
	case deleted > 0:
		target += "&done=" + url.QueryEscape(fmt.Sprintf(productBulkDone, deleted))
	}
	c.Redirect(http.StatusFound, target)
}
