// inventory_cache.go — 商品侧库存缓存的同步与对账（issue #16 验收 6/7）。
//
// 商品侧 product_variants.stock_total / stock_synced_at 只是**展示值**：
//
//	· 它永远不参与任何可用量判断（判定一律走 inventory_stocks 的行锁）；
//	· 同步发生在库存事务**提交之后**（跨模块写不塞进同一个事务）：
//	  缓存写失败不回滚真源、不让主流程报错，而是落台账 + 由对账兜底；
//	· 对账拿真源汇总与缓存值逐变体比对，可选按真源修复。
//
// 真源汇总的口径是「同一变体在各仓的 quantity 求和」—— 库存维度是 SKU × 仓库，
// 商品侧的缓存是 SKU 级的总数，两者是不同粒度，汇总是一对多的投影。
package inventoryservice

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	inventorymodel "go_wp/internal/module/inventory/model"
)

// SyncStockCache 显式同步商品侧库存缓存（可指定单个变体，缺省整工程）。
//
// 与变动后的自动同步共用同一实现：口径只有一个 —— 真源汇总。
func (s *Service) SyncStockCache(ctx context.Context, req *inventorydto.SyncStockCacheReq) (res *inventorydto.SyncStockCacheResp, err error) {
	if req == nil {
		return nil, errors.New(inventoryenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	variantID := strings.TrimSpace(req.VariantID)
	var variantIDs []string
	if variantID != "" {
		variantIDs = []string{variantID}
	}
	rows, err := s.m.StockTotals(ctx, projectID, variantIDs)
	if err != nil {
		return nil, err
	}
	res = &inventorydto.SyncStockCacheResp{
		ProjectID: projectID,
		Items:     make([]*inventorydto.CacheSyncItemResp, 0, len(rows)),
	}
	for _, row := range rows {
		item, failure := s.applyCacheSync(ctx, projectID, row.VariantID, row.SKUCode, row.Total)
		res.Items = append(res.Items, item)
		if failure != "" {
			res.Failed++
			continue
		}
		res.Synced++
	}
	return res, nil
}

// ReconcileStockCache 缓存对账（验收 6：对账兜底；Repair 为真时按真源修复）。
func (s *Service) ReconcileStockCache(ctx context.Context, req *inventorydto.ReconcileStockCacheReq) (res *inventorydto.ReconcileStockCacheResp, err error) {
	if req == nil {
		return nil, errors.New(inventoryenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	variantID := strings.TrimSpace(req.VariantID)
	var variantIDs []string
	if variantID != "" {
		variantIDs = []string{variantID}
	}
	rows, err := s.m.StockTotals(ctx, projectID, variantIDs)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.VariantID)
	}
	cached, err := s.readCachedTotals(ctx, ids)
	if err != nil {
		return nil, err
	}
	res = &inventorydto.ReconcileStockCacheResp{
		ProjectID: projectID,
		Items:     make([]*inventorydto.ReconcileItemResp, 0, len(rows)),
	}
	for _, row := range rows {
		item := &inventorydto.ReconcileItemResp{
			VariantID: row.VariantID, SKUCode: row.SKUCode,
			TrueTotal: row.Total, CachedTotal: cached[row.VariantID],
		}
		item.Differed = item.TrueTotal != item.CachedTotal
		res.Total++
		if !item.Differed {
			res.Matched++
			res.Items = append(res.Items, item)
			continue
		}
		res.Differed++
		if req.Repair {
			if _, failure := s.applyCacheSync(ctx, projectID, row.VariantID, row.SKUCode, row.Total); failure != "" {
				item.RepairError = failure
			} else {
				item.Repaired = true
				res.Repaired++
			}
		}
		res.Items = append(res.Items, item)
	}
	return res, nil
}

// syncStockCache 变动提交之后的缓存同步（best-effort）。
//
// 返回「变体 → 已同步的真源汇总」与失败原因清单。**不返回 error**：
// 真源变动已经落库，缓存写失败只影响展示，主流程必须成功。
func (s *Service) syncStockCache(ctx context.Context, projectID string, variants map[string]string) (totals map[string]int, failures []string) {
	totals = make(map[string]int, len(variants))
	failures = make([]string, 0)
	if len(variants) == 0 {
		return totals, failures
	}
	ids := make([]string, 0, len(variants))
	for id := range variants {
		ids = append(ids, id)
	}
	rows, err := s.m.StockTotals(ctx, projectID, ids)
	if err != nil {
		failures = append(failures, err.Error())
		return totals, failures
	}
	for _, row := range rows {
		totals[row.VariantID] = row.Total
		if _, failure := s.applyCacheSync(ctx, projectID, row.VariantID, row.SKUCode, row.Total); failure != "" {
			failures = append(failures, failure)
		}
	}
	return totals, failures
}

// applyCacheSync 把某个变体的真源汇总写进商品侧缓存，并记下台账（含时间戳）。
//
// 失败时保持台账里上一次成功写入的值，status 置 failed 并记录原因 ——
// 这就是「同步失败不影响主流程 + 有对账兜底」里的兜底凭据。
func (s *Service) applyCacheSync(ctx context.Context, projectID, variantID, skuCode string, total int) (item *inventorydto.CacheSyncItemResp, failure string) {
	now := time.Now().UTC()
	previous := 0
	if row, gerr := s.m.GetCacheSync(ctx, variantID); gerr == nil {
		previous = row.CachedTotal
	}
	item = &inventorydto.CacheSyncItemResp{
		VariantID: variantID, SKUCode: skuCode, TrueTotal: total,
		CachedTotal: total, SyncedAt: now.Format(time.RFC3339), Status: inventoryenums.CacheSyncOK,
	}
	record := &inventorymodel.CacheSyncEntity{
		ID: uuid.NewString(), ProjectID: projectID, VariantID: variantID, SKUCode: skuCode,
		TrueTotal: total, CachedTotal: total, Status: inventoryenums.CacheSyncOK,
		SyncedAt: now, UpdatedAt: now,
	}
	switch {
	case s.stockCache == nil:
		failure = inventoryenums.ErrStockCachePortMissing
	default:
		if err := s.stockCache.SyncVariantStockTotal(ctx, variantID, total); err != nil {
			failure = err.Error()
		}
	}
	if failure != "" {
		item.Status = inventoryenums.CacheSyncFailed
		item.Error = failure
		item.CachedTotal = previous
		record.Status = inventoryenums.CacheSyncFailed
		record.Error = failure
		record.CachedTotal = previous
	}
	// 台账写失败不影响主流程（真源与缓存都已处理），静默丢弃。
	_ = s.m.UpsertCacheSync(ctx, record)
	if failure != "" {
		failure = variantID + ": " + failure
	}
	return item, failure
}

// readCachedTotals 批量读商品侧缓存值（缺端口时是装配缺陷，必须显式暴露）。
func (s *Service) readCachedTotals(ctx context.Context, variantIDs []string) (totals map[string]int, err error) {
	totals = make(map[string]int, len(variantIDs))
	if len(variantIDs) == 0 {
		return totals, nil
	}
	if s.stockCache == nil {
		return nil, errors.New(inventoryenums.ErrStockCachePortMissing)
	}
	rows, err := s.stockCache.ListVariantStockTotals(ctx, variantIDs)
	if err != nil {
		return nil, err
	}
	for id, total := range rows {
		totals[id] = total
	}
	return totals, nil
}
