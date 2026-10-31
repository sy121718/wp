package model

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go_wp/pkg/rls"
)

const (
	ReturnStatusRequested = "requested"
	ReturnStatusApproved  = "approved"
	ReturnStatusReceived  = "received"
	ReturnStatusCompleted = "completed"
	ReturnStatusRejected  = "rejected"
	ReturnStatusCancelled = "cancelled"
)

var ReturnActiveStatuses = []string{
	ReturnStatusRequested, ReturnStatusApproved, ReturnStatusReceived, ReturnStatusCompleted,
}

var ReturnedStatuses = []string{ReturnStatusReceived, ReturnStatusCompleted}

type ReturnEntity struct {
	ID        uint64 `gorm:"column:id;primaryKey"`
	ProjectID string `gorm:"column:project_id"`
	OrderID   uint64 `gorm:"column:order_id"`
	OrderNo   string `gorm:"column:order_no"`
	ReturnNo  string `gorm:"column:return_no"`
	Status    string `gorm:"column:status"`
	Reason    string `gorm:"column:reason"`

	RefundAmount  int64      `gorm:"column:refund_amount"`
	UserID        *uint64    `gorm:"column:user_id"`
	CustomerEmail string     `gorm:"column:customer_email"`
	CustomerName  string     `gorm:"column:customer_name"`
	AdminNote     string     `gorm:"column:admin_note"`
	ReviewerID    uint64     `gorm:"column:reviewer_id"`
	ReviewerName  string     `gorm:"column:reviewer_name"`
	ReviewedAt    *time.Time `gorm:"column:reviewed_at"`
	ReceivedAt    *time.Time `gorm:"column:received_at"`
	RefundedAt    *time.Time `gorm:"column:refunded_at"`
	TransactionID string     `gorm:"column:transaction_id"`
	RequestID     string     `gorm:"column:request_id"`
	CreateTime    time.Time  `gorm:"column:create_time"`
	UpdateTime    time.Time  `gorm:"column:update_time"`
}

func (ReturnEntity) TableName() string { return "order_returns" }

type ReturnItemEntity struct {
	ID           uint64 `gorm:"column:id;primaryKey"`
	ReturnID     uint64 `gorm:"column:return_id"`
	OrderItemID  uint64 `gorm:"column:order_item_id"`
	ProductID    string `gorm:"column:product_id"`
	VariantID    string `gorm:"column:variant_id"`
	ProductName  string `gorm:"column:product_name"`
	VariantLabel string `gorm:"column:variant_label"`
	SKU          string `gorm:"column:sku"`
	UnitPrice    int64  `gorm:"column:unit_price"`
	Quantity     int    `gorm:"column:quantity"`

	ReceivedQuantity int       `gorm:"column:received_quantity"`
	RefundAmount     int64     `gorm:"column:refund_amount"`
	CreateTime       time.Time `gorm:"column:create_time"`
}

func (ReturnItemEntity) TableName() string { return "order_return_items" }

type ReturnFilter struct {
	ProjectID string
	Status    string
	Keyword   string
	OrderID   uint64

	UserID *uint64
	Offset int
	Limit  int
}

type ReturnModel struct{ db *gorm.DB }

func NewReturnModel(db *gorm.DB) *ReturnModel { return &ReturnModel{db: db} }

func (m *ReturnModel) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&ReturnEntity{})
}

func (m *ReturnModel) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

func (m *ReturnModel) CreateTx(ctx context.Context, tx *gorm.DB, e *ReturnEntity) (err error) {

	if serr := rls.ScopeTx(tx, e.ProjectID); serr != nil {
		return serr
	}
	return tx.WithContext(ctx).Model(&ReturnEntity{}).Create(e).Error
}

func (m *ReturnModel) CreateItemsTx(ctx context.Context, tx *gorm.DB, items []*ReturnItemEntity) (err error) {
	if len(items) == 0 {
		return nil
	}
	return tx.WithContext(ctx).Model(&ReturnItemEntity{}).Create(&items).Error
}

func (m *ReturnModel) GetByID(ctx context.Context, projectID string, id uint64) (e *ReturnEntity, err error) {
	e = &ReturnEntity{}
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	if err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&ReturnEntity{}).Where("id = ? AND project_id = ?", id, projectID).First(e).Error
	}); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return e, nil
}

