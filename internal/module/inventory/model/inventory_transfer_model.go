package inventorymodel

// inventory_transfer_model.go — 调拨单 / 明细 / 凭证附件（迁移 627，W3）。
//
// 三张表的分工（写在这里而不是散在 service 各处）：
//
//	inventory_transfers           单据头：单号 / 两仓 / 状态 / 操作人与时刻 / 幂等键
//	inventory_transfer_items      明细：搬哪条**源仓库存行**多少件（+ 变体与编码快照）
//	inventory_transfer_attachments 凭证：附件的「用途」（request / ship / receive）与排序
//
// 三条不变量：
//
//  1. 明细指向**库存行**（stock_id）而不是变体：调拨搬的是源仓里那一批具体的货，
//     这样才能表达「未认领的货也能调拨」；
//  2. 出货与签收**各需至少一张凭证**（service 强制，本层只负责落库）——
//     一张没有凭证的移库事后无法证明「货是谁交出去的、谁签收的」；
//  3. 状态单向：pending → in_transit → completed，或 pending → cancelled。
//     in_transit 不能直接取消（货在路上），要走反向调拨或先签收。

import (
	"context"
	"time"

	"gorm.io/gorm"

	"gorm.io/gorm/clause"

	"go_wp/pkg/rls"
)

// TransferEntity 调拨单头（迁移 627）。
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
	// RequestID 幂等键：空串 = 没有幂等键（合法状态），非空时同工程唯一。
	RequestID  string    `gorm:"column:request_id;not null"`
	CreateTime time.Time `gorm:"column:create_time;not null"`
	UpdateTime time.Time `gorm:"column:update_time;not null"`
}

// TableName 实现 gorm 表名。
func (TransferEntity) TableName() string { return "inventory_transfers" }

// TransferItemEntity 调拨明细（迁移 627）：一条**源仓库存行**搬多少件。
//
// VariantID 可空：未认领行（迁移 621 引入的 variant_id IS NULL）也允许调拨 ——
// 「仓库里已有的货」正是最需要调拨的那批（清关到仓、客户寄卖的货）。
type TransferItemEntity struct {
	ID         int64   `gorm:"column:id;primaryKey"`
	TransferID string  `gorm:"column:transfer_id;not null"`
	StockID    string  `gorm:"column:stock_id;not null"`
	VariantID  *string `gorm:"column:variant_id"`
	SKUCode    string  `gorm:"column:sku_code;not null"`
	Quantity   int     `gorm:"column:quantity;not null"`
}

// TableName 实现 gorm 表名。
func (TransferItemEntity) TableName() string { return "inventory_transfer_items" }

// TransferAttachmentEntity 调拨凭证（迁移 627）：附件的用途与排序。
type TransferAttachmentEntity struct {
	ID           int64     `gorm:"column:id;primaryKey"`
	TransferID   string    `gorm:"column:transfer_id;not null"`
	AttachmentID int64     `gorm:"column:attachment_id;not null"`
	UsageType    string    `gorm:"column:usage_type;not null"`
	Sort         int       `gorm:"column:sort;not null"`
	CreateTime   time.Time `gorm:"column:create_time;not null"`
}

// TableName 实现 gorm 表名。
func (TransferAttachmentEntity) TableName() string { return "inventory_transfer_attachments" }

// —— 调拨单 ——

// CreateTransferTx 事务内建调拨单 + 明细（单据与明细同事务：只留单据没有明细就是「搬 0 件」）。
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

// GetTransferTx 事务内按 id 取调拨单（不存在返回 gorm.ErrRecordNotFound）。
func (m *Model) GetTransferTx(ctx context.Context, tx *gorm.DB, projectID, id string) (e *TransferEntity, err error) {
	if serr := rls.ScopeTx(tx, projectID); serr != nil {
		return nil, serr
	}
	e = &TransferEntity{}
	err = tx.WithContext(ctx).Model(&TransferEntity{}).Where("id = ?", id).First(e).Error
	return e, err
}

