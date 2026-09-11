// product_stock_cache.go — 商品侧库存缓存端口实现（issue #16）。
//
// 依赖方向：inventory → product。库存模块发起变动后（**事务已提交**）经本端口
// 把真源汇总写进 product_variants.stock_total / stock_synced_at。
//
// 商品模块只是缓存的**所有者**（表在自己这边，跨模块写只能走自己的入口），
// 不参与任何可用量判断：stock_total 永远只是后台列表的展示值。
package productservice

import (
	"context"
	"errors"
	"strings"
	"time"

	productenums "go_wp/internal/module/product/enums"
)

// SyncVariantStockTotal 写变体库存缓存并盖上同步时间戳（只动这两列）。
func (s *Service) SyncVariantStockTotal(ctx context.Context, variantID string, total int) (err error) {
	if strings.TrimSpace(variantID) == "" {
		return errors.New(productenums.ErrInvalidParam)
	}
	return s.m.SyncVariantStockTotal(ctx, variantID, total, time.Now().UTC())
}

// ListVariantStockTotals 批量读变体库存缓存值（库存模块对账用）。
func (s *Service) ListVariantStockTotals(ctx context.Context, variantIDs []string) (totals map[string]int, err error) {
	return s.m.ListVariantStockTotals(ctx, variantIDs)
}
