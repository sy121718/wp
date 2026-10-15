// product_bundle_page.go — 捆绑配置页的读模型（取数编排 + 行 / 选项装配 + 页级事实）。
//
// 与其余八屏同一形态。这一屏的装配量比前面几屏大（成员表行、候选下拉、三种来源面板
// 全在这一屏算出），所以它们也一并搬进来，而不是留在控制器 —— 它们都是**纯函数**，
// 输入是读回来的原料，输出是渲染行，一条也不需要 gin.Context：
//
//	· 商品标签（"商品名 · SKU"）从 skus 现查，配置里存的 ProductName 可能是改过的旧值；
//	· 成员行的「来源」列给的是**事实段**（词条 + 数据），取词与分段拼接在模板；
//	· 来源下拉的三项、来源商品下拉、来源仓分组全是 `BundleSourceLabel` 的同一份映射。
//
// 留在控制器的只有两样：写路径产物 Submitted（请求体里的东西，读模型不认识 gin.Context）
// 与两个 URL 形状（ResolveURL / ListQuery）。
//
// 下拉项里 Sources / SourceProducts 刻意**仍是 map 而不是结构体**：它们的判据是
// 「LabelKey 键在不在」—— 空项给词条、数据项整键省略，模板用 isset 分两条取值链。
// 结构体的字段永远存在，套不上这个判据（数据项会走进取词分支、渲染成空白）。
// 同 productPickOptions 的取舍，门禁是 product_dropdown_option_shape_test.go。
package productservice

import (
	"context"
	"errors"
	"strconv"
	"strings"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	projectdto "go_wp/internal/module/project/dto"
)

// BundlePageProductSize 来源商品下拉的取值上限（这一屏一次读全工程商品）。
const BundlePageProductSize = 100

