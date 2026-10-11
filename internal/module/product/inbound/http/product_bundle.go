package producthttp

import (
	"context"
	"errors"
	"go_wp/internal/adminui"
	"go_wp/internal/middleware/builtin"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	"go_wp/internal/shell"
	"go_wp/pkg/response"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// product_bundle.go — 捆绑商品（容器 + 可选组）：配置页、成员解析接口与详情页的捆绑面板。
//
// 可用量来自库存真源端口（warehouseOptions / warehouseSKUOptions）：捆绑的「可卖几份」
// 由各成员的最小可用量决定，不在商品表里存。

// ProductBundlePage 捆绑配置页。
// 与商品页同族（挂在 productPageHandle 上）：选工程 → 选商品 → 配选项规则 → 保存。
// 表单是**原生并行数组**（variantId / required / defaultQty / minQty / maxQty 各自多值），
// 零自定义 JS 就能表达「一行的多个字段」—— 不用 JSON 字符串塞进隐藏域，
// 也就没有「前端拼 JSON 出错但服务端照单全收」的缝隙。
//
// 页面底部内嵌前台配置器片段（hx-get /_fragments/bundleConfigurator）：
// 运营在这里看到的就是访客看到的那一份渲染。

// 为什么单独成文件：这一段是「仓库侧只回答这条货在这个仓叫什么」在商品页的唯一落点 ——
// 候选按仓分组、每仓有上限、未接库存契约时整块降级为空。与商品页其余部分（列表 / 详情 /
// 抽屉业务字段）没有耦合，放在一起只会让两边互相牵制。
//
// 前端只是便捷入口：ProductsCreate 会带着仓库 id 让 service 在**该仓**重新复核一遍。
//
// 接口面（原先的 product_handle.go 捆绑段）在前，后台页面在后。

func (h *productPageHandle) ProductBundlePage(c *gin.Context) {
	h.renderBundlePage(c, nil, "", false)
}

func (h *productPageHandle) renderBundlePage(c *gin.Context, submitted *productdto.BundleConfig, failure string, fragment bool) {
	ctx := c.Request.Context()
	// 取数失败**不在这里出口**：整页 GET 只有一条渲染路径（函数末尾那次 c.HTML），
	// 失败页与成功页渲染同一份模板、同一份页壳数据，差别只有状态码 / no-store / 失败槽
	//（契约见 docs/rules/template-boundary.md §整页 GET 的失败出口）。
	//
	// 本函数有两个调用方，**响应边界不同**（必须分开处理）：
	//   · ProductBundlePage：整页 GET（fragment=false）—— 失败 = 同页完整 HTML + 500/404/400 + no-store；
	//   · ProductBundleSave 的写失败重渲染（fragment=isHXRequest）—— 换的是页面里那一块，
	//     整页 HTML 塞进片段槽会把外层结构一起换掉，所以这一档**仍是片段**（200）。
	httpStatus := http.StatusOK
	var loadErr error
	fail := func(err error) {
		if loadErr == nil {
			loadErr = err
			// 取数没读到 = 500。不是「还没配」——见下面 GetBundleConfig 那一档。
			httpStatus = http.StatusInternalServerError
		}
	}
	projects, err := h.projects.List(ctx)
	if err != nil {
		fail(err)
	}
	selectedProject := bundleQueryParam(c, "project", "projectId")
	if selectedProject == "" && len(projects) > 0 {
		selectedProject = projects[0].ID
	}
	if loadErr == nil && selectedProject != "" && !productProjectExists(projects, selectedProject) {
		// 与商品域其它整页 GET 同一口径（判据是本次请求已加载的工程集合）。
		httpStatus = http.StatusBadRequest
		loadErr = errors.New(productenums.ErrInvalidParam)
	}
	var list []*productdto.ProductResp
	if loadErr == nil && selectedProject != "" {
		var lerr error
		if list, lerr = h.products.List(ctx, &productdto.ListReq{ProjectID: selectedProject, Size: 100}); lerr != nil {
			fail(lerr)
		}
	}
	selectedProduct := bundleQueryParam(c, "product", "productId")
	var detail *productdto.BundleConfigResp
	if loadErr == nil && selectedProduct != "" {
		var derr error
		detail, derr = h.products.GetBundleConfig(ctx, &productdto.GetBundleConfigReq{ProductID: selectedProduct})
		if derr != nil {
			// 按业务事实分档，不把三种情况混成一种：
			//   · 商品不存在（ErrNotFound）→ 404：这一页没有可读的对象；
			//   · 参数不合法（ErrInvalidParam）→ 400；
			//   · 其余（驱动 / 超时 / 权限）→ 500：**主数据没读到**。
			// 此前一律 detail=nil 渲染空骨架，把「读不出来」说成了「这个商品还没配捆绑」——
			// 而这两种事实对运营的动作完全不同（一个是稍后重试，一个是开始配置）。
			detail = nil
			switch productErrKeyOf(derr) {
			case productenums.ErrNotFound:
				httpStatus = http.StatusNotFound
				loadErr = derr
			case productenums.ErrInvalidParam:
				httpStatus = http.StatusBadRequest
				loadErr = derr
			default:
				fail(derr)
			}
		}
	}
	if submitted != nil && detail != nil {
		detail.Config = *submitted
	}
	var skus []*productdto.BundleSKUResp
	if loadErr == nil {
		var serr error
		if skus, serr = h.products.ListBundleSKUs(ctx, &productdto.ListBundleSKUReq{ProjectID: selectedProject}); serr != nil {
			fail(serr)
		}
	}
	// —— 成员来源面板的数据（docs/14 §1.2 的三种来源，批次 C）——
	//
	// 来源 / 来源商品 / 来源仓都是**页级上下文**（与工程、商品同一层）：用 GET 重新渲染，
	// 而不是让前端拼参数 —— 属性值勾选清单与仓库 SKU 候选都由服务端给出，
	// 前端不需要复刻任何口径（它只负责把选中的行追加进成员清单）。
	source := strings.TrimSpace(c.Query("source"))
	if source == "" {
		source = productenums.BundleSourceProduct
	}
	sourceProductID := strings.TrimSpace(c.Query("sourceProduct"))
	sourceWarehouseID := strings.TrimSpace(c.Query("sourceWarehouse"))
	// 来源属性候选也是取数：前面已经读失败时不再发（失败后零派生查询）。
	sourceAttrs := []*productdto.AttributeResp{}
	if loadErr == nil {
		sourceAttrs = h.bundleSourceAttributes(ctx, sourceProductID)
	}
	warehouses := []productdto.WarehouseOptionView{}
	if loadErr == nil {
		var werr error
		if warehouses, werr = h.warehouseOptions(ctx, selectedProject); werr != nil {
			fail(werr)
		}
	}
	// 仓库 SKU 候选**按仓分组**逐仓取（复用「从仓库选」的既有能力，见 product_warehouse_sku_view.go）：
	// 一次取全库再在内存分组会把整张库存表拉进内存，而这里只需要「最近常用的一屏」。
	warehouseGroups := []productdto.WarehouseSKUGroupView{}
	if loadErr == nil {
		var gerr error
		if warehouseGroups, gerr = h.warehouseSKUOptions(ctx, selectedProject, warehouses); gerr != nil {
			fail(gerr)
		}
	}
	// failure 只由**写动作失败重渲染**填（ProductBundleSave）——取代原先 302 + ?err= 的那条
	// 读侧判定已整批删除（见 product_jump.go 的文件头），首屏不再从 query 取任何提示。
	rows := bundleConfigRows(detail, skus)
	if submitted != nil {
		// 有提交快照就一律用它渲染（**不再要求 detail 也在**）：写失败之后重读又失败时，
		// 成员清单是这一屏里最贵的内容，不能因为页面重建失败就把它丢掉。
		rows = bundleSubmittedRows(detail, skus, c)
	}
	// 配置数值也跟同一份快照走：成员留下了、数值却回到 0，用户会以为配置被改过。
	cfgMaxOptions, cfgMinTotalQty, cfgMaxTotalQty := 0, 0, 0
	switch {
	case submitted != nil:
		cfgMaxOptions, cfgMinTotalQty, cfgMaxTotalQty = submitted.MaxOptions, submitted.MinTotalQty, submitted.MaxTotalQty
	case detail != nil:
		cfgMaxOptions, cfgMinTotalQty, cfgMaxTotalQty = detail.Config.MaxOptions, detail.Config.MinTotalQty, detail.Config.MaxTotalQty
	}
	data := gin.H{
		"title":           productenums.ProductBundleTitle,
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
		// Cfg*：配置数值的回填源（提交快照优先，其次才是库里的配置）。
		"CfgMaxOptions":  cfgMaxOptions,
		"CfgMinTotalQty": cfgMinTotalQty,
		"CfgMaxTotalQty": cfgMaxTotalQty,
		// Submitted：这一屏有「用户刚提交的快照」（写失败重渲染）。
		// SaveBlocked：页面重建失败（loadErr != nil）时禁止保存 —— 配置读不出来就无法保证
		// 这次提交的是完整配置，宁可让用户先「重新载入」，也不要缺配置误写。
		"Submitted":   submitted != nil,
		"SaveBlocked": loadErr != nil,
		// 成员来源面板（批次 C）：三种来源 + 各自的候选数据。
		// 槽位收事实（Key + 中文兜底 / 数据名），取词在模板。
		"Sources":           bundleSourceOptions(source),
		"Source":            source,
		"SourceProducts":    bundleSourceProductOptions(list, sourceProductID),
		"SourceAttributes":  sourceAttrs,
		"SourceWarehouses":  bundleSourceWarehouseGroups(warehouseGroups, sourceWarehouseID),
		"SourceWarehouseID": sourceWarehouseID,
		"ResolveURL":        "/admin/products/bundle/members/resolve",
		// 页面上下文（表单 action 的 query）：保存失败 / 成功时由 shell.BackPath 读回。
		"ListQuery": productQueryFromRequest(c, productBundleBackKeys...),
	}
	if loadErr != nil {
		data["LoadFailed"] = true
		data["LoadError"] = productErrPageText(c, loadErr)
		// 这一屏不是可缓存的成功页：重试必须真的回到服务端。
		c.Header("Cache-Control", "no-store")
	}
	// 片段档（htmx 写失败重渲染）：换的是页面里那一块，整页失败页塞进片段槽会把外层
	// 结构一起换掉 —— 这一档**仍是片段**（200）。失败信息进两个槽：Err 是这次保存没成功
	// 的原因（调用方给），LoadError 是「这一页没能重建出来」；有提交快照时片段里照样渲染
	// 回填表单（保存禁用），否则 hx-swap=outerHTML 会把用户整块表单换成一句提示。
	if fragment {
		c.HTML(http.StatusOK, "admin/product/product_bundle_form.html", adminui.Prepare(c, data))
		return
	}
	if loadErr != nil && submitted != nil {
		// 原生 POST 写失败 + 重读也失败：整页把提交快照渲染回来。
		//
		// 状态码保持 200：这不是「整页 GET 读不出来」（那一条是 500 + 失败槽），而是
		// 「写没成功、页面重建失败」——响应本身是完整可用的页面（表单在、保存禁用、
		// 给了「重新载入」入口），把用户刚输入的内容换成一个失败槽才是更坏的结果。
		c.HTML(http.StatusOK, "admin/product/product_bundle.html", adminui.Prepare(c, data))
		return
	}
	// 整页档：出口只有这一处（页面成败都走它）。没有提交快照的装载失败 = 失败槽 + 状态码。
	c.HTML(httpStatus, "admin/product/product_bundle.html", adminui.Prepare(c, data))
}

// ProductBundleSave 保存捆绑配置；失败在当前请求回填，HX 成功整页跳转。
func (h *productPageHandle) ProductBundleSave(c *gin.Context) {
	ctx := c.Request.Context()
	productID := strings.TrimSpace(c.PostForm("productId"))
	if productID == "" {
		productBundleJump(c, false, productBundleNoProductMsg)
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
		// 失败**原地重渲染**（保留用户刚配的成员清单），不跳提示页：
		// 成员表是这一屏里最贵的内容，跳走等于让它整屏消失（见 internal/templates/CLAUDE.md
		// 「写表单失败时原地留住输入」）。htmx 档换片段、原生档换整页。
		q := url.Values{"product": {productID}}
		if project := strings.TrimSpace(c.PostForm("projectId")); project != "" {
			q.Set("project", project)
		}
		c.Request.URL.RawQuery = q.Encode()
		h.renderBundlePage(c, &cfg, productErrText(c, err), isHXRequest(c))
		return
	}
	// 成功走整页提示（htmx 由 shell.RenderJump 输出 HX-Redirect）——取代原先的 302 / HX-Redirect。
	productBundleJump(c, true, productActionDoneMsg)
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
//
// 来源列给的是**事实段**（bundleSourceParts：词条 + 数据），取词与分段拼接在模板 ——
// 控制器不取词，否则同一个来源在英文界面上会留一句中文。
func bundleConfigRows(detail *productdto.BundleConfigResp, skus []*productdto.BundleSKUResp) []gin.H {
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
				bundleSourceParts(o.BundleOption), label))
		}
	}
	if len(rows) < productdto.BundleMaxOptionsLimit {
		rows = append(rows, bundleRow(productdto.BundleOption{Required: true, DefaultQty: 1, MinQty: 1}, 0, nil, ""))
	}
	return rows
}

