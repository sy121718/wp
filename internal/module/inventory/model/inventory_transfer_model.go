package inventorymodel

import (
	"context"
	"time"

	"gorm.io/gorm"

	"gorm.io/gorm/clause"

	"go_wp/pkg/rls"
)

type TransferEntity struct {
	ID              string     `gorm:"column:id;primaryKey"`
	ProjectID       string     `gorm:"column:project_id;not null"`
	TransferNo      string     `gorm:"column:transfer_no;not null"`
	FromWarehouseID string     `gorm:"column:from_warehouse_id;not null"`
	ToWarehouseID   string     `gorm:"column:to_warehouse_id;not null"`
	Status          string     `gorm:"column:status;not null"`
	Remark          string     `gorm:"column:remark;not null"`
	CreateBy        int64      `gorm:"column:create_by;not null"`
	ShippedBy       int64      `gorm:"column:shipped_by;not null"`
	ReceivedBy      int64      `gorm:"column:received_by;not null"`
	ShippedAt       *time.Time `gorm:"column:shipped_at"`
	ReceivedAt      *time.Time `gorm:"column:received_at"`

	RequestedBy int64      `gorm:"column:requested_by;not null"`
	RequestedAt *time.Time `gorm:"column:requested_at"`
	ApprovedBy  int64      `gorm:"column:approved_by;not null"`
	ApprovedAt  *time.Time `gorm:"column:approved_at"`

	ApprovedName string     `gorm:"column:approved_name;not null"`
	RejectedBy   int64      `gorm:"column:rejected_by;not null"`
	RejectedAt   *time.Time `gorm:"column:rejected_at"`
	RejectedName string     `gorm:"column:rejected_name;not null"`

	RejectedReason string `gorm:"column:rejected_reason;not null"`

	RequestID  string    `gorm:"column:request_id;not null"`
	CreateTime time.Time `gorm:"column:create_time;not null"`
	UpdateTime time.Time `gorm:"column:update_time;not null"`
}

func (TransferEntity) TableName() string { return "inventory_transfers" }

type TransferItemEntity struct {
	ID         int64   `gorm:"column:id;primaryKey"`
	TransferID string  `gorm:"column:transfer_id;not null"`
	StockID    string  `gorm:"column:stock_id;not null"`
	VariantID  *string `gorm:"column:variant_id"`
	SKUCode    string  `gorm:"column:sku_code;not null"`
	Quantity   int     `gorm:"column:quantity;not null"`
}

func (TransferItemEntity) TableName() string { return "inventory_transfer_items" }

type TransferAttachmentEntity struct {
	ID           int64     `gorm:"column:id;primaryKey"`
	TransferID   string    `gorm:"column:transfer_id;not null"`
	AttachmentID int64     `gorm:"column:attachment_id;not null"`
	UsageType    string    `gorm:"column:usage_type;not null"`
	Sort         int       `gorm:"column:sort;not null"`
	CreateTime   time.Time `gorm:"column:create_time;not null"`
}

func (TransferAttachmentEntity) TableName() string { return "inventory_transfer_attachments" }

func (m *Model) CreateTransferTx(ctx context.Context, tx *gorm.DB, e *TransferEntity,
	items []*TransferItemEntity, attachments []*TransferAttachmentEntity) (err error) {
	if serr := rls.ScopeTx(tx, e.ProjectID); serr != nil {
		return serr
	}
	if err = tx.WithContext(ctx).Create(e).Error; err != nil {
		return err
	}
	for _, it := range items {
		it.TransferID = e.ID
	}
	if len(items) > 0 {
		if err = tx.WithContext(ctx).Create(&items).Error; err != nil {
			return err
		}
	}
	for _, a := range attachments {
		a.TransferID = e.ID
	}
	if len(attachments) > 0 {
		return tx.WithContext(ctx).Create(&attachments).Error
	}
	return nil
}

func (m *Model) GetTransferTx(ctx context.Context, tx *gorm.DB, projectID, id string) (e *TransferEntity, err error) {
	if serr := rls.ScopeTx(tx, projectID); serr != nil {
		return nil, serr
	}
	e = &TransferEntity{}
	err = tx.WithContext(ctx).Model(&TransferEntity{}).Where("id = ?", id).First(e).Error
	return e, err
}

