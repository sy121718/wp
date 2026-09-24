// product_bundle_handle.go — 后台捆绑配置页（issue #20）。
//
// 与商品页同族（挂在 productPageHandle 上）：选工程 → 选商品 → 配选项规则 → 保存。
// 表单是**原生并行数组**（variantId / required / defaultQty / minQty / maxQty 各自多值），
// 零自定义 JS 就能表达「一行的多个字段」—— 不用 JSON 字符串塞进隐藏域，
// 也就没有「前端拼 JSON 出错但服务端照单全收」的缝隙。
//
// 页面底部内嵌前台配置器片段（hx-get /_fragments/bundleConfigurator）：
// 运营在这里看到的就是访客看到的那一份渲染。
package producthttp

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/internal/web/shell"
)

// ProductBundlePage 捆绑配置页。
func (h *productPageHandle) ProductBundlePage(c *gin.Context) {
	h.renderBundlePage(c, nil, "", false)
}

func (h *productPageHandle) renderBundlePage(c *gin.Context, submitted *productdto.BundleConfig, failure string, fragment bool) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		shell.PageError(c, "product_bundle", err)
		return
	}
	selectedProject := bundleQueryParam(c, "project", "projectId")
	if selectedProject == "" && len(projects) > 0 {
		selectedProject = projects[0].ID
	}
	var list []*productdto.ProductResp
	if selectedProject != "" {
		list, err = h.products.List(ctx, &productdto.ListReq{ProjectID: selectedProject, Size: 100})
		if err != nil {
			shell.PageError(c, "product_bundle", err)
			return
		}
	}
	selectedProduct := bundleQueryParam(c, "product", "productId")
	var detail *productdto.BundleConfigResp
	if selectedProduct != "" {
		detail, err = h.products.GetBundleConfig(ctx, &productdto.GetBundleConfigReq{ProductID: selectedProduct})
		if err != nil {
			// 配置读不出来时仍渲染页面骨架。
			detail = nil
		}
	}
	if submitted != nil && detail != nil {
		detail.Config = *submitted
	}
	skus, serr := h.products.ListBundleSKUs(ctx, &productdto.ListBundleSKUReq{ProjectID: selectedProject})
	if serr != nil {
		shell.PageError(c, "product_bundle", serr)
		return
	}
	// —— 成员来源面板的数据（docs/14 §1.2 的三种来源，批次 C）——
	//
	// 来源 / 来源商品 / 来源仓都是**页级上下文**（与工程、商品同一层）：用 GET 重新渲染，
	// 而不是让前端拼参数 —— 属性值勾选清单与仓库 SKU 候选都由服务端给出，
	// 前端不需要复刻任何口径（它只负责把选中的行追加进成员清单）。
	tr := shell.TranslateFor(c)
	source := strings.TrimSpace(c.Query("source"))
	if source == "" {
		source = productenums.BundleSourceProduct
	}
	sourceProductID := strings.TrimSpace(c.Query("sourceProduct"))
	sourceWarehouseID := strings.TrimSpace(c.Query("sourceWarehouse"))
	sourceAttrs := h.bundleSourceAttributes(ctx, sourceProductID)
	warehouses, werr := h.warehouseOptions(ctx, selectedProject)
	if werr != nil {
		shell.PageError(c, "product_bundle", werr)
		return
	}
	// 仓库 SKU 候选**按仓分组**逐仓取（复用「从仓库选」的既有能力，见 product_warehouse_sku_view.go）：
	// 一次取全库再在内存分组会把整张库存表拉进内存，而这里只需要「最近常用的一屏」。
	warehouseGroups, gerr := h.warehouseSKUOptions(ctx, selectedProject, warehouses)
	if gerr != nil {
		shell.PageError(c, "product_bundle", gerr)
		return
	}
	template := "admin/product/product_bundle.html"
	if fragment {
		template = "admin/product/product_bundle_form.html"
	}
	if failure == "" {
		failure = productPageErr(c)
	}
	rows := bundleConfigRows(tr, detail, skus)
	if submitted != nil && detail != nil {
		rows = bundleSubmittedRows(tr, detail, skus, c)
	}
	c.HTML(http.StatusOK, template, shell.Prepare(c, gin.H{
		"title":           "捆绑配置",
		"menu":            "products",
		"Projects":        projects,
		"SelectedProject": selectedProject,
		"Products":        list,
		"SelectedProduct": selectedProduct,
		"Detail":          detail,
		"Rows":            rows,
		"CandidateSKUs":   bundleCandidateOptions(skus),
		"SkuCount":        len(skus),
		"MaxOptions":      productdto.BundleMaxOptionsLimit,
		"Err":             failure,
		// 成员来源面板（批次 C）：三种来源 + 各自的候选数据。
		"Sources":           bundleSourceOptions(tr, source),
		"Source":            source,
		"SourceProducts":    bundleSourceProductOptions(list, sourceProductID),
		"SourceAttributes":  sourceAttrs,
		"SourceWarehouses":  bundleSourceWarehouseGroups(warehouseGroups, sourceWarehouseID),
		"SourceWarehouseID": sourceWarehouseID,
		"ResolveURL":        "/admin/products/bundle/members/resolve",
	}))
}

