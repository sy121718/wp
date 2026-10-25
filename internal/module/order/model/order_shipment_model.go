package model

// order_shipment_model.go — 承运商 + 发货单 + 发货明细（迁移 624，S1）。
//
// 三张表都是**履约事实**：承运商是可选主数据（停用不删），发货单是「某次发货」的凭据，
// 发货明细是「这次发了哪几行的多少件」。
//
// 三条不变量（写在这里，而不是散落在 service 各处的注释里）：
//
//  1. 发货明细挂在**订单行**上（order_item_id），不是挂在变体上 —— 订单行已经是
//     「哪件商品、多少钱、多少件」的快照，明细只需要回答「这一次发了它多少件」；
//  2. 已发数量的真源是**明细的求和**（SumShippedQuantityTx），不是订单行上的一个缓存列：
//     缓存列会在「删一张发货单」这类操作后静默失准，而求和永远不会；
//  3. 承运商停用（status = disabled）而不是删除 —— 历史发货单上的 carrier_id 还指着它，
//     删掉会让「这单是哪家送的」变成一条悬空外键。

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// OrderCarrierEntity 承运商（迁移 624）。
type OrderCarrierEntity struct {
	ID        string `gorm:"column:id;primaryKey"`
	ProjectID string `gorm:"column:project_id;not null"`
	Code      string `gorm:"column:code;not null"`
	Name      string `gorm:"column:name;not null"`
	// TrackingURLTemplate 跟踪页模板，唯一允许的占位符是 {tracking_no}。
	// 空串 = 没有公开跟踪页（本地自营配送常见）。
	TrackingURLTemplate string    `gorm:"column:tracking_url_template;not null"`
	Status              string    `gorm:"column:status;not null"`
	Sort                int       `gorm:"column:sort;not null"`
	Metadata            []byte    `gorm:"column:metadata;type:jsonb;not null"`
	CreateTime          time.Time `gorm:"column:create_time;not null"`
	UpdateTime          time.Time `gorm:"column:update_time;not null"`
}

// TableName 实现 gorm 表名。
func (OrderCarrierEntity) TableName() string { return "order_carriers" }

// OrderShipmentEntity 发货单（迁移 624）。
//
// carrier_name 是**名称快照**：承运商改名 / 停用 / 删除之后，历史单据仍要显示当时的名字。
type OrderShipmentEntity struct {
	ID           string    `gorm:"column:id;primaryKey"`
	ProjectID    string    `gorm:"column:project_id;not null"`
	OrderID      uint64    `gorm:"column:order_id;not null"`
	ShipmentNo   string    `gorm:"column:shipment_no;not null"`
	CarrierID    *string   `gorm:"column:carrier_id"`
	CarrierName  string    `gorm:"column:carrier_name;not null"`
	TrackingNo   string    `gorm:"column:tracking_no;not null"`
	ShippedAt    time.Time `gorm:"column:shipped_at;not null"`
	OperatorID   int64     `gorm:"column:operator_id;not null"`
	OperatorName string    `gorm:"column:operator_name;not null"`
	Remark       string    `gorm:"column:remark;not null"`
	// RequestID 幂等键：空串 = 本次没有幂等键（合法状态），非空时同工程唯一。
	RequestID  string    `gorm:"column:request_id;not null"`
	CreateTime time.Time `gorm:"column:create_time;not null"`
}

// TableName 实现 gorm 表名。
func (OrderShipmentEntity) TableName() string { return "order_shipments" }

// OrderShipmentItemEntity 发货明细（迁移 624）。
//
// stock_id 是**仓库商品 SKU 行快照**（可为空：库存行可能随变体删除而消失，但发货事实要留得住）。
type OrderShipmentItemEntity struct {
	ID          int64   `gorm:"column:id;primaryKey"`
	ShipmentID  string  `gorm:"column:shipment_id;not null"`
	OrderItemID uint64  `gorm:"column:order_item_id;not null"`
	WarehouseID string  `gorm:"column:warehouse_id;not null"`
	StockID     *string `gorm:"column:stock_id"`
	Quantity    int     `gorm:"column:quantity;not null"`
}

// TableName 实现 gorm 表名。
func (OrderShipmentItemEntity) TableName() string { return "order_shipment_items" }