func (m *Model) LockTransferTx(ctx context.Context, tx *gorm.DB, projectID, id string) (e *TransferEntity, err error) {
	if serr := rls.ScopeTx(tx, projectID); serr != nil {
		return nil, serr
	}
	e = &TransferEntity{}
	err = tx.WithContext(ctx).Model(&TransferEntity{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(e).Error
	return e, err
}

func (m *Model) UpdateTransferTx(ctx context.Context, tx *gorm.DB, e *TransferEntity) (err error) {
	if serr := rls.ScopeTx(tx, e.ProjectID); serr != nil {
		return serr
	}
	return tx.WithContext(ctx).Model(&TransferEntity{}).Where("id = ?", e.ID).Save(e).Error
}

func (m *Model) ListTransferItemsTx(ctx context.Context, tx *gorm.DB, projectID, transferID string) (list []*TransferItemEntity, err error) {
	if serr := rls.ScopeTx(tx, projectID); serr != nil {
		return nil, serr
	}
	err = tx.WithContext(ctx).Model(&TransferItemEntity{}).
		Where("transfer_id = ?", transferID).Order("id ASC").Find(&list).Error
	return list, err
}

func (m *Model) CreateTransferAttachmentsTx(ctx context.Context, tx *gorm.DB, projectID string,
	rows []*TransferAttachmentEntity) (err error) {
	if len(rows) == 0 {
		return nil
	}
	if serr := rls.ScopeTx(tx, projectID); serr != nil {
		return serr
	}
	return tx.WithContext(ctx).Create(&rows).Error
}

func (m *Model) ListTransferAttachmentsTx(ctx context.Context, tx *gorm.DB, projectID, transferID string) (
	list []*TransferAttachmentEntity, err error) {
	if serr := rls.ScopeTx(tx, projectID); serr != nil {
		return nil, serr
	}
	err = tx.WithContext(ctx).Model(&TransferAttachmentEntity{}).
		Where("transfer_id = ?", transferID).Order("usage_type ASC, sort ASC, id ASC").Find(&list).Error
	return list, err
}

func (m *Model) FindTransferByRequestID(ctx context.Context, projectID, requestID string) (e *TransferEntity, err error) {
	if requestID == "" {
		return nil, nil
	}
	e = &TransferEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		ferr := tx.WithContext(ctx).Model(&TransferEntity{}).
			Where("project_id = ? AND request_id = ?", projectID, requestID).First(e).Error
		if ferr == gorm.ErrRecordNotFound {
			return nil
		}
		return ferr
	})
	if err != nil {
		return nil, err
	}
	if e.ID == "" {
		return nil, nil
	}
	return e, nil
}

func (m *Model) CountTransfersOnDayTx(ctx context.Context, tx *gorm.DB, projectID string,
	dayStart, dayEnd time.Time) (n int64, err error) {
	if serr := rls.ScopeTx(tx, projectID); serr != nil {
		return 0, serr
	}
	err = tx.WithContext(ctx).Model(&TransferEntity{}).
		Where("project_id = ? AND create_time >= ? AND create_time < ?", projectID, dayStart, dayEnd).
		Count(&n).Error
	return n, err
}

type TransferListFilter struct {
	ProjectID string

	Status  string
	Keyword string
}

type TransferListRow struct {
	ID              string    `gorm:"column:id"`
	TransferNo      string    `gorm:"column:transfer_no"`
	Status          string    `gorm:"column:status"`
	Remark          string    `gorm:"column:remark"`
	FromWarehouseID string    `gorm:"column:from_warehouse_id"`
	ToWarehouseID   string    `gorm:"column:to_warehouse_id"`
	FromCode        string    `gorm:"column:from_code"`
	FromName        string    `gorm:"column:from_name"`
	ToCode          string    `gorm:"column:to_code"`
	ToName          string    `gorm:"column:to_name"`
	ItemCount       int       `gorm:"column:item_count"`
	TotalQuantity   int       `gorm:"column:total_quantity"`
	CreateTime      time.Time `gorm:"column:create_time"`

	ShippedAt  *time.Time `gorm:"column:shipped_at"`
	ReceivedAt *time.Time `gorm:"column:received_at"`

	RequestedBy    int64      `gorm:"column:requested_by"`
	RequestedAt    *time.Time `gorm:"column:requested_at"`
	ApprovedBy     int64      `gorm:"column:approved_by"`
	ApprovedAt     *time.Time `gorm:"column:approved_at"`
	ApprovedName   string     `gorm:"column:approved_name"`
	RejectedAt     *time.Time `gorm:"column:rejected_at"`
	RejectedName   string     `gorm:"column:rejected_name"`
	RejectedReason string     `gorm:"column:rejected_reason"`
}