// ProductBundleSave 保存捆绑配置；失败在当前请求回填，HX 成功整页跳转。
func (h *productPageHandle) ProductBundleSave(c *gin.Context) {
	ctx := c.Request.Context()
	productID := strings.TrimSpace(c.PostForm("productId"))
	if productID == "" {
		c.Redirect(http.StatusFound, "/admin/products/bundle?err="+url.QueryEscape(productBundleNoProductText))
		return
	}
	cfg := productdto.NewEmptyBundleConfig()
	if v, aerr := strconv.Atoi(strings.TrimSpace(c.PostForm("maxOptions"))); aerr == nil && v > 0 {
		cfg.MaxOptions = v
	}
	cfg.MinTotalQty = atoiOrZero(c.PostForm("minTotalQty"))
	cfg.MaxTotalQty = atoiOrZero(c.PostForm("maxTotalQty"))
	ids := c.PostFormArray("variantId")
	required := c.PostFormArray("required")
	defaults := c.PostFormArray("defaultQty")
	mins := c.PostFormArray("minQty")
	maxes := c.PostFormArray("maxQty")
	// 来源快照是**并行数组**（与上面五个同一种形态）：成员表的每一行都提交这四个字段，
	// 因此索引与 variantId 一一对应。来源只作溯源与展示，服务端仍按白名单归一
	//（未知来源在 service 里被拒，非仓库来源的仓库字段被清空）。
	sourceKinds := c.PostFormArray("sourceKind")
	warehouseIDs := c.PostFormArray("memberWarehouseId")
	warehouseSKUs := c.PostFormArray("memberWarehouseSku")
	externalSKUs := c.PostFormArray("memberExternalSku")
	for i, vid := range ids {
		vid = strings.TrimSpace(vid)
		if vid == "" {
			continue
		}
		cfg.Options = append(cfg.Options, productdto.BundleOption{
			VariantID:  vid,
			Required:   atoiAt(required, i) == 1,
			DefaultQty: atoiAt(defaults, i),
			MinQty:     atoiAt(mins, i),
			MaxQty:     atoiAt(maxes, i),

			SourceKind:   strAt(sourceKinds, i),
			WarehouseID:  strAt(warehouseIDs, i),
			WarehouseSKU: strAt(warehouseSKUs, i),
			ExternalSKU:  strAt(externalSKUs, i),
		})
	}
	if _, err := h.products.SetBundleConfig(ctx, &productdto.SetBundleConfigReq{
		ProductID: productID,
		Config:    cfg,
		// 操作人只从会话取（主数据变更记录要记「谁改的」）。
		OperatorID: builtin.GetUsername(c),
	}); err != nil {
		q := url.Values{"product": {productID}}
		if project := strings.TrimSpace(c.PostForm("projectId")); project != "" {
			q.Set("project", project)
		}
		c.Request.URL.RawQuery = q.Encode()
		h.renderBundlePage(c, &cfg, productErrText(c, err), c.GetHeader("HX-Request") == "true")
		return
	}
	destination := "/admin/products/bundle?product=" + url.QueryEscape(productID)
	if c.GetHeader("HX-Request") == "true" {
		c.Header("HX-Redirect", destination)
		c.Status(http.StatusOK)
		return
	}
	c.Redirect(http.StatusFound, destination)
}