// bundleRow 构造一行数据；候选 SKU 列表仅供唯一空白行使用。
func bundleRow(o productdto.BundleOption, available int, sourceParts []productdto.BundleSourcePart, skuLabel string) gin.H {
	if sourceParts == nil {
		// 空白行（还没选 SKU）没有来源可言：给空切片而不是 nil，模板侧只按 len 判断。
		sourceParts = []productdto.BundleSourcePart{}
	}
	return gin.H{
		"VariantID":  o.VariantID,
		"SKULabel":   skuLabel,
		"Required":   o.Required,
		"DefaultQty": o.DefaultQty,
		"MinQty":     o.MinQty,
		"MaxQty":     o.MaxQty,
		"Available":  available,
		// 来源快照（仅作溯源与展示，不是身份）：四个字段原样回填进隐藏域，
		// 代表当前这一行的来源；**标签**已拆成事实段（取词在模板）。
		"SourceKind":   o.SourceKind,
		"WarehouseID":  o.WarehouseID,
		"WarehouseSKU": o.WarehouseSKU,
		"ExternalSKU":  o.ExternalSKU,
		"SourceParts":  sourceParts,
	}
}

// bundleSubmittedRows rebuilds rows from the posted arrays, not persisted configuration.
func bundleSubmittedRows(detail *productdto.BundleConfigResp, skus []*productdto.BundleSKUResp, c *gin.Context) []gin.H {
	labels := make(map[string]string, len(skus))
	for _, s := range skus {
		if s != nil {
			labels[s.VariantID] = s.ProductName + " · " + s.SKUCode
		}
	}
	// detail 可能为 nil：写失败重渲染时配置又读不出来（双失败档），此时仍要用
	// POST 里的提交快照把成员清单渲染回来 —— SKU 标签退回「从 POST / skus 能拿到的部分」。
	if detail != nil {
		for _, o := range detail.Options {
			if o != nil && labels[o.VariantID] == "" && o.SKUCode != "" {
				labels[o.VariantID] = strings.TrimSpace(o.ProductName + " · " + o.SKUCode)
			}
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
		rows = append(rows, bundleRow(option, 0, bundleSourceParts(option), label))
	}
	if len(rows) < productdto.BundleMaxOptionsLimit && !hasBlank {
		rows = append(rows, bundleRow(productdto.BundleOption{Required: true, DefaultQty: 1, MinQty: 1}, 0, nil, ""))
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

// bundleSourceOptions 来源下拉的三项（值即 enums 常量 —— 服务端按它选解析分支）。
//
// 槽位收事实（Key + 中文兜底），取词在模板：控制器提前翻等于把这三句锁死成中文。
// 事实本身来自 productenums.BundleSourceLabel —— 与成员清单的「来源」列**同一份映射**
// （此前下拉自带一句更长的兜底「从商品导入（该商品的启用变体）」，与库内词条不是同一句）。
func bundleSourceOptions(selected string) []gin.H {
	values := []string{
		productenums.BundleSourceProduct,
		productenums.BundleSourceWarehouse,
		productenums.BundleSourceAttributes,
	}
	out := make([]gin.H, 0, len(values))
	for _, value := range values {
		key, fallback := productenums.BundleSourceLabel(value)
		out = append(out, gin.H{
			"Value":         value,
			"LabelKey":      key,
			"LabelFallback": fallback,
			"Selected":      value == selected,
		})
	}
	return out
}

// bundleSourceProductOptions 来源商品下拉项（工程内全部商品；自引用由服务端拒绝）。
//
// 「— 不选 —」空项给词条（事实），商品项给数据名 —— 与 productPickOptions 同一形状。
// 商品项同样**不给空的 LabelKey 键**（模板判据是 isset，空串也会走取词分支渲染成空白）。
func bundleSourceProductOptions(products []*productdto.ProductResp, selected string) []gin.H {
	out := make([]gin.H, 0, len(products)+1)
	out = append(out, gin.H{
		"ID": "", "LabelKey": productenums.ProductBundleOptionNone, "LabelFallback": "— 不选 —",
		"Selected": selected == "",
	})
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
func bundleSourceWarehouseGroups(groups []productdto.WarehouseSKUGroupView, selectedWarehouseID string) []gin.H {
	out := make([]gin.H, 0, len(groups))
	for _, g := range groups {
		out = append(out, gin.H{
			"WarehouseID": g.WarehouseID,
			"Label":       g.Label,
			"IsDefault":   g.IsDefault,
			"Selected":    g.WarehouseID != "" && g.WarehouseID == selectedWarehouseID,
			"Items":       g.Items,
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

// productBundleNoProductText 捆绑页的参数级提示（未选商品就提交保存），走整页提示页。
//
// key + 中文兜底：写侧取词（英文站点不再显示中文）。
const productBundleNoProductFallback = "请先选择商品"

// productBundleNoProductMsg 上面那条提示的**事实**（key + 中文兜底，取词在提示页模板里）。
var productBundleNoProductMsg = productMsg{Key: productenums.ProductBundleNoProduct, Fallback: productBundleNoProductFallback}

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
	// 例外（JSON 出口的成品串）：这个端点的响应体是给前端直接渲染的（sourceLabel / skips.text），
	// 没有模板取词这一层 —— 与页面槽位不同，这里的字符串必须已经是当前语言的成品。
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

// isBundleProduct 商品类型是否为捆绑容器（详情页据此决定要不要渲染「捆绑构成」）。
//
// 判据只认类型本身，不看「bundle_items 是否非空」——后者是迁移 238 之前的旧口径，
// 分不清「主体卖自己的 SKU」与「容器卖组合」（bundle 商品的价 0 变体就是这么来的）。
func isBundleProduct(productType string) bool {
	return productType == productmodel.TypeBundle
}

// bundleSourceParts 成员来源快照的**事实段**（仅用于展示 —— 来源不是身份）。
//
// 「从仓库选」再多带两段：仓库 SKU 与外部编码 —— 运营对账时手上可能是其中任意一个；
// 仓库 id 留在 JSON 里做溯源，不搬上表格（uuid 对读的人没有含义）。
//
// 第一段恒存在（来源词条），后两段按数据有无增减 —— 模板据段数为 0 渲染「—」。
// 来源 → (key, 兜底) 的映射在 productenums.BundleSourceLabel（空 kind = 历史配置 / 手工指定）。
func bundleSourceParts(o productdto.BundleOption) []productdto.BundleSourcePart {
	key, fallback := productenums.BundleSourceLabel(strings.TrimSpace(o.SourceKind))
	parts := make([]productdto.BundleSourcePart, 0, 3)
	parts = append(parts, productdto.BundleSourcePart{LabelKey: key, LabelFallback: fallback})
	if sku := strings.TrimSpace(o.WarehouseSKU); sku != "" {
		parts = append(parts, productdto.BundleSourcePart{
			LabelKey: productenums.ProductBundleSourceWarehouseSKULabel, LabelFallback: "仓库 SKU", Value: sku,
		})
	}
	if ext := strings.TrimSpace(o.ExternalSKU); ext != "" {
		parts = append(parts, productdto.BundleSourcePart{
			LabelKey: productenums.ProductBundleSourceExternalSKULabel, LabelFallback: "外部编码", Value: ext,
		})
	}
	return parts
}

// bundleSourceLabel 来源事实段 → 当前语言的成品串。
//
// **只有 JSON 出口用它**（ProductsBundleMembersResolve 的 `sourceLabel`：前端把它直接显示，
// 没有模板取词这一层）—— 属于「JSON 出口的成品串」例外，调用点的 tr 同理。
// 模板侧一律用 bundleSourceParts 交事实、在模板里取词与拼接。
func bundleSourceLabel(tr func(key, fallback string) string, o productdto.BundleOption) string {
	segments := make([]string, 0, 3)
	for _, part := range bundleSourceParts(o) {
		text := tr(part.LabelKey, part.LabelFallback)
		if part.Value != "" {
			text += " " + part.Value
		}
		segments = append(segments, text)
	}
	return strings.Join(segments, " · ")
}

// bundlePanelRows 捆绑构成表的行：成员 SKU 的规则、可用量与后台价格口径。
//
// 成员挂牌价（ItemPrice）在这里只作**参考**：套餐金额只由容器价得出，成员价不参与
// 任何计价与前台展示（迁移 238 的类型不变量）；成员成本只进后台口径。
//
// SKU 已被删除时配置里只剩 variantId（SKUCode 为空），照常出一行并标注 ——
// 静默丢项会让人以为配置里本来就没有这一项，也就不会去修它。
func bundlePanelRows(options []*productdto.BundleOptionDetail) []gin.H {
	rows := make([]gin.H, 0, len(options))
	for _, o := range options {
		if o == nil {
			continue
		}
		// Missing 表示配置指向的变体已经不存在（与「存在但被停用」是两回事，
		// 状态文案与可行动作都不同，故不合并成一个 bool）。
		missing := o.SKUCode == ""
		itemPrice, costPrice := formatAmount(o.ItemPrice), formatNullableAmount(o.CostPrice)
		if missing {
			// 变体没了，价格与成本都无从谈起：这里的 0 会被读成「这个成员不要钱」。
			itemPrice, costPrice = "—", "—"
		}
		rows = append(rows, gin.H{
			"VariantID":   o.VariantID,
			"SKUCode":     o.SKUCode,
			"ProductName": o.ProductName,
			"Required":    o.Required,
			"DefaultQty":  o.DefaultQty,
			"MinQty":      o.MinQty,
			"MaxQty":      o.MaxQty,
			"Available":   o.Available,
			"ItemPrice":   itemPrice,
			"CostPrice":   costPrice,
			"Enabled":     o.Enabled,
			"Missing":     missing,
			// 来源快照（仅展示）：事实段，取词与分段拼接在模板；空 = 手工指定 / 历史配置。
			"SourceParts": bundleSourceParts(o.BundleOption),
		})
	}
	return rows
}

// bundlePanel 商品详情页「捆绑构成」区块的数据（仅 type=bundle 调用）。
//
// 读不出配置时**不静默**：区块照常渲染并带一条可读原因（区块本身就是运维发现
// 「配置读不出来」的地方，整块消失会让人以为这个商品不是捆绑品）。
// 错误原因只给词条文案，不铺 err.Error()（内部包装文案对运营不可行动）。
func (h *productPageHandle) bundlePanel(c *gin.Context, projectID, productID string) gin.H {
	panel := gin.H{
		"Loaded":    false,
		"ErrorText": "",
		// 容器价是套餐金额的唯一来源，读不出来时给 —（不拿成员价兜底：那是错的口径）。
		"BasePrice":   "—",
		"Options":     []gin.H{},
		"OptionCount": 0,
	}
	detail, err := h.products.GetBundleConfig(c.Request.Context(), &productdto.GetBundleConfigReq{
		ProductID: productID,
		// 工程显式传下去（DB-009）：配置详情含库存可用量，走真源查询要在工程作用域里。
		// 后台页不走 resolveProjectID 的唯一工程兜底（多工程下它会直接判参数错）。
		ProjectID: projectID,
	})
	if err != nil || detail == nil {
		panel["ErrorText"] = productMsgText(c, bundlePanelReadFailedMsg)
		return panel
	}
	rows := bundlePanelRows(detail.Options)
	panel["Loaded"] = true
	panel["BasePrice"] = formatAmount(detail.BasePrice)
	panel["Options"] = rows
	panel["OptionCount"] = len(rows)
	return panel
}

// warehouseOptions 某工程的仓库下拉项（issue #15；默认仓在最前并标注）。
//
// 下沉后这里只是**端口的转调**：标签怎么拼、默认仓后缀用哪个词条、除 Label 外还带
// 哪些列（列表「库存」列要把未入库的仓也列出来，靠的是 Code / Name）—— 口径唯一一份
// 在 outbound 适配器里，页面处理器与读模型因此不可能分叉。
//
// 未注入（h.warehouses == nil）时返回空列表：模板此时不渲染下拉，变体创建按
// 「不指定仓库」处理。
func (h *productPageHandle) warehouseOptions(ctx context.Context, projectID string) (out []productdto.WarehouseOptionView, err error) {
	if h.warehouses == nil {
		return []productdto.WarehouseOptionView{}, nil
	}
	return h.warehouses.WarehouseOptions(ctx, projectID)
}

// productStockCell 商品列表「库存」列的数据（三态 + 分仓明细）。
//
// 三态口径（docs/14 §1.4）：任一仓不跟踪 → ∞（**绝不求和**，求和等于把无限当 0）；
// 全部跟踪 → 各仓数量之和（0 就显示 0）；一个仓都没有这一行 → 未入库。
//
// 分仓明细按**工程仓库清单**逐仓列出：服务端返回的 StockWarehouses 只含有库存行的仓，
// 「某仓没有行」同样是一条信息（该仓未入库）—— 只列有行的仓会让运营以为这个商品只在那几个仓。
//
// 文案（∞ / 未入库）留在模板侧用 tr() 取词，这里只给出状态与数字：
// 一种语言一个值，别把中文拼进数据层（接口调用方拿到的会是中文）。
func productStockCell(detail *productdto.ProductResp, warehouses []productdto.WarehouseOptionView) gin.H {
	cell := gin.H{
		"State": productenums.StockStateNone,
		"Total": 0,
		"Rows":  []gin.H{},
	}
	if detail == nil {
		return cell
	}
	if detail.StockState != "" {
		cell["State"] = detail.StockState
	}
	cell["Total"] = detail.StockTotal
	byWarehouse := make(map[string]*productdto.ProductWarehouseStockResp, len(detail.StockWarehouses))
	for _, row := range detail.StockWarehouses {
		if row != nil {
			byWarehouse[row.WarehouseID] = row
		}
	}
	rows := make([]gin.H, 0, len(warehouses))
	for _, w := range warehouses {
		item := gin.H{"WarehouseID": w.ID, "WarehouseLabel": w.Label,
			"State": productenums.StockStateNone, "Quantity": 0}
		if row, ok := byWarehouse[w.ID]; ok {
			item["State"] = row.State
			item["Quantity"] = row.Quantity
			if row.SKUCode != "" {
				item["SKUCode"] = row.SKUCode
			}
		}
		rows = append(rows, item)
	}
	cell["Rows"] = rows
	return cell
}

// bundlePanelReadFailedFallback 捆绑配置读取失败的中文兜底（key 在
// productenums.ProductBundleDetailFailed）。
//
// 与捆绑配置页同一句：两处读的是同一份配置，失败原因也同一批（商品不存在 /
// 库存真源端口未接入，后者拿不到可用量）。复用词条而不是另起一句，
// 免得同一件事在两个页面上给出两种说法。
// bundlePanelReadFailedMsg 上面那条提示的**事实**（片段槽位按文本渲染，见调用点注释）。
var bundlePanelReadFailedMsg = productMsg{Key: productenums.ProductBundleDetailFailed, Fallback: bundlePanelReadFailedFallback}

// bundlePanelReadFailedFallback 上述 key 的中文兜底（i18n 未初始化或词条缺失时用）。
const bundlePanelReadFailedFallback = "配置读取失败：商品不存在，或库存真源端口未接入（无法给出可用量）。"

// warehouseSKUOptionSize 已随取数下沉搬到 outbound 适配器
//（internal/module/product/outbound/inventory 的 warehouseSKUOptionSize）：
// 每仓取多少条候选是**取数口径**，与页面怎么渲染无关。

// warehouseSKUOptions 新建商品抽屉「从仓库选」的候选：(仓库 → 该仓的货) 分组（迁移 251）。
//
// 与 warehouseOptions 同一手法：只是**端口的转调** —— 逐仓取 / 每仓上限 / 裸码剥离
// 这几条口径唯一一份在 outbound 适配器（见 product/outbound/inventory）。
//
// 未注入库存契约（装配缺陷 / 直接渲染模板的单测）时返回空 —— 抽屉照常渲染，
// 只是没有「从仓库选」这条入口（接口路径传来的 skuSource=warehouse 仍由 service 校验）。
func (h *productPageHandle) warehouseSKUOptions(ctx context.Context, projectID string, warehouses []productdto.WarehouseOptionView) (out []productdto.WarehouseSKUGroupView, err error) {
	if h.warehouses == nil {
		return []productdto.WarehouseSKUGroupView{}, nil
	}
	return h.warehouses.WarehouseSKUOptions(ctx, projectID, warehouses)
}

// ==========================================================================
// 捆绑接口的四个出口：读配置、存配置、整单校验、SKU 数据源。
// 整单校验在这里是**后端硬校验**：前端片段调的是同一个 service 方法，
// 因此「构造请求绕过前端」得到的结论与前台一致（验收 4）。
// ==========================================================================
// GetBundleConfig 读某商品的捆绑配置（含每项 SKU 快照与真源可用量）。
func (h *Handle) GetBundleConfig(c *gin.Context) {
	req := &productdto.GetBundleConfigReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.GetBundleConfig(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDetailSuccess, res)
}

// SetBundleConfig 保存捆绑配置（整体替换）。
func (h *Handle) SetBundleConfig(c *gin.Context) {
	req := &productdto.SetBundleConfigReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	// issue #19：配置变更要落主数据变更记录，操作人只能来自会话。
	req.OperatorID = operatorFromContext(c)
	res, err := h.svc.SetBundleConfig(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgBundleSaveSuccess, res)
}

// ValidateBundleSelection 整单硬校验 + 算价（纯读，不写库）。
func (h *Handle) ValidateBundleSelection(c *gin.Context) {
	req := &productdto.ValidateBundleSelectionReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.ValidateBundleSelection(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgBundleValidateSuccess, res)
}

// ListBundleSKUs 可挑选的 SKU 清单（跨商品，后台配置器的下拉数据源）。
func (h *Handle) ListBundleSKUs(c *gin.Context) {
	req := &productdto.ListBundleSKUReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	list, err := h.svc.ListBundleSKUs(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgListSuccess, list)
}
