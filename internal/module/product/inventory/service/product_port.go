// product_port.go — 实现 product 契约定义的变体库存端口（依赖方向 inventory → product）。
//
// 商品模块建变体时需要两件本模块才知道的事：
//  1. 归属仓的短码 —— SKU 编码形如 {仓短码}_{商品码}_{序号}，未指定仓时用默认仓；
//  2. 在归属仓生成一条初始 0 的库存记录。
//
// 商品模块只依赖本端口（端口定义在 product/contract），实现留在这里 ——
// 商品模块不认识仓库模块，装配期由顶层把本服务注入。
package inventoryservice

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	productcontract "go_wp/internal/module/product/contract"
	inventorycontract "go_wp/internal/module/product/inventory/contract"
	inventoryenums "go_wp/internal/module/product/inventory/enums"
)

var _ inventorycontract.ProductStockPort = (*Service)(nil)

// NormalizeExternalSKU applies the inventory domain's canonical validation.
func (s *Service) NormalizeExternalSKU(raw string) (code string, err error) {
	return NormalizeExternalSKU(raw)
}

// ResolveWarehouse 解析归属仓（warehouseID 为空 → 该工程的默认仓），返回只读引用。
func (s *Service) ResolveWarehouse(ctx context.Context, projectID, warehouseID string) (ref *productcontract.WarehouseRef, err error) {
	wh, err := s.resolveWarehouse(ctx, projectID, warehouseID)
	if err != nil {
		return nil, err
	}
	return &productcontract.WarehouseRef{
		ID: wh.ID, ProjectID: wh.ProjectID, Code: wh.Code, Name: wh.Name,
	}, nil
}

// EnsureVariantStock 在归属仓为该 SKU 生成库存记录（已存在则复用）。
//
// 签名与 quantity 语义（2026-09-19 商品侧冻结，照此实现）：
//
//	· quantity == nil → 新建行 track_quantity = false（**不跟踪 = 无限**，新建行默认口径）；
//	· quantity != nil → 新建行 track_quantity = true 并写入该数量。0 是合法值
//	（= 明确没货），与 nil 严格区分 —— 这正是 CHECK (track_quantity OR quantity = 0)
//	要表达的事：不跟踪的行不允许带数字。
//
// skuCode 的语义是**仓库侧裸码**（如 DRAWERSMOKE_001）：仓库里的 SKU 永远不带仓码前缀，
// 前缀只出现在商品侧（SZ_DRAWERSMOKE_001，标注归属 / 认领仓）。**本层不剥前缀、也不加前缀**，
// 剥前缀是商品侧的职责；唯一例外是存量迁移 262 会把历史数据里多余的仓码前缀剥掉一次。
//
// 幂等：已存在的那一行原样返回，quantity / track_quantity / external_sku 都不被覆盖
// （覆盖式修改走 UpdateStockTracking / BindExternalSKU 这两条显式入口）。
func (s *Service) EnsureVariantStock(ctx context.Context, ref *productcontract.WarehouseRef, productID, variantID, skuCode string, quantity *int) (err error) {
	// 非 Tx 版本只负责**事务边界**：自己开一个事务并委托给 Tx 版本，
	// 两者语义逐字一致（不会各写一份判定）。
	return s.m.Transaction(ctx, func(tx *gorm.DB) error {
		return s.EnsureVariantStockTx(ctx, tx, ref, productID, variantID, skuCode, quantity)
	})
}