// ProductBundlePage 捆绑配置页的读模型。
//
// 顺序与原控制器逐字对齐（工程清单 → 工程存在性 → 工程商品 → 捆绑配置三档 → 候选 SKU
// → 来源属性 → 仓库清单 → 逐仓 SKU 候选），因为「谁先失败」决定用户看到哪一档：
// 工程清单读不到 500、`?project=<不存在的 id>` 400、配置读不到三档各自不同（见下面）。
func (s *Service) ProductBundlePage(ctx context.Context, req *productdto.ProductBundlePageReq) (*productdto.ProductBundlePageView, error) {
	view := &productdto.ProductBundlePageView{
		Title:            productenums.ProductBundleTitle,
		Menu:             "products",
		Projects:         []projectdto.ProjectResp{},
		Products:         []*productdto.ProductResp{},
		Skus:             []*productdto.BundleSKUResp{},
		Rows:             []productdto.BundleOptionRow{},
		CandidateSKUs:    []productdto.BundleCandidateView{},
		Sources:          []map[string]any{},
		SourceProducts:   []map[string]any{},
		SourceAttributes: []*productdto.AttributeResp{},
		SourceWarehouses: []productdto.BundleSourceWarehouseGroupView{},
		MaxOptions:       productdto.BundleMaxOptionsLimit,
	}
	var firstErr error
	fail := func(e error) {
		if firstErr == nil {
			firstErr = e
			// 取数没读到 = 500。不是「还没配」——见下面捆绑配置那一档。
			view.HTTPStatus = 500
		}
	}

	projects, perr := s.project.List(ctx)
	if perr != nil {
		fail(perr)
	} else {
		view.Projects = projects
	}

	var submitted *productdto.BundleSubmittedForm
	source := ""
	sourceProductID := ""
	sourceWarehouseID := ""
	selectedProject := ""
	selectedProduct := ""
	if req != nil {
		submitted = req.Submitted
		selectedProject = strings.TrimSpace(req.ProjectID)
		selectedProduct = strings.TrimSpace(req.ProductID)
		source = strings.TrimSpace(req.Source)
		sourceProductID = strings.TrimSpace(req.SourceProductID)
		sourceWarehouseID = strings.TrimSpace(req.SourceWarehouseID)
	}
	if selectedProject == "" && len(projects) > 0 {
		selectedProject = projects[0].ID
	}
	if firstErr == nil && selectedProject != "" && !ProjectExists(projects, selectedProject) {
		view.HTTPStatus = 400
		firstErr = errors.New(productenums.ErrInvalidParam)
	}
	// 未选来源商品时给空值：来源属性候选是「来源商品的属性组」，
	// 没选商品时它必然是空 —— 省掉面板一次无意义的查询（与接线前同一档：那时
	// bundleSourceAttributes 也在这条上直接返回空清单）。
	if selectedProduct == "" {
		sourceProductID = ""
	}

	if firstErr == nil && selectedProject != "" {
		list, lerr := s.List(ctx, &productdto.ListReq{ProjectID: selectedProject, Size: BundlePageProductSize})
		if lerr != nil {
			fail(lerr)
		} else if list != nil {
			view.Products = list
		}
	}

	var detail *productdto.BundleConfigResp
	if firstErr == nil && selectedProduct != "" {
		// 带工程作用域：GetBundleConfigReq.ProjectID 为空时由 service 走「唯一工程」兜底，
		// 多工程部署下那一步回 ErrInvalidParam，被下面判成 400 —— 整页打不开，
		// 而商品本来就在上面已解析出的 selectedProject 里。
		cfg, derr := s.GetBundleConfig(ctx, &productdto.GetBundleConfigReq{ProductID: selectedProduct, ProjectID: selectedProject})
		if derr != nil {
			// 按业务事实分档，不把三种情况混成一种（此前一律 detail=nil 渲染空骨架，
			// 把「读不出来」说成了「这个商品还没配捆绑」—— 这两种事实对运营的动作完全不同：
			// 一个是稍后重试，一个是开始配置）：
			//   · 商品不存在（ErrNotFound）→ 404；参数不合法 → 400；其余（驱动 / 超时 / 权限）→ 500。
			detail = nil
			switch {
			case isProductErrKey(derr, productenums.ErrNotFound):
				view.HTTPStatus = 404
				firstErr = derr
			case isProductErrKey(derr, productenums.ErrInvalidParam):
				view.HTTPStatus = 400
				firstErr = derr
			default:
				fail(derr)
			}
		} else {
			detail = cfg
		}
	}
	if submitted != nil && detail != nil {
		// 配置数值也从提交快照走（成员留下了、数值却回到 0，用户会以为配置被改过）。
		detail.Config.MaxOptions = submitted.MaxOptions
		detail.Config.MinTotalQty = submitted.MinTotalQty
		detail.Config.MaxTotalQty = submitted.MaxTotalQty
	}
	view.Detail = detail

	if firstErr == nil {
		skus, serr := s.ListBundleSKUs(ctx, &productdto.ListBundleSKUReq{ProjectID: selectedProject})
		if serr != nil {
			fail(serr)
		} else if skus != nil {
			view.Skus = skus
		}
	}

	// 成员来源面板的数据（docs/14 §1.2 的三种来源，批次 C）：来源 / 来源商品 / 来源仓
	// 都是**页级上下文**（与工程、商品同一层）：用 GET 重新渲染，而不是让前端拼参数 ——
	// 属性值勾选清单与仓库 SKU 候选都由服务端给出，前端不需要复刻任何口径
	//（它只负责把选中的行追加进成员清单）。
	// 三处取数与前面一样遵守「失败后零派生查询」。
	if source == "" {
		source = productenums.BundleSourceProduct
	}
	view.Source = source
	view.SourceWarehouseID = sourceWarehouseID
	if firstErr == nil && sourceProductID != "" {
		attrs := s.BundleSourceAttributes(ctx, selectedProject, sourceProductID)
		view.SourceAttributes = attrs
	}
	warehouses := []productdto.WarehouseOptionView{}
	if firstErr == nil {
		var werr error
		if warehouses, werr = s.BundlePageWarehouses(ctx, selectedProject); werr != nil {
			fail(werr)
		}
	}
	// 仓库 SKU 候选**按仓分组**逐仓取（复用「从仓库选」的既有能力）：一次取全库再在内存分组
	// 会把整张库存表拉进内存，而这里只需要「最近常用的一屏」。
	warehouseGroups := []productdto.WarehouseSKUGroupView{}
	if firstErr == nil {
		var gerr error
		if warehouseGroups, gerr = s.BundlePageWarehouseSKUs(ctx, selectedProject, warehouses); gerr != nil {
			fail(gerr)
		}
	}
	view.SelectedProject = selectedProject
	view.SelectedProduct = selectedProduct
	view.SourceWarehouses = bundleSourceWarehouseGroups(warehouseGroups, sourceWarehouseID)

	// —— 以下装配在**失败档也要做**（逐字对齐原控制器）——
	//
	// 失败页上这一屏仍有表单：用户刚配的东西不能因为一次读失败就消失，保存入口则撤掉
	//（SaveBlocked）。只置 LoadFailed 而不给这些键，模板会在缺键上断渲染。
	if submitted != nil {
		// 有提交快照就一律用它渲染（**不再要求 detail 也在**）：写失败之后重读又失败时，
		// 成员清单是这一屏里最贵的内容，不能因为页面重建失败就把它丢掉。
		// 配置数值也跟同一份快照走：成员留下了、数值却回到 0，用户会以为配置被改过。
		view.Rows = bundleSubmittedRows(detail, view.Skus, submitted)
		view.CfgMaxOptions, view.CfgMinTotalQty, view.CfgMaxTotalQty =
			submitted.MaxOptions, submitted.MinTotalQty, submitted.MaxTotalQty
	} else {
		view.Rows = bundleConfigRows(detail, view.Skus)
		if detail != nil {
			view.CfgMaxOptions, view.CfgMinTotalQty, view.CfgMaxTotalQty =
				detail.Config.MaxOptions, detail.Config.MinTotalQty, detail.Config.MaxTotalQty
		}
	}
	view.CandidateSKUs = bundleCandidateOptions(view.Skus)
	view.SkuCount = len(view.Skus)
	view.Sources = bundleSourceOptions(source)
	view.SourceProducts = bundleSourceProductOptions(view.Products, sourceProductID)
	view.Submitted = submitted != nil
	// 页面重建失败时禁止保存：配置读不出来就无法保证这次提交的是完整配置，
	// 宁可让用户先「重新载入」，也不要缺配置误写。
	view.SaveBlocked = firstErr != nil
	view.LoadFailed = firstErr != nil
	if firstErr != nil {
		return view, firstErr
	}
	return view, nil
}

