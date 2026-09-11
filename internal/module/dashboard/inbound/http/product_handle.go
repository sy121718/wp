// product_handle.go — 后台商品管理页（issue #5 / T3a）。
//
// 独立于 dashboard 的通用 Handle：只依赖 product 契约与 project 契约，
// 避免把商品依赖掺进 dashboard 的通用装配。
//
// 交互遵循后台规范：GET 渲染完整页，POST 处理完 302 回列表（原生表单 + csrf_token 隐藏域）。
package dashboardhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	inventorycontract "go_wp/internal/module/inventory/contract"
	inventorydto "go_wp/internal/module/inventory/dto"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	projectcontract "go_wp/internal/module/project/contract"
)

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
		c.String(http.StatusInternalServerError, err.Error())
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
		c.String(http.StatusInternalServerError, ferr.Error())
		return
	}
	brands, berr := h.listBrands(ctx, selected)
	if berr != nil {
		c.String(http.StatusInternalServerError, berr.Error())
		return
	}
	// 标签一次取好（issue #11）：每个商品行要渲染「挂哪些手工标签 / 命中了哪些自动标签」，
	// 放在循环里取会变成 N 次查询。
	tags, terr := h.listTags(ctx, selected)
	if terr != nil {
		c.String(http.StatusInternalServerError, terr.Error())
		return
	}
	if selected != "" {
		list, lerr := h.products.List(ctx, &productdto.ListReq{ProjectID: selected, Size: 100})
		if lerr != nil {
			c.String(http.StatusInternalServerError, lerr.Error())
			return
		}
		for _, p := range list {
			// 列表项不含变体明细，逐个取详情（上限 100，后台页可接受）。
			detail, derr := h.products.Get(ctx, &productdto.GetReq{ID: p.ID})
			if derr != nil {
				continue
			}
			rows = append(rows, gin.H{
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
				"CategoryIDs":         detail.CategoryIDs,
				"CategoryChecks":      checkedCategoryOptions(flat, detail.CategoryIDs),
				"PrimaryOptions":      primaryCategoryOptions(flat, detail.PrimaryCategoryID),
				"BrandOptions":        brandPickOptions(brands, detail.BrandID),
				"PrimaryCategoryName": categoryNameByID(flat, detail.PrimaryCategoryID),
				"BrandName":           brandNameByID(brands, detail.BrandID),
				// 标签（issue #11）：手工标签勾选挂载（勾选态服务端算好）；自动标签只读展示 ——
				// 归属由规则重算维护，手工改会被下一次重算覆盖，故不提供勾选框。
				"TagIDs":    detail.TagIDs,
				"TagChecks": checkedTagOptions(tags, detail.TagIDs),
				"AutoTags":  attachedAutoTags(tags, detail.TagIDs),
			})
		}
	}
	// 归属仓下拉（issue #15）：变体的归属仓在这里选（不选 = 默认仓）。
	warehouseOptions, werr := h.warehouseOptions(ctx, selected)
	if werr != nil {
		c.String(http.StatusInternalServerError, werr.Error())
		return
	}
	// withCSRF：注入 csrf_token（POST 表单隐藏域）+ 导航树 + 权限码 + 多语言，
	// 与其它后台页面同一渲染入口（缺 token 时表单提交会被 CSRF 中间件挡下）。
	c.HTML(http.StatusOK, "admin/products.html", withCSRF(c, gin.H{
		"title":            "商品",
		"menu":             "products",
		"Projects":         projects,
		"SelectedProject":  selected,
		"WarehouseOptions": warehouseOptions,
		"Products":         rows,
		// 上一步的错误（上限拒绝 / 参数错误）经查询串回显 —— 同属性页的做法。
		"Err": strings.TrimSpace(c.Query("err")),
	}))
}

// variantRows 后台变体表的数据行：规格列把 option_values 翻成可读文本。
func variantRows(detail *productdto.ProductResp) []gin.H {
	rows := make([]gin.H, 0, len(detail.Variants))
	for _, v := range detail.Variants {
		rows = append(rows, gin.H{
			"ID": v.ID, "SKUCode": v.SKUCode, "Spec": specLabel(v.OptionValues, detail.Attributes),
			"Price":        formatAmount(v.Price),
			"ComparePrice": formatNullableAmount(v.ComparePrice),
			"CostPrice":    formatNullableAmount(v.CostPrice),
			"Enabled":      v.Enabled, "StockTotal": v.StockTotal,
		})
	}
	return rows
}

// variationAttributes 参与变体的属性组（组合生成面板只勾选这些组）。
func variationAttributes(attrs []*productdto.AttributeResp) []*productdto.AttributeResp {
	out := make([]*productdto.AttributeResp, 0, len(attrs))
	for _, a := range attrs {
		if a != nil && a.IsVariation {
			out = append(out, a)
		}
	}
	return out
}