// bundleQueryParam 取本页的上下文参数（工程 / 商品），按候选名依次认第一个非空值。
//
// 参数名有两套且都合法：本页自己的两个选择器用 project / product；商品详情页的
// 「编辑捆绑构成」入口用 projectId / productId（与商品页其余表单同一套字段名，
// 直接复用商品 id 不会串）。两个都认，入口从哪来都能选中同一个商品 ——
// 少认一个的表现是「点入口进来是空骨架」，而页面上看不出哪里不对。
func bundleQueryParam(c *gin.Context, names ...string) string {
	for _, name := range names {
		if v := strings.TrimSpace(c.Query(name)); v != "" {
			return v
		}
	}
	return ""
}

// bundleConfigRows 配置表单的行（已配置的项 + 唯一空白候选行）。
//
// 空白行保留原生表单无 JS 单次添加；已存成员仅展示 SKU 与隐藏身份。
// **每一行都提交同一组字段**（variantId / required / defaultQty / minQty / maxQty /
// sourceKind / memberWarehouseId / memberWarehouseSku / memberExternalSku）：
// 并行数组靠 DOM 顺序对齐，少一个字段就会让后面所有行的索引错位。
func bundleConfigRows(tr func(key, fallback string) string, detail *productdto.BundleConfigResp, skus []*productdto.BundleSKUResp) []gin.H {
	rows := make([]gin.H, 0, productdto.BundleMaxOptionsLimit)
	// 已配置项的可用量来自配置详情（service 已按真源批量取好），
	// 新加的空白行没有可用量可言（还没选 SKU）。
	skuByID := make(map[string]string, len(skus))
	for _, s := range skus {
		if s != nil {
			skuByID[s.VariantID] = s.ProductName + " · " + s.SKUCode
		}
	}
	if detail != nil {
		for _, o := range detail.Options {
			if o == nil {
				continue
			}
			label := skuByID[o.VariantID]
			if label == "" {
				label = strings.TrimSpace(o.ProductName + " · " + o.SKUCode)
				if o.SKUCode == "" {
					label = o.VariantID
				}
			}
			rows = append(rows, bundleRow(o.BundleOption, o.Available,
				bundleSourceLabel(tr, o.BundleOption), label))
		}
	}
	if len(rows) < productdto.BundleMaxOptionsLimit {
		rows = append(rows, bundleRow(productdto.BundleOption{Required: true, DefaultQty: 1, MinQty: 1}, 0, "", ""))
	}
	return rows
}

// bundleRow 构造一行数据；候选 SKU 列表仅供唯一空白行使用。
func bundleRow(o productdto.BundleOption, available int, sourceLabel, skuLabel string) gin.H {
	return gin.H{
		"VariantID":  o.VariantID,
		"SKULabel":   skuLabel,
		"Required":   o.Required,
		"DefaultQty": o.DefaultQty,
		"MinQty":     o.MinQty,
		"MaxQty":     o.MaxQty,
		"Available":  available,
		// 来源快照（仅作溯源与展示，不是身份）：四个字段原样回填进隐藏域，
		// 代表当前这一行的来源；标签已按当前语言算好。
		"SourceKind":   o.SourceKind,
		"WarehouseID":  o.WarehouseID,
		"WarehouseSKU": o.WarehouseSKU,
		"ExternalSKU":  o.ExternalSKU,
		"SourceLabel":  sourceLabel,
	}
}

