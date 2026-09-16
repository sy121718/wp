// variant_availability.go — 按变体 id 查可用量（issue #24，商品详情实时库存）。
//
// 这是 VariantAvailabilityPort 面向访问面的包装：调用方给出**工程作用域**（访问面片段从
// URL 参数取 projectId，与 productList 片段同一口径；购物车从请求带的工程取），本方法在该
// 工程的作用域内查库存真源 —— 不属于该工程的变体由策略天然查不到，不需要「按变体反查工程」
// 那一步（反查要先读有策略的 products，正是死结所在）。
//
// 口径说明（与 productList 一致）：片段参数可被篡改，但商品与可用量本就是公开数据，
// 越权面仅限「读到别的工程同样公开的库存数字」；真正的把关在结算写路径。
//
// 三条刻意的口径：
//
//	· **只读真源**：可用量经 s.availability（inventory 实现）读 inventory_stocks，
//	  绝不读 product_variants.stock_total 缓存（那是展示缓存，滞后且可被改；spec 死线）；
//	· **尽力而为**：端口未注入（纯商品单测 / inventory 未装配）时返回空结果而不是报错 ——
//	  调用方据此渲染「以结算时库存为准」，而不是把一个读不到库存的页面变成 500；
//	  （写路径上的加购校验另有 fail-closed 的判定，不靠这里兜底）
//	· **限量**：一次最多查 VariantAvailabilityLookupMaxIDs 个变体，超出即截断（GET 片段不报错）。
package productservice

import (
	"context"
	"strings"
)

// VariantAvailabilityLookupMaxIDs 单次查询上界（片段参数长度与查询 IN 列表的双重保护）。
const VariantAvailabilityLookupMaxIDs = 100

// VariantAvailabilities 实现 productcontract.VariantAvailabilityLookupPort。
func (s *Service) VariantAvailabilities(ctx context.Context, projectID string, variantIDs []string) (out map[string]int, err error) {
	out = map[string]int{}
	ids := normalizeVariantIDs(variantIDs)
	if len(ids) == 0 {
		return out, nil
	}
	if s.availability == nil {
		return out, nil // 降级：调用方渲染「以结算时库存为准」
	}
	// 工程作用域由调用方给定；工程号非法（空串 / 非 uuid）时返回空结果而不是报错 ——
	// 本端口整体是「尽力而为」语义（读不到库存不该把页面变成 500）。
	pid := strings.TrimSpace(projectID)
	if pid == "" || !isUUID(pid) {
		return out, nil
	}
	// 该工程的可用量一次查回：不属于本工程的变体由 inventory_stocks 的策略挡在外面，
	// 它们不会出现在结果里，调用方按「未知」渲染兜底文案。
	avail, aerr := s.availability.AvailableQuantities(ctx, pid, ids)
	if aerr != nil {
		return nil, aerr
	}
	for id, n := range avail {
		if n < 0 {
			n = 0
		}
		out[id] = n
	}
	return out, nil
}

// normalizeVariantIDs 去空、去重、校验形状、截断；顺序保持首次出现（片段输出顺序可预测）。
//
// 形状校验是必需的，不是防御性洁癖：片段参数来自 URL（任何人都能写 variantIds=abc），
// 而非 uuid 的字符串带进 `WHERE id IN ?` 会让 PostgreSQL 直接报
// `invalid input syntax for type uuid`（SQLSTATE 22P02）—— 页面变成 500。
// 非法 id 在这里被丢弃，调用方按「未知」渲染兜底文案（与查不到同一条路）。
func normalizeVariantIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] || !isUUID(id) {
			continue
		}
		seen[id] = true
		out = append(out, id)
		if len(out) >= VariantAvailabilityLookupMaxIDs {
			break
		}
	}
	return out
}