func (m *Model) ListTransfers(ctx context.Context, f TransferListFilter, limit, offset int) (list []*TransferListRow, err error) {
	err = rls.InProjectScope(ctx, m.db, f.ProjectID, func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Table("inventory_transfers AS t").
			Select("t.id, t.transfer_no, t.status, t.remark, t.from_warehouse_id, t.to_warehouse_id, "+
				"t.create_time, t.shipped_at, t.received_at, "+
				"t.requested_by, t.requested_at, t.approved_by, t.approved_at, t.approved_name, "+
				"t.rejected_at, t.rejected_name, t.rejected_reason, "+
				"fw.code AS from_code, fw.name AS from_name, "+
				"tw.code AS to_code, tw.name AS to_name, "+
				"COALESCE(i.item_count, 0) AS item_count, COALESCE(i.total_quantity, 0) AS total_quantity").
			Joins("JOIN inventory_warehouses AS fw ON fw.id = t.from_warehouse_id").
			Joins("JOIN inventory_warehouses AS tw ON tw.id = t.to_warehouse_id").
			Joins("LEFT JOIN (SELECT transfer_id, COUNT(*) AS item_count, SUM(quantity) AS total_quantity "+
				"FROM inventory_transfer_items GROUP BY transfer_id) AS i ON i.transfer_id = t.id").
			Where("t.project_id = ?", f.ProjectID)
		if f.Status != "" {
			q = q.Where("t.status = ?", f.Status)
		}
		if f.Keyword != "" {
			like := "%" + f.Keyword + "%"
			q = q.Where("(t.transfer_no ILIKE ? OR t.remark ILIKE ?)", like, like)
		}
		q = q.Order("t.create_time DESC")
		if limit > 0 {
			q = q.Limit(limit).Offset(offset)
		}
		return q.Scan(&list).Error
	})
	return list, err
}

func (m *Model) CountTransfers(ctx context.Context, f TransferListFilter) (n int64, err error) {
	err = rls.InProjectScope(ctx, m.db, f.ProjectID, func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Model(&TransferEntity{}).Where("project_id = ?", f.ProjectID)
		if f.Status != "" {
			q = q.Where("status = ?", f.Status)
		}
		if f.Keyword != "" {
			like := "%" + f.Keyword + "%"
			q = q.Where("(transfer_no ILIKE ? OR remark ILIKE ?)", like, like)
		}
		return q.Count(&n).Error
	})
	return n, err
}

func (m *Model) GetTransfer(ctx context.Context, id, projectID string) (e *TransferEntity, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		e = &TransferEntity{}
		return tx.WithContext(ctx).Model(&TransferEntity{}).Where("id = ?", id).First(e).Error
	})
	if err != nil {
		return nil, err
	}
	return e, nil
}

func (m *Model) ListTransferItems(ctx context.Context, projectID, transferID string) (list []*TransferItemEntity, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&TransferItemEntity{}).
			Where("transfer_id = ?", transferID).Order("id ASC").Find(&list).Error
	})
	return list, err
}

func (m *Model) ListTransferAttachments(ctx context.Context, projectID, transferID string) (
	list []*TransferAttachmentEntity, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&TransferAttachmentEntity{}).
			Where("transfer_id = ?", transferID).Order("usage_type ASC, sort ASC, id ASC").Find(&list).Error
	})
	return list, err
}

func (m *Model) DeductUnclaimedStockTx(ctx context.Context, tx *gorm.DB, projectID, stockID string,
	quantity int, at time.Time) (affected int64, err error) {
	if serr := rls.ScopeTx(tx, projectID); serr != nil {
		return 0, serr
	}
	res := tx.WithContext(ctx).Model(&StockEntity{}).
		Where("id = ? AND variant_id IS NULL AND quantity >= ?", stockID, quantity).
		Updates(map[string]any{
			"quantity":    gorm.Expr("quantity - ?", quantity),
			"update_time": at,
		})
	return res.RowsAffected, res.Error
}

func (m *Model) IncreaseStockQuantityTx(ctx context.Context, tx *gorm.DB, stockID string,
	quantity int, at time.Time) (err error) {
	return tx.WithContext(ctx).Model(&StockEntity{}).Where("id = ?", stockID).
		Updates(map[string]any{
			"quantity":       gorm.Expr("quantity + ?", quantity),
			"track_quantity": true,
			"update_time":    at,
		}).Error
}