// specLabel 规格组合的可读文本：按属性组定义顺序把「组名 值名」拼起来。
//
// 组已被删除或 key 改过的历史组合用原始 key→值兜底显示，不让后台丢信息。
func specLabel(raw json.RawMessage, attrs []*productdto.AttributeResp) string {
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil || len(m) == 0 {
		return "—"
	}
	used := map[string]bool{}
	parts := make([]string, 0, len(m))
	for _, a := range attrs {
		if a == nil {
			continue
		}
		value, ok := m[a.Key]
		if !ok {
			continue
		}
		used[a.Key] = true
		label := value
		for _, v := range a.Values {
			if v.Key == value {
				label = v.Label
				break
			}
		}
		parts = append(parts, a.Name+" "+label)
	}
	rest := make([]string, 0, len(m))
	for k, v := range m {
		if !used[k] {
			rest = append(rest, k+" "+v)
		}
	}
	sort.Strings(rest)
	parts = append(parts, rest...)
	return strings.Join(parts, " · ")
}

// formatAmount 数值 → 后台展示文本（整数不带小数尾巴）。
func formatAmount(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// formatNullableAmount 可空数值 → 展示文本（空显示为 —）。
func formatNullableAmount(v *float64) string {
	if v == nil {
		return "—"
	}
	return formatAmount(*v)
}

// ProductsVariantGenerate 按勾选的属性值批量生成变体组合（issue #8）。
//
// mode=all：不勾选任何值，按商品全部参与变体的属性组 × 全部启用值生成
// （service 的无表单路径，导入 / 接口走同一条）。
// 其它情况按勾选生成；一个都没勾选时直接退回并提示 —— 不静默退化成「全部生成」，
// 那是最容易一次误造出上百个变体的路径。
func (h *productPageHandle) ProductsVariantGenerate(c *gin.Context) {
	projectID := c.PostForm("projectId")
	req := &productdto.GenerateVariantsReq{
		ProductID:   c.PostForm("productId"),
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
			c.Redirect(http.StatusFound, "/admin/products?project="+projectID+"&err="+productenums.ErrVariationSelectionEmpty)
			return
		}
	}
	if _, err := h.products.GenerateVariants(c.Request.Context(), req); err != nil {
		c.Redirect(http.StatusFound, "/admin/products?project="+projectID+"&err="+err.Error())
		return
	}
	c.Redirect(http.StatusFound, "/admin/products?project="+projectID)
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
		c.Redirect(http.StatusFound, "/admin/products?project="+projectID+"&err="+err.Error())
		return
	}
	c.Redirect(http.StatusFound, "/admin/products?project="+projectID)
}

// ProductsVariantDelete 删除变体。
func (h *productPageHandle) ProductsVariantDelete(c *gin.Context) {
	projectID := c.PostForm("projectId")
	if err := h.products.DeleteVariant(c.Request.Context(), &productdto.DeleteVariantReq{ID: c.PostForm("id")}); err != nil {
		c.Redirect(http.StatusFound, "/admin/products?project="+projectID+"&err="+err.Error())
		return
	}
	c.Redirect(http.StatusFound, "/admin/products?project="+projectID)
}

// ProductsAttributesSet 整体替换某商品引用的属性组（issue #7）。
//
// 引用的组必须是同一工程内真实存在的组（service 校验）；提交空数组即解绑全部。
func (h *productPageHandle) ProductsAttributesSet(c *gin.Context) {
	projectID := c.PostForm("projectId")
	req := &productdto.UpdateReq{
		ID:           c.PostForm("id"),
		AttributeIDs: splitIDs(c.PostForm("attributeIds")),
	}
	if _, err := h.products.Update(c.Request.Context(), req); err != nil {
		c.Redirect(http.StatusFound, "/admin/products?project="+projectID+"&err="+err.Error())
		return
	}
	c.Redirect(http.StatusFound, "/admin/products?project="+projectID)
}

// warehouseOptions 某工程的仓库下拉项（issue #15；默认仓在最前并标注）。
//
// 未注入仓库契约时返回空列表：模板此时不渲染下拉，变体创建按「不指定仓库」处理。
func (h *productPageHandle) warehouseOptions(ctx context.Context, projectID string) (out []gin.H, err error) {
	out = []gin.H{}
	if h.inventories == nil || projectID == "" {
		return out, nil
	}
	rows, lerr := h.inventories.ListWarehouses(ctx, &inventorydto.ListWarehouseReq{ProjectID: projectID})
	if lerr != nil {
		return nil, lerr
	}
	for _, w := range rows {
		label := w.Name + "（" + w.Code + "）"
		if w.IsDefault {
			label += " · 默认仓"
		}
		out = append(out, gin.H{"ID": w.ID, "Label": label, "IsDefault": w.IsDefault})
	}
	return out, nil
}

// ProductsDelete 删除商品（连带变体）。
func (h *productPageHandle) ProductsDelete(c *gin.Context) {
	projectID := c.PostForm("projectId")
	if err := h.products.Delete(c.Request.Context(), &productdto.DeleteReq{ID: c.PostForm("id")}); err != nil {
		c.Redirect(http.StatusFound, "/admin/products?project="+projectID+"&err="+err.Error())
		return
	}
	c.Redirect(http.StatusFound, "/admin/products?project="+projectID)
}