// bundleSubmittedRows rebuilds rows from the posted arrays, not persisted configuration.
func bundleSubmittedRows(tr func(key, fallback string) string, detail *productdto.BundleConfigResp, skus []*productdto.BundleSKUResp, c *gin.Context) []gin.H {
	labels := make(map[string]string, len(skus))
	for _, s := range skus {
		if s != nil {
			labels[s.VariantID] = s.ProductName + " · " + s.SKUCode
		}
	}
	for _, o := range detail.Options {
		if o != nil && labels[o.VariantID] == "" && o.SKUCode != "" {
			labels[o.VariantID] = strings.TrimSpace(o.ProductName + " · " + o.SKUCode)
		}
	}
	ids := c.PostFormArray("variantId")
	required, defaults := c.PostFormArray("required"), c.PostFormArray("defaultQty")
	mins, maxes := c.PostFormArray("minQty"), c.PostFormArray("maxQty")
	kinds, warehouses := c.PostFormArray("sourceKind"), c.PostFormArray("memberWarehouseId")
	warehouseSKUs, externalSKUs := c.PostFormArray("memberWarehouseSku"), c.PostFormArray("memberExternalSku")
	rows := make([]gin.H, 0, len(ids)+1)
	hasBlank := false
	for i, rawID := range ids {
		id := strings.TrimSpace(rawID)
		if id == "" {
			hasBlank = true
		}
		option := productdto.BundleOption{
			VariantID: id, Required: atoiAt(required, i) == 1,
			DefaultQty: atoiAt(defaults, i), MinQty: atoiAt(mins, i), MaxQty: atoiAt(maxes, i),
			SourceKind: strAt(kinds, i), WarehouseID: strAt(warehouses, i),
			WarehouseSKU: strAt(warehouseSKUs, i), ExternalSKU: strAt(externalSKUs, i),
		}
		label := labels[id]
		if label == "" && id != "" {
			label = id
		}
		rows = append(rows, bundleRow(option, 0, bundleSourceLabel(tr, option), label))
	}
	if len(rows) < productdto.BundleMaxOptionsLimit && !hasBlank {
		rows = append(rows, bundleRow(productdto.BundleOption{Required: true, DefaultQty: 1, MinQty: 1}, 0, "", ""))
	}
	return rows
}

func bundleCandidateOptions(skus []*productdto.BundleSKUResp) []gin.H {
	options := make([]gin.H, 0, len(skus))
	for _, s := range skus {
		if s != nil {
			options = append(options, gin.H{"ID": s.VariantID, "Label": s.ProductName + " · " + s.SKUCode})
		}
	}
	return options
}

// —— 成员来源面板（docs/14 §1.2 的三种来源，批次 C）——

// bundleSourceOptions 来源下拉的三项（值即 enums 常量 —— 服务端按它选解析分支）。
func bundleSourceOptions(tr func(key, fallback string) string, selected string) []gin.H {
	items := []struct {
		Value    string
		Fallback string
	}{
		{productenums.BundleSourceProduct, "从商品导入（该商品的启用变体）"},
		{productenums.BundleSourceWarehouse, "从仓库选（按仓挑选仓库 SKU）"},
		{productenums.BundleSourceAttributes, "自选属性值组合（服务端重算笛卡尔积）"},
	}
	out := make([]gin.H, 0, len(items))
	for _, it := range items {
		out = append(out, gin.H{
			"Value":    it.Value,
			"Label":    tr(bundleSourceKey(it.Value), it.Fallback),
			"Selected": it.Value == selected,
		})
	}
	return out
}

// bundleSourceKey 来源值 → 词条 key（与模板/词条表同一份对应关系）。
func bundleSourceKey(value string) string {
	switch value {
	case productenums.BundleSourceWarehouse:
		return "admin.product_bundle.source.warehouse"
	case productenums.BundleSourceAttributes:
		return "admin.product_bundle.source.attributes"
	default:
		return "admin.product_bundle.source.product"
	}
}

// bundleSourceProductOptions 来源商品下拉项（工程内全部商品；自引用由服务端拒绝）。
func bundleSourceProductOptions(products []*productdto.ProductResp, selected string) []gin.H {
	out := make([]gin.H, 0, len(products)+1)
	out = append(out, gin.H{"ID": "", "Label": "— 不选 —", "Selected": selected == ""})
	for _, p := range products {
		if p == nil {
			continue
		}
		out = append(out, gin.H{"ID": p.ID, "Label": p.Name, "Selected": p.ID == selected})
	}
	return out
}