// BundleSourceAttributes 来源商品的「参与变体」属性组（自选属性组合来源的勾选清单）。
//
// 只列参与变体的属性组：不参与变体的组构不出组合（与变体生成抽屉同一口径）。
// 未选来源商品 / 读不出来 / 该商品没有参与变体的组，都返回空清单 ——
// 页面据它渲染一句可行动的提示（而不是让面板变成一块无法操作的空白）。
//
// 读不出来当空清单而不是失败：这一屏对它的依赖是**展示**（可勾选的候选），
// 前置条件是配置本身 —— 让面板的可选清单决定整页成败，运营连配置都改不了了。
//
// projectID 必须由调用方给（本页已解析出的 selectedProject）：取商品那一跳不带作用域时，
// 多工程部署下 product 侧的 resolveProjectID 回 ErrInvalidParam，本函数的「读不出来」
// 分支把它静默吞成空清单 —— 用户看到「没有可勾选属性」，不报错、日志也干净，
// 而那个来源商品本来就在同一个工程里。
func (s *Service) BundleSourceAttributes(ctx context.Context, projectID, productID string) []*productdto.AttributeResp {
	id := strings.TrimSpace(productID)
	if id == "" {
		return []*productdto.AttributeResp{}
	}
	detail, err := s.Get(ctx, &productdto.GetReq{ID: id, ProjectID: projectID})
	if err != nil || detail == nil {
		return []*productdto.AttributeResp{}
	}
	return VariationAttributes(detail.Attributes)
}

// BundlePageWarehouses 本工程的仓库清单（端口未注入 = 空清单，与接线前同一档降级）。
func (s *Service) BundlePageWarehouses(ctx context.Context, projectID string) ([]productdto.WarehouseOptionView, error) {
	if s.pageWarehouses == nil {
		return []productdto.WarehouseOptionView{}, nil
	}
	return s.pageWarehouses.WarehouseOptions(ctx, projectID)
}

// BundlePageWarehouseSKUs 每仓一屏的仓库 SKU 候选（端口未注入 = 空分组）。
func (s *Service) BundlePageWarehouseSKUs(ctx context.Context, projectID string, warehouses []productdto.WarehouseOptionView) ([]productdto.WarehouseSKUGroupView, error) {
	if s.pageWarehouses == nil {
		return []productdto.WarehouseSKUGroupView{}, nil
	}
	return s.pageWarehouses.WarehouseSKUOptions(ctx, projectID, warehouses)
}

