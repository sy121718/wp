// variant_availability.go — 按变体 id 查可用量（issue #24，商品详情实时库存）。
//
// 这是 VariantAvailabilityPort 面向访问面的包装：片段端点手里只有变体 id（静态产物里烘的
// data 属性），没有工程上下文，工程在这里按变体反查补齐。
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
	"sort"
	"strings"
)

// VariantAvailabilityLookupMaxIDs 单次查询上界（片段参数长度与查询 IN 列表的双重保护）。
const VariantAvailabilityLookupMaxIDs = 100

// VariantAvailabilities 实现 productcontract.VariantAvailabilityLookupPort。
func (s *Service) VariantAvailabilities(ctx context.Context, variantIDs []string) (out map[string]int, err error) {
	out = map[string]int{}
	ids := normalizeVariantIDs(variantIDs)
	if len(ids) == 0 {
		return out, nil
	}
	if s.availability == nil {
		return out, nil // 降级：调用方渲染「以结算时库存为准」
	}
	projects, perr := s.m.VariantProjectIDs(ctx, ids)
	if perr != nil {
		return nil, perr
	}
	// 按工程分组：同一批变体可能跨工程（片段参数不受商品约束），可用量必须逐工程查。
	byProject := map[string][]string{}
	for _, id := range ids {
		pid, ok := projects[id]
		if !ok || pid == "" {
			continue // 变体已删除：不出现在结果里（调用方按「未知」处理）
		}
		byProject[pid] = append(byProject[pid], id)
	}
	// 工程键排序后逐个查：并发查同一批数据没有收益，而在确定性上要付代价。
	pids := make([]string, 0, len(byProject))
	for pid := range byProject {
		pids = append(pids, pid)
	}
	sort.Strings(pids)
	for _, pid := range pids {
		avail, aerr := s.availability.AvailableQuantities(ctx, pid, byProject[pid])
		if aerr != nil {
			return nil, aerr
		}
		for id, n := range avail {
			if n < 0 {
				n = 0
			}
			out[id] = n
		}
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