// LockTransferTx 事务内对调拨单取 FOR UPDATE 行锁（出货 / 签收 / 取消都必须串行）。
func (m *Model) LockTransferTx(ctx context.Context, tx *gorm.DB, projectID, id string) (e *TransferEntity, err error) {
	if serr := rls.ScopeTx(tx, projectID); serr != nil {
		return nil, serr
	}
	e = &TransferEntity{}
	err = tx.WithContext(ctx).Model(&TransferEntity{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(e).Error
	return e, err
}

// UpdateTransferTx 事务内写回调拨单（状态 / 操作人与时刻）。
func (m *Model) UpdateTransferTx(ctx context.Context, tx *gorm.DB, e *TransferEntity) (err error) {
	if serr := rls.ScopeTx(tx, e.ProjectID); serr != nil {
		return serr
	}
	return tx.WithContext(ctx).Model(&TransferEntity{}).Where("id = ?", e.ID).Save(e).Error
}

// ListTransferItemsTx 事务内取某调拨单的明细（按 id 升序：与创建顺序一致）。
func (m *Model) ListTransferItemsTx(ctx context.Context, tx *gorm.DB, projectID, transferID string) (list []*TransferItemEntity, err error) {
	if serr := rls.ScopeTx(tx, projectID); serr != nil {
		return nil, serr
	}
	err = tx.WithContext(ctx).Model(&TransferItemEntity{}).
		Where("transfer_id = ?", transferID).Order("id ASC").Find(&list).Error
	return list, err
}

// CreateTransferAttachmentsTx 事务内写凭证行（出货 / 签收各自的 usage_type）。
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

// ListTransferAttachmentsTx 事务内取某调拨单的凭证（按用途分组由调用方做）。
func (m *Model) ListTransferAttachmentsTx(ctx context.Context, tx *gorm.DB, projectID, transferID string) (
	list []*TransferAttachmentEntity, err error) {
	if serr := rls.ScopeTx(tx, projectID); serr != nil {
		return nil, serr
	}
	err = tx.WithContext(ctx).Model(&TransferAttachmentEntity{}).
		Where("transfer_id = ?", transferID).Order("usage_type ASC, sort ASC, id ASC").Find(&list).Error
	return list, err
}

// FindTransferByRequestID 幂等命中检查：命中返回既有调拨单，未命中返回 (nil, nil)。
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

// CountTransfersOnDayTx 事务内数某工程当日的调拨单张数（单号序号的来源）。
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

// TransferListFilter 调拨单列表的查询条件（条件以参数传入）。
type TransferListFilter struct {
	ProjectID string
	// Status 空串 = 不过滤。
	Status  string
	Keyword string
}

// TransferListRow 调拨单列表的一行（含两个仓的展示信息与品类数 / 总数量）。
//
// 两个汇总由 SQL 算好（不是模板算）：模板算数量会与 service 口径分叉
// （template_logic_test.go 的判据针对的正是这类「看起来无害」的聚合）。
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
	// ShippedAt / ReceivedAt 可空：还没到那一步。
	ShippedAt  *time.Time `gorm:"column:shipped_at"`
	ReceivedAt *time.Time `gorm:"column:received_at"`
}

// ListTransfers 调拨单列表（按工程 / 状态 / 关键词过滤 + 分页；limit <= 0 表示不限）。
func (m *Model) ListTransfers(ctx context.Context, f TransferListFilter, limit, offset int) (list []*TransferListRow, err error) {
	err = rls.InProjectScope(ctx, m.db, f.ProjectID, func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Table("inventory_transfers AS t").
			Select("t.id, t.transfer_no, t.status, t.remark, t.from_warehouse_id, t.to_warehouse_id, "+
				"t.create_time, t.shipped_at, t.received_at, "+
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

// CountTransfers 调拨单总数（与 ListTransfers 同一份过滤条件）。
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

// GetTransfer 按 id 取调拨单（非事务版）。
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

// ListTransferItems 某调拨单的明细（非事务版）。
func (m *Model) ListTransferItems(ctx context.Context, projectID, transferID string) (list []*TransferItemEntity, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&TransferItemEntity{}).
			Where("transfer_id = ?", transferID).Order("id ASC").Find(&list).Error
	})
	return list, err
}

// ListTransferAttachments 某调拨单的凭证（非事务版）。
func (m *Model) ListTransferAttachments(ctx context.Context, projectID, transferID string) (
	list []*TransferAttachmentEntity, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&TransferAttachmentEntity{}).
			Where("transfer_id = ?", transferID).Order("usage_type ASC, sort ASC, id ASC").Find(&list).Error
	})
	return list, err
}

// DeductUnclaimedStockTx 未认领行出库（按库存行 id）：quantity -= n，守卫在 WHERE 里。
//
// 为什么不走 applyStockChangesTx：那条路径按 (variant, warehouse) 加锁与写流水，
// 而未认领行没有变体 —— 补一个空串会让流水落一条无法归属的记录。
// 这里只改数量（守卫 quantity >= n 在 SQL 里，并发下只有一句能匹配上行）。
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

// IncreaseStockQuantityTx 目标仓入库（quantity += n，并按需翻跟踪开关）。
//
// 与入库走不同的路径：调拨签收是**必有单据**的入库，数量已经由源仓那一行确定，
// 不需要走变动契约的「原因字典 / 流水」那套（单据本身就是凭据）。
// 但仍要把 track_quantity 翻成 true —— 有数量却不跟踪会撞迁移 261 的 CHECK。
func (m *Model) IncreaseStockQuantityTx(ctx context.Context, tx *gorm.DB, stockID string,
	quantity int, at time.Time) (err error) {
	return tx.WithContext(ctx).Model(&StockEntity{}).Where("id = ?", stockID).
		Updates(map[string]any{
			"quantity":       gorm.Expr("quantity + ?", quantity),
			"track_quantity": true,
			"update_time":    at,
		}).Error
}