// BundleSourceParts 成员来源快照的**事实段**（仅用于展示 —— 来源不是身份）。
//
// 一行给出「来源种类 + 仓库 SKU + 外部编码」三段事实，取词与分段拼接在模板：
// 控制器提前翻等于把这一列锁死成中文。
func BundleSourceParts(o productdto.BundleOption) []productdto.BundleSourcePart {
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

// bundleConfigRows 成员表行（从**库里的配置**来）。
//
// 表单是**原生并行数组**（variantId / required / defaultQty / minQty / maxQty 各自多值），
// 零自定义 JS 就能表达「一行的多个字段」—— 不用 JSON 字符串塞进隐藏域，
// 也就没有「前端拼 JSON 出错但服务端照单全收」的缝隙。
// 并行数组靠 DOM 顺序对齐，少一个字段就会让后面所有行的索引错位。
func bundleConfigRows(detail *productdto.BundleConfigResp, skus []*productdto.BundleSKUResp) []productdto.BundleOptionRow {
	rows := make([]productdto.BundleOptionRow, 0, productdto.BundleMaxOptionsLimit)
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
			rows = append(rows, bundleRow(o.BundleOption, o.Available, BundleSourceParts(o.BundleOption), label))
		}
	}
	if len(rows) < productdto.BundleMaxOptionsLimit {
		rows = append(rows, bundleRow(productdto.BundleOption{Required: true, DefaultQty: 1, MinQty: 1}, 0, nil, ""))
	}
	return rows
}

// bundleRow 构造一行数据；候选 SKU 列表仅供唯一空白行使用。
func bundleRow(o productdto.BundleOption, available int, sourceParts []productdto.BundleSourcePart, skuLabel string) productdto.BundleOptionRow {
	if sourceParts == nil {
		// 空白行（还没选 SKU）没有来源可言：给空切片而不是 nil，模板侧只按 len 判断。
		sourceParts = []productdto.BundleSourcePart{}
	}
	return productdto.BundleOptionRow{
		VariantID:    o.VariantID,
		SKULabel:     skuLabel,
		Required:     o.Required,
		DefaultQty:   o.DefaultQty,
		MinQty:       o.MinQty,
		MaxQty:       o.MaxQty,
		Available:    available,
		SourceKind:   o.SourceKind,
		WarehouseID:  o.WarehouseID,
		WarehouseSKU: o.WarehouseSKU,
		ExternalSKU:  o.ExternalSKU,
		SourceParts:  sourceParts,
	}
}

// bundleSubmittedRows 从**本次提交**的原始数组重建行，而不是从库里读回来的配置。
//
// 与保存路径的分工：保存时空白行被跳过（它不是一个成员），渲染时空白行必须**原样占位** ——
// 行与数组按 DOM 顺序一一对应，摘掉空行会让后面的成员整体上移。
func bundleSubmittedRows(detail *productdto.BundleConfigResp, skus []*productdto.BundleSKUResp, form *productdto.BundleSubmittedForm) []productdto.BundleOptionRow {
	labels := make(map[string]string, len(skus))
	for _, s := range skus {
		if s != nil {
			labels[s.VariantID] = s.ProductName + " · " + s.SKUCode
		}
	}
	// detail 可能为 nil：写失败重渲染时配置又读不出来（双失败档），此时仍要用
	// 提交快照把成员清单渲染回来 —— SKU 标签退回「从候选 / 配置里能拿到的部分」。
	if detail != nil {
		for _, o := range detail.Options {
			if o != nil && labels[o.VariantID] == "" && o.SKUCode != "" {
				labels[o.VariantID] = strings.TrimSpace(o.ProductName + " · " + o.SKUCode)
			}
		}
	}
	ids := form.VariantIDs
	rows := make([]productdto.BundleOptionRow, 0, len(ids)+1)
	hasBlank := false
	for i, rawID := range ids {
		id := strings.TrimSpace(rawID)
		if id == "" {
			hasBlank = true
		}
		option := productdto.BundleOption{
			VariantID: id, Required: bundleAtoiAt(form.Required, i) == 1,
			DefaultQty: bundleAtoiAt(form.DefaultQty, i), MinQty: bundleAtoiAt(form.MinQty, i),
			MaxQty:     bundleAtoiAt(form.MaxQty, i),
			SourceKind: bundleStrAt(form.SourceKinds, i), WarehouseID: bundleStrAt(form.WarehouseIDs, i),
			WarehouseSKU: bundleStrAt(form.WarehouseSKUs, i), ExternalSKU: bundleStrAt(form.ExternalSKUs, i),
		}
		label := labels[id]
		if label == "" && id != "" {
			// 库与候选都给不出标签时退回 id：让运营至少看得出这一行是哪一条。
			label = id
		}
		rows = append(rows, bundleRow(option, 0, BundleSourceParts(option), label))
	}
	if len(rows) < productdto.BundleMaxOptionsLimit && !hasBlank {
		rows = append(rows, bundleRow(productdto.BundleOption{Required: true, DefaultQty: 1, MinQty: 1}, 0, nil, ""))
	}
	return rows
}

