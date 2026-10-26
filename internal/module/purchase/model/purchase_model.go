// purchase_model.go — purchase 模块的表访问单元入口。
//
// 本包的 Model 是「表访问单元（Repository）」的句柄：只做本模块表的 CRUD、
// 聚合内原子组合与只读投影；业务规则（状态推导、超收拒绝、幂等命中、成本价回写时机）
// 一律留在 service 层。
//
// 表隔离：本模块拥有五张表 —— inventory_purchase_orders / inventory_purchase_order_lines /
// inventory_purchase_receipts / inventory_purchase_receipt_items / inventory_sources。
// **表名保留 inventory_ 前缀**是历史命名（2026-10 采购独立成模块时未改）：迁移 196 / 206 / 626
// 的 plpgsql 函数体里写死了这些名字，改名要连带重写触发器与 RLS 名单，代价远大于命名收益。
// 命名属于谁由本文件的写入范围决定，不由前缀决定。
//
// 凡是本模块**没有**的表（例如仓库 inventory_warehouses）一律不在这里出现 —— 需要仓名时
// 由 service 经采购契约的库存端口取，不在 model 里跨模块 JOIN。
package purchasemodel

import (
	"context"

	"gorm.io/gorm"
)

// Model 采购域表访问单元（不持有业务状态）。
type Model struct {
	db *gorm.DB
}

// NewModel 构造。
func NewModel(db *gorm.DB) *Model { return &Model{db: db} }

// Transaction 事务边界由 service 决定，句柄在 model 侧开、由 service 向下传。
func (m *Model) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}
