// Package purchasecontract purchase 模块对外契约（货源 / 采购单 / 入库）。
package purchasecontract

import (
	"context"

	"gorm.io/gorm"
)

// WarehouseRef 仓库的只读引用（本模块自有形状，不借用库存 dto —— 跨模块只经契约）。
type WarehouseRef struct {
	ID        string
	ProjectID string
	Code      string
	Name      string
}

// StockLine 一条库存变动行（数量 + 可选的本次成本价）。
type StockLine struct {
	WarehouseID string
	ProductID   string
	VariantID   string
	SKUCode     string
	Quantity    int
	// CostPrice 非 nil 即把该值写成 (仓库, SKU) 的当前成本价（采购到货价 / 生产手工成本）。
	CostPrice *float64
}

// StockChange 一次库存变动（入库方向）。
//
// 形状照抄库存侧的变动入参但**不 import 它的 dto**：本模块只声明自己要用到的字段，
// 由库存侧适配器（inventory/outbound/purchasestock）翻译成库存的 ChangeStockReq。
type StockChange struct {
	ProjectID  string
	Direction  string
	ReasonCode string
	SourceType string
	SourceRef  string
	Remark     string
	OperatorID string
	Lines      []StockLine
}

// WarehouseOption 收货仓下拉的一项（本模块自有形状：模板要「名称（编码）· 默认仓」）。
type WarehouseOption struct {
	ID        string
	Code      string
	Name      string
	IsDefault bool
}

// VariantOption 采购行 SKU 下拉的一项。
//
// BareSKU 是**仓库侧裸码**（inventory_stocks.sku_code）：采购行的 sku_code 必须按裸码落库，
// 仓库里的 SKU 永远不带仓码前缀。没有库存行的变体退回商品侧编码，由服务端在入库入口归一。
type VariantOption struct {
	VariantID   string
	ProductID   string
	ProductName string
	SKUCode     string
	BareSKU     string
	CostPrice   *float64
}

// VariantCatalog 采购页下拉需要的两个只读能力（商品侧变体清单 + 库存侧裸码映射）。
//
// 与 StockPort 分开：这两条只服务后台页的候选列表，业务路径（建单 / 收货）一条都不用。
// 装配期后置注入（商品模块反过来要库存契约，固定顺序装不出来），未注入时下拉为空。
type VariantCatalog interface {
	ListAllVariantOptions(ctx context.Context, projectID string) ([]VariantOption, error)
	BareSKUsByVariant(ctx context.Context, projectID string) (map[string]string, error)
}

// StockPort 采购要用的库存能力（全部由 inventory 模块实现，适配器在库存侧）。
//
// 为什么是这么窄的三个方法：采购只关心「收货把货放进哪个仓、仓的短码叫什么、
// 一次入库变动落在哪个批次」。仓库增删、流水查询、调拨都在库存模块自己的页面里。
type StockPort interface {
	// ChangeStockTx 在**调用方事务内**做一次库存变动，返回本次批次号。
	//
	// 返回批次号而不是只回 error：入库单要把批次号写进自己那一行（追溯「这批货
	// 动的哪笔流水」），而提交后回读会让「单据已提交、流水查不到」有一个窗口。
	ChangeStockTx(ctx context.Context, tx *gorm.DB, req *StockChange) (batchID string, err error)
	// ResolveWarehouse 解析收货仓：显式给 id 需同工程且启用；为空兜底工程默认仓。
	ResolveWarehouse(ctx context.Context, projectID, warehouseID string) (ref *WarehouseRef, err error)
	// WarehouseNames 工程内「仓库 id → 名称」的一次性映射（列表补名，不产生 N+1）。
	WarehouseNames(ctx context.Context, projectID string) (map[string]string, error)
	// Warehouses 工程内的仓库清单（收货仓下拉：要短码与「是否默认仓」）。
	Warehouses(ctx context.Context, projectID string) ([]WarehouseOption, error)
}
