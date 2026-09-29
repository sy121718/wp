package producthttp

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	"go_wp/internal/web/shell"
	"go_wp/pkg/money"
)

// product_view.go - 商品页的视图构造（变体行、规格标签、金额/评分格式化、仓库下拉
// 与捆绑构成面板）。

// variantListRows 变体「清单」表的初始行 —— 直接来自**库里已有的变体**。
//
// 关键语义（docs/14 §8「预览—保存」）：清单的种子是既有变体，**不是重算笛卡尔积** ——
// 重算会让上次删掉的组合自己冒出来。只有运营再点一次「生成组合」（显式动作）才会
// 把新组合追加进清单；保存之前库里一个字节都不变，所以行上的「移除」只是移出清单。
//
// OptionValues 是**规范化后的 JSON 文本**（逐行原样回传服务端做组合去重与跳过提示），
// 规格列的可读文本由服务端拼（specLabel），前端不做属性名解析。
func variantListRows(detail *productdto.ProductResp) []gin.H {
	rows := make([]gin.H, 0, len(detail.Variants))
	for _, v := range detail.Variants {
		rows = append(rows, gin.H{
			"ID": v.ID, "SKUCode": v.SKUCode, "Spec": specLabel(v.OptionValues, detail.Attributes),
			"OptionValues": optionValuesJSON(v.OptionValues),
			"Price":        formatAmount(v.Price),
			"ComparePrice": formatNullableAmount(v.ComparePrice),
			"CostPrice":    formatNullableAmount(v.CostPrice),
			"Enabled":      v.Enabled, "StockTotal": v.StockTotal,
		})
	}
	return rows
}

// optionValuesJSON 变体组合的规范化 JSON 文本（空 / null 归一到 {}）。
//
// 它同时是清单行的 data 属性值与预览去重的入参：组合为空的对象表示「无规格变体」
// （商品创建时的首个变体），归一成 {} 而不是空串，前端 JSON.parse 才不会炸。
func optionValuesJSON(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return "{}"
	}
	return s
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
//
// 唯一实现在 pkg/money.FormatYuan（审计 CQ-013：此前与商品构建期的 formatPrice
// 逐字节重复）。保留本名字是因为同包多个后台页面文件共用它。
func formatAmount(v float64) string { return money.FormatYuan(v) }

// formatNullableAmount 可空数值 → 展示文本（空显示为 —）。
func formatNullableAmount(v *float64) string {
	if v == nil {
		return "—"
	}
	return formatAmount(*v)
}

// formatScore 评分统一两位小数（4.5 → 4.50）。
//
// 与集合源给前台的 rating 字段同一口径：评分是 0~5 的小数，
// 用最短表示会得到 4.5 / 4.25 混排，两位小数更符合评分展示习惯。
func formatScore(v float64) string {
	return strconv.FormatFloat(v, 'f', 2, 64)
}

// ratingOf 读某商品的评分明细与投影值（读不到就按「没有评分」处理，页面照常渲染）。
//
// projectID 由调用方给出（DB-009）：product_ratings 有 FORCE 策略，明细查询要工程作用域。
// 兜底解析（resolveProjectID 取唯一工程）在多工程下会直接判参数错，所以后台页这里把
// 自己在用的那个工程显式传下去，不走兜底。
func ratingOf(svc productcontract.ProductService, ctx context.Context, projectID, productID string) *productdto.RatingResp {
	res, err := svc.ListRatings(ctx, &productdto.ListRatingsReq{ProductID: productID, ProjectID: projectID})
	if err != nil || res == nil {
		return &productdto.RatingResp{}
	}
	return res
}

// —— 捆绑构成（商品详情页按 type=bundle 渲染的编辑区）——

// bundlePanelReadFailedKey 捆绑配置读取失败的词条 key。
//
// 与捆绑配置页同一句：两处读的是同一份配置，失败原因也同一批（商品不存在 /
// 库存真源端口未接入，后者拿不到可用量）。复用词条而不是另起一句，
// 免得同一件事在两个页面上给出两种说法。
const bundlePanelReadFailedKey = "admin.product_bundle.detailFailed"

// bundlePanelReadFailedFallback 上述 key 的中文兜底（i18n 未初始化或词条缺失时用）。
const bundlePanelReadFailedFallback = "配置读取失败：商品不存在，或库存真源端口未接入（无法给出可用量）。"

// isBundleProduct 商品类型是否为捆绑容器（详情页据此决定要不要渲染「捆绑构成」）。
//
// 判据只认类型本身，不看「bundle_items 是否非空」——后者是迁移 238 之前的旧口径，
// 分不清「主体卖自己的 SKU」与「容器卖组合」（bundle 商品的价 0 变体就是这么来的）。
func isBundleProduct(productType string) bool {
	return productType == productmodel.TypeBundle
}