// EnsureVariantStockTx 在**调用方的事务**里为归属仓生成库存记录（幂等）。
//
// 为什么需要 Tx 变体（2026-09-19 商品域要求）：商品保存要把「商品 + 变体 + 各仓库存行
// + 变更记录」放进同一个事务，任何一步失败整体回滚。非 Tx 版本每次自己开事务，
// 商品回滚时库存行已经提交 —— 正是用户说的「很容易翻车」。
//
// 三条约束：
//   - 本方法**不再自己开事务**（调用方已经开着）；
//   - 工程作用域设在传入的 tx 上（rls.ScopeTx），**错误原样返回绝不吞掉**；
//   - 幂等建行走 ON CONFLICT (variant_id, warehouse_id) DO NOTHING：并发命中唯一键时
//     不报错，然后在同一事务内回读 —— 一句报错的 INSERT 会把整个事务标记为 aborted，
//     调用方后续的写入会全部失败。
//
// quantity / skuCode 的语义与非 Tx 版本完全一致（见 EnsureVariantStock）。
func (s *Service) EnsureVariantStockTx(ctx context.Context, tx *gorm.DB, ref *productcontract.WarehouseRef, productID, variantID, skuCode string, quantity *int) (err error) {
	if ref == nil || strings.TrimSpace(ref.ID) == "" {
		return errors.New(inventoryenums.ErrStockWarehouseNeeded)
	}
	if strings.TrimSpace(variantID) == "" {
		return errors.New(inventoryenums.ErrStockVariantRequired)
	}
	_, err = s.ensureStockRowTx(ctx, tx, ref.ProjectID, ref.ID, productID, variantID, skuCode, quantity)
	return err
}

// EnsureVariantStockWithExternal 在归属仓为该 SKU 生成库存行，并写上该仓的外部编码（迁移 251）。
//
// 「创建商品即入库并带上外码」走这一条（docs/14 §9.3 的两条来路都只改映射，不动属性真源）。
// 三点语义：
//
//	· 外码只作用于**新建**那一行：已存在的库存行沿用既有值（覆盖式改外码走 BindExternalSKU
//	  这条显式入口）—— 免得「再 ensure 一次」把运营登记过的对方编码悄悄清掉；
//	· externalSKU 为空串是合法的：该仓用我们自己的 SKU（自营仓的常态）；
//	· 归一 / 校验共用一个入口（NormalizeExternalSKU），与库存侧的写入口径只有一份规则。
func (s *Service) EnsureVariantStockWithExternal(ctx context.Context, ref *productcontract.WarehouseRef, productID, variantID, skuCode, externalSKU string, quantity *int) (err error) {
	// 同 EnsureVariantStock：非 Tx 版本只负责开事务，判定与写入都在 Tx 版本里。
	return s.m.Transaction(ctx, func(tx *gorm.DB) error {
		return s.EnsureVariantStockWithExternalTx(ctx, tx, ref, productID, variantID, skuCode, externalSKU, quantity)
	})
}

// EnsureVariantStockWithExternalTx 在**调用方的事务**里建库存行并写上该仓的外部编码。
//
// 与非 Tx 版本的差别只有事务边界（见 EnsureVariantStockTx 的三条约束）；
// 外码的归一与校验（NormalizeExternalSKU）在这里先做，失败时不碰数据库。
func (s *Service) EnsureVariantStockWithExternalTx(ctx context.Context, tx *gorm.DB, ref *productcontract.WarehouseRef, productID, variantID, skuCode, externalSKU string, quantity *int) (err error) {
	if ref == nil || strings.TrimSpace(ref.ID) == "" {
		return errors.New(inventoryenums.ErrStockWarehouseNeeded)
	}
	if strings.TrimSpace(variantID) == "" {
		return errors.New(inventoryenums.ErrStockVariantRequired)
	}
	code, err := NormalizeExternalSKU(externalSKU)
	if err != nil {
		return err
	}
	_, err = s.ensureStockRowWithExternalTx(ctx, tx, ref.ProjectID, ref.ID, productID, variantID, skuCode, code, quantity)
	return err
}

// AvailableQuantities 批量读 SKU 的可用量（product 契约的 VariantAvailabilityPort，issue #20）。
//
// 读的是 inventory_stocks **真源**、跨仓求和，且与「缓存该被同步成什么值」用的是同一条
// 汇总口径（model.StockTotals）—— 套餐里显示能买几件，与商品侧缓存里写了几件，
// 不会出现两套算法各自算出不同数字。
//
// 无库存记录的变体不出现在返回值里（调用方按 0 兜底），filter 跨工程由 projectID 限定。
func (s *Service) AvailableQuantities(ctx context.Context, projectID string, variantIDs []string) (out map[string]int, err error) {
	out = make(map[string]int, len(variantIDs))
	if len(variantIDs) == 0 {
		return out, nil
	}
	rows, err := s.m.StockTotals(ctx, projectID, variantIDs)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.VariantID] = r.Total
	}
	return out, nil
}