// OrderCarrierModel 承运商表访问单元。
type OrderCarrierModel struct{ db *gorm.DB }

func NewOrderCarrierModel(db *gorm.DB) *OrderCarrierModel { return &OrderCarrierModel{db: db} }

// List 某工程的承运商（默认只列启用中的：停用的不该出现在发货表单里）。
func (m *OrderCarrierModel) List(ctx context.Context, projectID string, includeDisabled bool) (list []*OrderCarrierEntity, err error) {
	q := m.db.WithContext(ctx).Model(&OrderCarrierEntity{}).Where("project_id = ?", projectID)
	if !includeDisabled {
		q = q.Where("status = ?", "active")
	}
	err = q.Order("sort ASC, code ASC").Find(&list).Error
	return list, err
}

// Get 按 id 取承运商（不存在返回 gorm.ErrRecordNotFound）。
func (m *OrderCarrierModel) Get(ctx context.Context, id, projectID string) (e *OrderCarrierEntity, err error) {
	e = &OrderCarrierEntity{}
	err = m.db.WithContext(ctx).Model(&OrderCarrierEntity{}).
		Where("id = ? AND project_id = ?", id, projectID).First(e).Error
	return e, err
}

// CodeExists 该工程内承运商代码是否被占用（大小写不敏感，excludeID 为空表示新建）。
//
// 判据与 DDL 的 uq_order_carriers_project_code（upper(code)）一致，只是提前到写入之前：
// 直接撞约束只会拿到一个没有上下文的 23505。
func (m *OrderCarrierModel) CodeExists(ctx context.Context, projectID, code, excludeID string) (exists bool, err error) {
	q := m.db.WithContext(ctx).Model(&OrderCarrierEntity{}).
		Where("project_id = ? AND upper(code) = upper(?)", projectID, code)
	if excludeID != "" {
		q = q.Where("id <> ?", excludeID)
	}
	var n int64
	if err = q.Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

// Create 新建承运商。
func (m *OrderCarrierModel) Create(ctx context.Context, e *OrderCarrierEntity) error {
	return m.db.WithContext(ctx).Create(e).Error
}

// Update 保存承运商（全字段）。
func (m *OrderCarrierModel) Update(ctx context.Context, e *OrderCarrierEntity) error {
	return m.db.WithContext(ctx).Model(&OrderCarrierEntity{}).Where("id = ?", e.ID).Save(e).Error
}

// OrderShipmentModel 发货单表访问单元。
type OrderShipmentModel struct{ db *gorm.DB }

func NewOrderShipmentModel(db *gorm.DB) *OrderShipmentModel { return &OrderShipmentModel{db: db} }

// CreateTx 事务内写一张发货单（单据 + 明细同事务：只留单据没有明细就是「发了 0 件」）。
func (m *OrderShipmentModel) CreateTx(ctx context.Context, tx *gorm.DB, e *OrderShipmentEntity,
	items []*OrderShipmentItemEntity) (err error) {
	if err = tx.WithContext(ctx).Create(e).Error; err != nil {
		return err
	}
	for _, it := range items {
		it.ShipmentID = e.ID
	}
	if len(items) == 0 {
		// 空明细在 service 层就被拒（ErrShipLinesRequired），这里兜住未来改动。
		return nil
	}
	return tx.WithContext(ctx).Create(&items).Error
}

// ListByOrder 某订单的全部发货（按发货时刻升序：客户看到的应当是「先发的在前」）。
func (m *OrderShipmentModel) ListByOrder(ctx context.Context, orderID uint64) (list []*OrderShipmentEntity, err error) {
	err = m.db.WithContext(ctx).Model(&OrderShipmentEntity{}).
		Where("order_id = ?", orderID).Order("shipped_at ASC, create_time ASC").Find(&list).Error
	return list, err
}

// ListItemsByShipments 批量取若干发货单的明细（列表页一次取全，不逐单查）。
func (m *OrderShipmentModel) ListItemsByShipments(ctx context.Context, shipmentIDs []string) (list []*OrderShipmentItemEntity, err error) {
	if len(shipmentIDs) == 0 {
		return nil, nil
	}
	err = m.db.WithContext(ctx).Model(&OrderShipmentItemEntity{}).
		Where("shipment_id IN ?", shipmentIDs).Order("id ASC").Find(&list).Error
	return list, err
}

// ListItemsByOrder 某订单的全部发货明细（按订单行聚合用）。
//
// 走 join 而不是先查 shipment 再 IN：一张订单的发货单数量是个位数，两次往返不值得，
// 而 join 让「订单 → 发货明细」只有一条查询（列表页一次一单，正是这条）。
func (m *OrderShipmentModel) ListItemsByOrder(ctx context.Context, orderID uint64) (list []*OrderShipmentItemEntity, err error) {
	err = m.db.WithContext(ctx).Model(&OrderShipmentItemEntity{}).
		Joins("JOIN order_shipments AS s ON s.id = order_shipment_items.shipment_id").
		Where("s.order_id = ?", orderID).Order("order_shipment_items.id ASC").Find(&list).Error
	return list, err
}

// SumShippedQuantityTx 事务内取某订单若干订单行的**已发数量账**（itemID → 已发件数）。
//
// 这是「已发多少」的唯一真源：明细求和，不是订单行上的缓存列 ——
// 缓存列会在「删一张发货单」这类操作后静默失准，而求和永远不会。
//
// 传 itemIDs 而不是整单：service 已经拿到订单行了，按它求和既省一次全表统计，
// 也让「已发账」与「本次要发的行」是同一个集合（多一行的差额就无从解释）。
func (m *OrderShipmentModel) SumShippedQuantityTx(ctx context.Context, tx *gorm.DB,
	orderID uint64, itemIDs []uint64) (sum map[uint64]int, err error) {
	sum = make(map[uint64]int, len(itemIDs))
	if len(itemIDs) == 0 {
		return sum, nil
	}
	var rows []struct {
		OrderItemID uint64 `gorm:"column:order_item_id"`
		Quantity    int    `gorm:"column:quantity"`
	}
	err = tx.WithContext(ctx).Model(&OrderShipmentItemEntity{}).
		Select("order_shipment_items.order_item_id, SUM(order_shipment_items.quantity) AS quantity").
		Joins("JOIN order_shipments AS s ON s.id = order_shipment_items.shipment_id").
		Where("s.order_id = ? AND order_shipment_items.order_item_id IN ?", orderID, itemIDs).
		Group("order_shipment_items.order_item_id").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		sum[r.OrderItemID] = r.Quantity
	}
	return sum, nil
}

