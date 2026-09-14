// variant_availability.go — 商品变体可用量片段（issue #24）。
//
// 一个 capability：productVariantAvailability GET anonymous —— 按变体 id 批量返回可用量结论
// （库存充足 / 仅剩 N 件 / 暂时缺货 / 以结算时库存为准）。
//
// 为什么必须走片段：商品详情的规格选择器是**已编译的静态产物**，而可用量在库存真源里
// 每秒都可能变（docs/04 §1.1）；把可用量烘进产物等于发布一份过期库存。
//
// 参数只有变体 id（逗号分隔，≤100）：工程上下文由 product 模块按变体反查补齐，片段层不认识
// 商品表结构，也不伪造工程 id。provider 未注入时降级为「以结算时库存为准」而不是报错 ——
// 静态页面上一个读不到库存的规格选择器仍应可读可用，真正的把关在结算写路径。
package runtimefragment

import (
	"context"
	"fmt"
	"strings"

	productcontract "go_wp/internal/module/product/contract"
	"go_wp/internal/templates"
)

// AvailabilityLowStockThreshold 低库存阈值（<= 这个数就提示「仅剩 N 件」）。
const AvailabilityLowStockThreshold = 5

// variantAvailabilityProvider 可用量查询依赖（装配期注入；nil = 未接入，走降级文案）。
var variantAvailabilityProvider productcontract.VariantAvailabilityLookupPort

// SetVariantAvailabilityProvider 注入可用量查询能力（装配期调用；传 nil 表示未接入）。
func SetVariantAvailabilityProvider(port productcontract.VariantAvailabilityLookupPort) {
	variantAvailabilityProvider = port
}

func init() {
	Register(Spec{
		Type:   "productVariantAvailability",
		Method: "GET",
		Auth:   AuthAnonymous,
		Render: renderVariantAvailability,
	})
}

// 参数名与上限。
const (
	variantAvailabilityParam  = "variantIds"
	variantAvailabilityMaxIDs = 100
)

// variantAvailabilityItem 单个变体的结论。
type variantAvailabilityItem struct {
	VariantID string
	// Available 可用量（Known 为假时无意义）。
	Available int
	// Known 是否查到了这个变体（已删除 / 跨工程查不到的为假）。
	Known bool
	// InStock 是否可购买。
	InStock bool
	// Message 面向顾客的中文结论。
	Message string
}

// variantAvailabilityView 片段模板数据。
type variantAvailabilityView struct {
	Items []variantAvailabilityItem
}

// renderVariantAvailability 渲染可用量片段。
func renderVariantAvailability(ctx context.Context, r *Request) (string, error) {
	if r == nil {
		return "", fmt.Errorf("片段请求为空")
	}
	ids := splitVariantIDs(r.Params[variantAvailabilityParam])
	if len(ids) == 0 {
		return "", fmt.Errorf("缺少参数 %s（逗号分隔的变体 id）", variantAvailabilityParam)
	}
	avail := map[string]int{}
	if variantAvailabilityProvider != nil {
		got, err := variantAvailabilityProvider.VariantAvailabilities(ctx, ids)
		if err != nil {
			return "", err
		}
		if got != nil {
			avail = got
		}
	}
	view := variantAvailabilityView{Items: make([]variantAvailabilityItem, 0, len(ids))}
	for _, id := range ids {
		n, known := avail[id]
		view.Items = append(view.Items, variantAvailabilityItem{
			VariantID: id,
			Available: n,
			Known:     known,
			InStock:   known && n > 0,
			Message:   availabilityMessageOf(r, known, n),
		})
	}
	return templates.RenderFragment("variant_availability", view)
}

// splitVariantIDs 解析逗号分隔的变体 id：去空、去重、限量（顺序保持首次出现）。
func splitVariantIDs(raw string) []string {
	out := make([]string, 0, 4)
	seen := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		id := strings.TrimSpace(part)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
		if len(out) >= variantAvailabilityMaxIDs {
			break
		}
	}
	return out
}