// bundleSourceWarehouseGroups 来源仓下拉 + 各仓的仓库 SKU 勾选清单。
//
// 复用「从仓库选」的既有投影（warehouseSKUOptions）：每个仓一屏候选，
// 选中即代表「这条货在这个仓」—— 服务端解析时仍会回该仓复核（前端只是线索）。
func bundleSourceWarehouseGroups(groups []gin.H, selectedWarehouseID string) []gin.H {
	out := make([]gin.H, 0, len(groups))
	for _, g := range groups {
		id, _ := g["WarehouseID"].(string)
		out = append(out, gin.H{
			"WarehouseID": id,
			"Label":       g["Label"],
			"IsDefault":   g["IsDefault"],
			"Selected":    id != "" && id == selectedWarehouseID,
			"Items":       g["Items"],
		})
	}
	return out
}

// bundleSourceAttributes 来源商品的「参与变体」属性组（自选属性组合来源的勾选清单）。
//
// 只列参与变体的属性组：不参与变体的组构不出组合（与变体生成抽屉同一口径）。
// 未选来源商品 / 读不出来 / 该商品没有参与变体的组，都返回空清单 ——
// 页面据它渲染一句可行动的提示（而不是让面板变成一块无法操作的空白）。
func (h *productPageHandle) bundleSourceAttributes(ctx context.Context, productID string) (attrs []*productdto.AttributeResp) {
	attrs = []*productdto.AttributeResp{}
	id := strings.TrimSpace(productID)
	if id == "" {
		return attrs
	}
	detail, err := h.products.Get(ctx, &productdto.GetReq{ID: id})
	if err != nil || detail == nil {
		return attrs
	}
	return variationAttributes(detail.Attributes)
}

// productBundleNoProductText 捆绑页的参数级提示（未选商品就提交保存），同样进 ?err=。
const productBundleNoProductText = "请先选择商品"

// strAt 取并行数组的第 i 个字符串（缺失一律空串）。
//
// 与 atoiAt 同一手法：这里不承担校验 —— 来源字段的白名单与清理在 service
// （未知来源会被明确拒绝，不是在这里静默丢弃）。
func strAt(values []string, i int) string {
	if i < 0 || i >= len(values) {
		return ""
	}
	return strings.TrimSpace(values[i])
}