// bundleSourceKindLabel 成员来源 kind → 当前语言标签（空 = 历史配置 / 手工指定）。
func bundleSourceKindLabel(tr func(key, fallback string) string, kind string) string {
	switch kind {
	case productenums.BundleSourceProduct:
		return tr(productenums.ProductBundleSourceProduct, "从商品导入")
	case productenums.BundleSourceWarehouse:
		return tr(productenums.ProductBundleSourceWarehouse, "从仓库选")
	case productenums.BundleSourceAttributes:
		return tr(productenums.ProductBundleSourceAttributes, "自选属性组合")
	default:
		return tr(productenums.ProductBundleSourceNone, "手工指定")
	}
}

// bundleSourceLabel 成员来源快照的可读文本（仅用于展示 —— 来源不是身份）。
//
// 「从仓库选」再多带两个字段：仓库 SKU 与外部编码 —— 运营对账时手上可能是其中任意一个；
// 仓库 id 留在 JSON 里做溯源，不搬上表格（uuid 对读的人没有含义）。
func bundleSourceLabel(tr func(key, fallback string) string, o productdto.BundleOption) string {
	label := bundleSourceKindLabel(tr, strings.TrimSpace(o.SourceKind))
	parts := make([]string, 0, 2)
	if sku := strings.TrimSpace(o.WarehouseSKU); sku != "" {
		parts = append(parts, tr(productenums.ProductBundleSourceWarehouseSKULabel, "仓库 SKU")+" "+sku)
	}
	if ext := strings.TrimSpace(o.ExternalSKU); ext != "" {
		parts = append(parts, tr(productenums.ProductBundleSourceExternalSKULabel, "外部编码")+" "+ext)
	}
	if len(parts) == 0 {
		return label
	}
	return label + " · " + strings.Join(parts, " · ")
}

// bundlePanelRows 捆绑构成表的行：成员 SKU 的规则、可用量与后台价格口径。
//
// 成员挂牌价（ItemPrice）在这里只作**参考**：套餐金额只由容器价得出，成员价不参与
// 任何计价与前台展示（迁移 238 的类型不变量）；成员成本只进后台口径。
//
// SKU 已被删除时配置里只剩 variantId（SKUCode 为空），照常出一行并标注 ——
// 静默丢项会让人以为配置里本来就没有这一项，也就不会去修它。
func bundlePanelRows(tr func(key, fallback string) string, options []*productdto.BundleOptionDetail) []gin.H {
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
			// 来源快照（仅展示）：空 = 手工指定 / 历史配置。
			"SourceLabel": bundleSourceLabel(tr, o.BundleOption),
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
		panel["ErrorText"] = shell.TranslateFor(c)(bundlePanelReadFailedKey, bundlePanelReadFailedFallback)
		return panel
	}
	rows := bundlePanelRows(shell.TranslateFor(c), detail.Options)
	panel["Loaded"] = true
	panel["BasePrice"] = formatAmount(detail.BasePrice)
	panel["Options"] = rows
	panel["OptionCount"] = len(rows)
	return panel
}

// warehouseOptions 某工程的仓库下拉项（issue #15；默认仓在最前并标注）。
//
// 未注入仓库契约时返回空列表：模板此时不渲染下拉，变体创建按「不指定仓库」处理。
//
// 除展示用的 Label 外还带上 Code 与 Name：列表「库存」列的分仓明细要按**工程仓库清单**
// 逐仓显示三态（服务端只返回有库存行的仓，「这个仓没有这一行」= 未入库，是页面才知道的事实）。
func (h *productPageHandle) warehouseOptions(ctx context.Context, projectID string,
	tr func(key, fallback string) string) (out []gin.H, err error) {
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
			// 默认仓后缀是**跨模块共用的词条**：真源在库存模块（enums 与词条都只有一条，
			// 见 inventoryenums.InventoryChangeWarehouseDefaultSuffix）—— 两处显示必须同步改。
			label += " " + tr(inventoryenums.InventoryChangeWarehouseDefaultSuffix, "· 默认仓")
		}
		out = append(out, gin.H{
			"ID": w.ID, "Label": label, "Code": w.Code, "Name": w.Name,
			"IsDefault": w.IsDefault,
		})
	}
	return out, nil
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
func productStockCell(detail *productdto.ProductResp, warehouses []gin.H) gin.H {
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
		id, _ := w["ID"].(string)
		label, _ := w["Label"].(string)
		item := gin.H{"WarehouseID": id, "WarehouseLabel": label,
			"State": productenums.StockStateNone, "Quantity": 0}
		if row, ok := byWarehouse[id]; ok {
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