func (m *ReturnModel) LockByIDTx(ctx context.Context, tx *gorm.DB, projectID string, id uint64) (e *ReturnEntity, err error) {
	if strings.TrimSpace(projectID) != "" {
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return nil, serr
		}
	}
	e = &ReturnEntity{}
	q := tx.WithContext(ctx).Model(&ReturnEntity{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", id)
	if strings.TrimSpace(projectID) != "" {
		q = q.Where("project_id = ?", projectID)
	}
	if err = q.First(e).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return e, nil
}

func (m *ReturnModel) GetByRequestID(ctx context.Context, projectID, requestID string) (e *ReturnEntity, err error) {
	if strings.TrimSpace(requestID) == "" {
		return nil, nil
	}
	e = &ReturnEntity{}
	if err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&ReturnEntity{}).Where("project_id = ? AND request_id = ?", projectID, requestID).First(e).Error
	}); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return e, nil
}

func (m *ReturnModel) List(ctx context.Context, f ReturnFilter) (list []*ReturnEntity, total int64, err error) {
	err = rls.InProjectScope(ctx, m.db, f.ProjectID, func(tx *gorm.DB) error {
		return m.listLocked(tx, f, &list, &total)
	})
	return list, total, err
}

func (m *ReturnModel) listLocked(tx *gorm.DB, f ReturnFilter, list *[]*ReturnEntity, total *int64) error {
	q := tx.Model(&ReturnEntity{}).Where("project_id = ?", f.ProjectID)
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.OrderID != 0 {
		q = q.Where("order_id = ?", f.OrderID)
	}
	if f.UserID != nil {
		q = q.Where("user_id = ?", *f.UserID)
	}
	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		like := "%" + kw + "%"
		q = q.Where("return_no ILIKE ? OR order_no ILIKE ? OR customer_email ILIKE ?", like, like, like)
	}
	if err := q.Count(total).Error; err != nil {
		return err
	}
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	return q.Order("id DESC").Offset(f.Offset).Limit(limit).Find(list).Error
}

func (m *ReturnModel) CountByStatus(ctx context.Context, projectID string) (counts map[string]int64, err error) {
	var rows []struct {
		Status string `gorm:"column:status"`
		N      int64  `gorm:"column:n"`
	}
	if err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&ReturnEntity{}).Select("status, COUNT(*) AS n").
			Where("project_id = ?", projectID).Group("status").Scan(&rows).Error
	}); err != nil {
		return nil, err
	}
	counts = make(map[string]int64, len(rows))
	for _, r := range rows {
		counts[r.Status] = r.N
	}
	return counts, nil
}

func (m *ReturnModel) UpdateFieldsTx(ctx context.Context, tx *gorm.DB, projectID string, id uint64, fields map[string]any) (err error) {
	if strings.TrimSpace(projectID) != "" {
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return serr
		}
	}
	q := tx.WithContext(ctx).Model(&ReturnEntity{}).Where("id = ?", id)
	if strings.TrimSpace(projectID) != "" {
		q = q.Where("project_id = ?", projectID)
	}
	return q.Updates(fields).Error
}

func (m *ReturnModel) ItemsByReturnID(ctx context.Context, returnID uint64) (items []*ReturnItemEntity, err error) {
	err = m.db.WithContext(ctx).Model(&ReturnItemEntity{}).
		Where("return_id = ?", returnID).Order("id ASC").Find(&items).Error
	return items, err
}

func (m *ReturnModel) ItemsByReturnIDTx(ctx context.Context, tx *gorm.DB, returnID uint64) (items []*ReturnItemEntity, err error) {
	err = tx.WithContext(ctx).Model(&ReturnItemEntity{}).
		Where("return_id = ?", returnID).Order("id ASC").Find(&items).Error
	return items, err
}

func (m *ReturnModel) ItemsByReturnIDs(ctx context.Context, returnIDs []uint64) (items []*ReturnItemEntity, err error) {
	if len(returnIDs) == 0 {
		return nil, nil
	}
	err = m.db.WithContext(ctx).Model(&ReturnItemEntity{}).
		Where("return_id IN ?", returnIDs).Order("id ASC").Find(&items).Error
	return items, err
}