// bundleStrAt 取并行数组的第 i 个字符串（缺失一律空串）。
//
// 这里不承担校验 —— 来源字段的白名单与清理在保存路径上（未知来源会被明确拒绝，
// 不是在这里静默丢弃）。与控制器侧的同名助手是同一手法。
func bundleStrAt(values []string, i int) string {
	if i < 0 || i >= len(values) {
		return ""
	}
	return strings.TrimSpace(values[i])
}

// bundleAtoiAt 取并行数组的第 i 个整数（缺失 / 非数字一律 0）。
func bundleAtoiAt(values []string, i int) int {
	if i < 0 || i >= len(values) {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(values[i]))
	if err != nil {
		return 0
	}
	return n
}

// bundleCandidateOptions 候选 SKU 下拉项。
func bundleCandidateOptions(skus []*productdto.BundleSKUResp) []productdto.BundleCandidateView {
	options := make([]productdto.BundleCandidateView, 0, len(skus))
	for _, s := range skus {
		if s != nil {
			options = append(options, productdto.BundleCandidateView{ID: s.VariantID, Label: s.ProductName + " · " + s.SKUCode})
		}
	}
	return options
}

// bundleSourceOptions 来源下拉的三项（值即 enums 常量 —— 服务端按它选解析分支）。
//
// 槽位收事实（Key + 中文兜底），取词在模板：控制器提前翻等于把这三句锁死成中文。
// 事实本身来自 productenums.BundleSourceLabel —— 与成员清单的「来源」列**同一份映射**
// （此前下拉自带一句更长的兜底「从商品导入（该商品的启用变体）」，与库内词条不是同一句）。
func bundleSourceOptions(selected string) []map[string]any {
	values := []string{
		productenums.BundleSourceProduct,
		productenums.BundleSourceWarehouse,
		productenums.BundleSourceAttributes,
	}
	out := make([]map[string]any, 0, len(values))
	for _, value := range values {
		key, fallback := productenums.BundleSourceLabel(value)
		out = append(out, map[string]any{
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
// 形状直接复用 productPickOptions：「— 不选 —」空项给词条（事实）、商品项给数据名且
// **不给空的 LabelKey 键**（模板判据是 isset，空串也会走取词分支渲染成空白）。
// 这段形状只有一处实现 —— 两个构造点各写一遍时，改了一处不会报错，只会让其中一个下拉
// 渲染出一排空白 option。
func bundleSourceProductOptions(products []*productdto.ProductResp, selected string) []map[string]any {
	data := make([]map[string]any, 0, len(products))
	for _, p := range products {
		if p == nil {
			continue
		}
		data = append(data, map[string]any{"ID": p.ID, "Label": p.Name})
	}
	return productPickOptions(productenums.ProductBundleOptionNone, "— 不选 —", selected, data)
}

// bundleSourceWarehouseGroups 来源仓下拉 + 各仓的仓库 SKU 勾选清单。
//
// 复用「从仓库选」的既有投影：每个仓一屏候选，选中即代表「这条货在这个仓」——
// 服务端解析时仍会回该仓复核（前端只是线索）。
func bundleSourceWarehouseGroups(groups []productdto.WarehouseSKUGroupView, selectedWarehouseID string) []productdto.BundleSourceWarehouseGroupView {
	out := make([]productdto.BundleSourceWarehouseGroupView, 0, len(groups))
	for _, g := range groups {
		out = append(out, productdto.BundleSourceWarehouseGroupView{
			WarehouseID: g.WarehouseID,
			Label:       g.Label,
			IsDefault:   g.IsDefault,
			Selected:    g.WarehouseID != "" && g.WarehouseID == selectedWarehouseID,
			Items:       g.Items,
		})
	}
	return out
}
