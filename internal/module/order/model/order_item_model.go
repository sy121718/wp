package model

// order_item_model.go — 订单项的表访问单元。
//
// 订单项是**快照**：下单时刻的商品名 / 规格 / SKU / 单价 / 成本各存一份。
// product_id 与 variant_id 只作追溯（「这是哪件商品的单」），不参与展示与计价 ——
// 商品改名改价绝不能改写历史订单。

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// OrderItemEntity 对应 order_items 表。金额一律整数分。
type OrderItemEntity struct {
	ID           uint64 `gorm:"column:id;primaryKey"`
	OrderID      uint64 `gorm:"column:order_id"`
	ProductID    string `gorm:"column:product_id"`
	VariantID    string `gorm:"column:variant_id"`
	ProductName  string `gorm:"column:product_name"`
	VariantLabel string `gorm:"column:variant_label"`
	SKU          string `gorm:"column:sku"`
	UnitPrice    int64  `gorm:"column:unit_price"`
	Quantity     int    `gorm:"column:quantity"`
	LineSubtotal int64  `gorm:"column:line_subtotal"`
	LineDiscount int64  `gorm:"column:line_discount"`
	LineTax      int64  `gorm:"column:line_tax"`
	LineTotal    int64  `gorm:"column:line_total"`
	// CostPrice 是下单时刻的行成本快照（分，来自该行**归属仓**的当前成本，迁移 256）。
	//
	// **可空**：nil = 下单时该 (仓库, SKU) 尚未核算（cost_price IS NULL），不是 0 ——
	// 0 是合法的显式成本（赠品 / 内部划拨）。DDL 侧已同步去掉 NOT NULL 与 DEFAULT 0
	//（迁移 256），所以「没核算」不会再被悄悄记成零成本。
	CostPrice *int64 `gorm:"column:cost_price"`
	// WarehouseID 发货仓（迁移 625）：**下单时解析并固化**，预占与发货扣减必须落在同一个仓。
	//
	// **可空**：存量订单没有这个事实，回填到默认仓等于伪造历史。
	// NULL = 历史行，首次发货时解析并回写（此后这一行固化）。
	WarehouseID *string   `gorm:"column:warehouse_id"`
	CreateTime  time.Time `gorm:"column:create_time"`
}

// TableName 实现 gorm 表名（默认推断为 order_item_entities）。
func (OrderItemEntity) TableName() string { return "order_items" }

// OrderItemModel 订单项表访问单元。
type OrderItemModel struct{ db *gorm.DB }

func NewOrderItemModel(db *gorm.DB) *OrderItemModel { return &OrderItemModel{db: db} }

// DB 返回绑定本表的句柄（只允许本 model 的仓储方法消费）。
func (m *OrderItemModel) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&OrderItemEntity{})
}

// Transaction 透传事务。
func (m *OrderItemModel) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

// CreateBatchTx 事务内批量写订单项。
//
// 订单项与订单头必须同一个事务：留下「有头无项」的订单比整单失败更难收拾。
func (m *OrderItemModel) CreateBatchTx(ctx context.Context, tx *gorm.DB, items []*OrderItemEntity) (err error) {
	if len(items) == 0 {
		return nil
	}
	return tx.WithContext(ctx).Model(&OrderItemEntity{}).Create(&items).Error
}

// CreateBatch 批量写订单项（非事务）。
func (m *OrderItemModel) CreateBatch(ctx context.Context, items []*OrderItemEntity) (err error) {
	if len(items) == 0 {
		return nil
	}
	return m.DB(ctx).Create(&items).Error
}

// ListByOrderID 取某单的全部订单项（按 id 升序，保持录入顺序）。
func (m *OrderItemModel) ListByOrderID(ctx context.Context, orderID uint64) (list []*OrderItemEntity, err error) {
	err = m.DB(ctx).Where("order_id = ?", orderID).Order("id ASC").Find(&list).Error
	return list, err
}

// ListByOrderIDTx 事务内取某单的全部订单项（作用域设在调用方的事务上，另见 ListByOrderID）。
//
// 为什么要有 Tx 版：调用方已经在订单事务里（取消失败要整体回滚），用非 Tx 版读会走
// 另一条连接 —— 读到的是事务外的快照，甚至在没有工程作用域时静默 0 行。
func (m *OrderItemModel) ListByOrderIDTx(ctx context.Context, tx *gorm.DB, orderID uint64) (list []*OrderItemEntity, err error) {
	err = tx.WithContext(ctx).Model(&OrderItemEntity{}).
		Where("order_id = ?", orderID).Order("id ASC").Find(&list).Error
	return list, err
}

// CountByOrderID 订单项条数（校验「有头必有项」）。
func (m *OrderItemModel) CountByOrderID(ctx context.Context, orderID uint64) (count int64, err error) {
	err = m.DB(ctx).Where("order_id = ?", orderID).Count(&count).Error
	return count, err
}

// SetWarehouseIDIfNullTx 事务内把发货仓回写到订单行，**仅在那一列仍为 NULL 时**（迁移 625）。
//
// 条件写而不是无条件写：已固化的值优先 —— 并发两次发货可能各自解析到同一个仓，
// 只有一个 UPDATE 真正改行；若期间被别的路径写成另一个仓（不该发生，但一致性不该靠假设），
// 这里不覆盖既有事实。
func (m *OrderItemModel) SetWarehouseIDIfNullTx(ctx context.Context, tx *gorm.DB, id uint64, warehouseID string) error {
	return tx.WithContext(ctx).Model(&OrderItemEntity{}).
		Where("id = ? AND warehouse_id IS NULL", id).
		Update("warehouse_id", warehouseID).Error
}