// FindByRequestID 幂等命中检查：命中返回既有发货单，未命中返回 (nil, nil)。
func (m *OrderShipmentModel) FindByRequestID(ctx context.Context, projectID, requestID string) (e *OrderShipmentEntity, err error) {
	if requestID == "" {
		return nil, nil
	}
	e = &OrderShipmentEntity{}
	err = m.db.WithContext(ctx).Model(&OrderShipmentEntity{}).
		Where("project_id = ? AND request_id = ?", projectID, requestID).First(e).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return e, nil
}

// CountShipmentsOnDayTx 事务内数某工程当日的发货单张数（单号序号的来源）。
//
// 单号 = SH + YYYYMMDD + 6 位序号。序号按当日前缀计数取，冲突由唯一索引兜底并重试一次
// （并发下两张单可能数到同一个序号 —— 唯一索引让其中一张失败，重试即可，不需要序列表）。
func (m *OrderShipmentModel) CountShipmentsOnDayTx(ctx context.Context, tx *gorm.DB,
	projectID string, dayStart, dayEnd time.Time) (n int64, err error) {
	err = tx.WithContext(ctx).Model(&OrderShipmentEntity{}).
		Where("project_id = ? AND create_time >= ? AND create_time < ?", projectID, dayStart, dayEnd).
		Count(&n).Error
	return n, err
}

// ListShipmentItemsByOrderItem 某订单行已被哪些发货单发过（详情抽屉用）。
func (m *OrderShipmentModel) ListShipmentItemsByOrderItem(ctx context.Context,
	orderItemID uint64) (list []*OrderShipmentItemEntity, err error) {
	err = m.db.WithContext(ctx).Model(&OrderShipmentItemEntity{}).
		Where("order_item_id = ?", orderItemID).Order("id ASC").Find(&list).Error
	return list, err
}