func (m *ReturnModel) UpdateItemReceivedTx(ctx context.Context, tx *gorm.DB, itemID uint64, received int, refund int64) (err error) {
	return tx.WithContext(ctx).Model(&ReturnItemEntity{}).Where("id = ?", itemID).
		Updates(map[string]any{"received_quantity": received, "refund_amount": refund}).Error
}

func (m *ReturnModel) IDsByOrder(ctx context.Context, projectID string, orderID uint64, statuses []string) (ids []uint64, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	if orderID == 0 || len(statuses) == 0 {
		return nil, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&ReturnEntity{}).
			Where("project_id = ? AND order_id = ? AND status IN ?", projectID, orderID, statuses).
			Order("id ASC").Pluck("id", &ids).Error
	})
	return ids, err
}

func (m *ReturnModel) IDsByOrderTx(ctx context.Context, tx *gorm.DB, projectID string, orderID uint64, statuses []string) (ids []uint64, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	if orderID == 0 || len(statuses) == 0 {
		return nil, nil
	}
	if serr := rls.ScopeTx(tx, projectID); serr != nil {
		return nil, serr
	}
	err = tx.WithContext(ctx).Model(&ReturnEntity{}).
		Where("project_id = ? AND order_id = ? AND status IN ?", projectID, orderID, statuses).
		Order("id ASC").Pluck("id", &ids).Error
	return ids, err
}

func (m *ReturnModel) SumQuantityByOrderItems(ctx context.Context, returnIDs []uint64, orderItemIDs []uint64) (sums map[uint64]int, err error) {
	sums = map[uint64]int{}
	if len(returnIDs) == 0 || len(orderItemIDs) == 0 {
		return sums, nil
	}
	var rows []struct {
		OrderItemID uint64 `gorm:"column:order_item_id"`
		Qty         int64  `gorm:"column:qty"`
	}
	if err = m.db.WithContext(ctx).Model(&ReturnItemEntity{}).
		Select("order_item_id, SUM(quantity) AS qty").
		Where("return_id IN ? AND order_item_id IN ?", returnIDs, orderItemIDs).
		Group("order_item_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		sums[r.OrderItemID] = int(r.Qty)
	}
	return sums, nil
}

func (m *ReturnModel) SumQuantityByOrderItemsTx(ctx context.Context, tx *gorm.DB, returnIDs []uint64, orderItemIDs []uint64) (sums map[uint64]int, err error) {
	sums = map[uint64]int{}
	if len(returnIDs) == 0 || len(orderItemIDs) == 0 {
		return sums, nil
	}
	var rows []struct {
		OrderItemID uint64 `gorm:"column:order_item_id"`
		Qty         int64  `gorm:"column:qty"`
	}
	if err = tx.WithContext(ctx).Model(&ReturnItemEntity{}).
		Select("order_item_id, SUM(quantity) AS qty").
		Where("return_id IN ? AND order_item_id IN ?", returnIDs, orderItemIDs).
		Group("order_item_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		sums[r.OrderItemID] = int(r.Qty)
	}
	return sums, nil
}

func (m *ReturnModel) SumReceivedQuantityByOrderItemsTx(ctx context.Context, tx *gorm.DB,
	projectID string, orderID uint64, orderItemIDs []uint64) (sums map[uint64]int, err error) {
	sums = map[uint64]int{}
	if orderID == 0 || len(orderItemIDs) == 0 {
		return sums, nil
	}
	ids, ierr := m.IDsByOrderTx(ctx, tx, projectID, orderID, ReturnedStatuses)
	if ierr != nil {
		return nil, ierr
	}
	if len(ids) == 0 {
		return sums, nil
	}
	var rows []struct {
		OrderItemID uint64 `gorm:"column:order_item_id"`
		Qty         int64  `gorm:"column:qty"`
	}
	if err = tx.WithContext(ctx).Model(&ReturnItemEntity{}).
		Select("order_item_id, SUM(received_quantity) AS qty").
		Where("return_id IN ? AND order_item_id IN ?", ids, orderItemIDs).
		Group("order_item_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		sums[r.OrderItemID] = int(r.Qty)
	}
	return sums, nil
}
