package dashboardhttp

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	inventorydto "go_wp/internal/module/product/inventory/dto"
	"go_wp/pkg/money"
)

// product_view.go - 商品页的视图构造（变体行、规格标签、金额/评分格式化与仓库下拉）。

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
func ratingOf(svc productcontract.ProductService, ctx context.Context, productID string) *productdto.RatingResp {
	res, err := svc.ListRatings(ctx, &productdto.ListRatingsReq{ProductID: productID})
	if err != nil || res == nil {
		return &productdto.RatingResp{}
	}
	return res
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