// ProductsBundleMembersResolve 解析捆绑成员的候选行（POST，**不落库**）。
//
// 与变体清单的预览端点（/admin/products/variant/preview）同一形态：服务端把候选行与逐条
// 跳过原因算好回 JSON，前端只负责把行**追加**进成员清单；「保存配置」才是落库动作。
//
// 为什么要 JSON 而不是服务端渲染的片段：成员清单是**未保存的前端状态**（与变体清单同理），
// 服务端渲染的片段落不进那个状态里；这里回的行结构与服务端渲染的行结构逐字一致。
//
// 失败回 400 + 可读文案（走 productErrText，不把 enums 裸 key 或 PG 报错铺给前端）。
func (h *productPageHandle) ProductsBundleMembersResolve(c *gin.Context) {
	ctx := c.Request.Context()
	req := &productdto.ResolveBundleMembersReq{
		ProductID:       strings.TrimSpace(c.PostForm("productId")),
		ProjectID:       strings.TrimSpace(c.PostForm("projectId")),
		Source:          strings.TrimSpace(c.PostForm("source")),
		SourceProductID: strings.TrimSpace(c.PostForm("sourceProductId")),
		WarehouseID:     strings.TrimSpace(c.PostForm("sourceWarehouse")),
		WarehouseSKUs:   c.PostFormArray("warehouseSku"),
		// 属性组合的勾选用生成组合抽屉的同一套字段名（attr:<属性组 id>=值 id）：
		// 收拢规则只有一份，两个入口不会一个认前缀、另一个漏读。
		Selections: variantSelectionFromForm(c),
		// 前端清单里已有的成员：服务端据此去重（同一变体只出现一次）。
		ExistingVariantIDs: c.PostFormArray("existingVariantId"),
	}
	res, err := h.products.ResolveBundleMembers(ctx, req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "message": productErrText(c, err)})
		return
	}
	tr := shell.TranslateFor(c)
	// 规格文本要用来源商品的属性组：「组合在商品侧没有对应变体」必须指得出是哪一组。
	attrs := []*productdto.AttributeResp{}
	if req.SourceProductID != "" {
		if detail, derr := h.products.Get(ctx, &productdto.GetReq{ID: req.SourceProductID}); derr == nil && detail != nil {
			attrs = detail.Attributes
		}
	}
	members := make([]gin.H, 0, len(res.Members))
	for _, m := range res.Members {
		if m == nil {
			continue
		}
		// 键名一律小驼峰：gin.H 是 map，JSON 键就是这里的字面量 ——
		// 页面 JS 按 variantId / sourceKind / warehouseSku 取值（与其余 JSON 接口同一口径），
		// 写成 PascalCase 会让前端拿到 undefined 却看不出任何错。
		members = append(members, gin.H{
			"variantId":   m.VariantID,
			"skuCode":     m.SKUCode,
			"productId":   m.ProductID,
			"productName": m.ProductName,
			"spec":        specLabel(m.OptionValues, attrs),
			"sourceKind":  m.Source.Kind,
			"sourceLabel": bundleSourceLabel(tr, productdto.BundleOption{
				SourceKind: m.Source.Kind, WarehouseID: m.Source.WarehouseID,
				WarehouseSKU: m.Source.WarehouseSKU, ExternalSKU: m.Source.ExternalSKU,
			}),
			"warehouseId":  m.Source.WarehouseID,
			"warehouseSku": m.Source.WarehouseSKU,
			"externalSku":  m.Source.ExternalSKU,
		})
	}
	skips := make([]gin.H, 0, len(res.Skipped))
	for _, s := range res.Skipped {
		// 跳过的行要能被认出来：「从规格组合来的」用可读规格文本，「从仓库来的」用仓库 SKU 文本。
		label := strings.TrimSpace(s.SKUCode)
		if label == "" {
			label = strings.TrimSpace(s.WarehouseSKU)
		}
		if label == "" {
			label = strings.TrimSpace(s.VariantID)
		}
		if spec := specLabel(s.OptionValues, attrs); spec != "" && spec != "—" {
			if label == "" {
				label = spec
			} else {
				label = label + " · " + spec
			}
		}
		skips = append(skips, gin.H{
			"reason": s.Reason,
			"text":   tr(s.Reason, bundleMemberSkipFallbacks[s.Reason]),
			"label":  label,
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"ok": true, "added": len(members), "skipped": len(skips),
		"members": members, "skips": skips,
	})
}

// bundleMemberSkipFallbacks 成员来源解析被跳过时的中文兜底（key 见 productenums.BundleMember*）。
//
// 与 variantSkipFallbacks 同一形态：词条缺失时页面上仍是可读的一句话，
// 而不是裸 key。
var bundleMemberSkipFallbacks = map[string]string{
	productenums.BundleMemberNotOnProduct:        "该属性值组合在商品侧没有对应变体，未加入（请先到该商品上生成这个规格的变体）",
	productenums.BundleMemberSkippedInList:       "该 SKU 已在成员清单里，未重复加入",
	productenums.BundleMemberWarehouseSKUMissing: "该仓库里没有这条仓库 SKU，未加入（请确认仓库选对了，或先在该仓建好这条货）",
	productenums.BundleMemberVariantDisabled:     "该变体已停用，未加入（停用的 SKU 挂进套餐会变成前台选不了又躲不开的必选项）",
	productenums.BundleMemberOptionsExceeded:     "已达该捆绑配置的选项数量上限，未加入（可先调大上限再解析）",
	productenums.ErrBundleSelfReference:          "该 SKU 属于这个捆绑容器自己，未加入（不能自引用）",
}

// atoiAt 取并行数组的第 i 个并转整数（缺失 / 非法一律 0）。
//
// 不用「解析失败即报错」：这里的 0 会进入 service 的配置校验，
// 该拒的（负数、必选数量为 0 等）由那边统一给出可读原因，前端只负责搬运。
func atoiAt(values []string, i int) int {
	if i < 0 || i >= len(values) {
		return 0
	}
	return atoiOrZero(values[i])
}

// atoiOrZero 解析整数，非法一律 0。
func atoiOrZero(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}
