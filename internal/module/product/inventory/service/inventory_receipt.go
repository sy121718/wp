// inventory_receipt.go — 入库登记：采购收货 / 自家工厂生产入库 / 进货历史（issue #18 验收 3/4/5/6）。
//
// 入库的事务形状（顺序是刻意的，不要重排）：
//
//	① 登记前：幂等键命中即原样返回（可重放保护，不第二次动库存）；
//	② 记账事务：锁采购单头 → 按行**原子递增**已入库数量（守卫在 UPDATE 的 WHERE 里）
//	   → 写入库单与入库行 → 用最新行重算推导状态；任一步失败整批回滚；
//	③ 库存变动：复用 #16 的 ChangeStock（方向 in + 原因字典 + 来源引用齐全），
//	   绝不旁路写 inventory_stocks；
//	④ 失败补偿：库存没动成功就把已入库数量退回并删掉入库单（不留半截记账）；
//	⑤ 提交后：记下批次号并把单据置 posted，再经商品模块端口回写 SKU 成本价
//	   （跨模块写不进同一个事务，失败记在入库单行上，不回滚已落地的真源）。
//
// 为什么「先记账再动库存」而不是反过来：超收守卫必须发生在库存变动之前，
// 否则并发收货会先把库存加上去再发现超收，只能靠反向变动擦屁股。
package inventoryservice

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	inventorydto "go_wp/internal/module/product/inventory/dto"
	inventoryenums "go_wp/internal/module/product/inventory/enums"
	inventorymodel "go_wp/internal/module/product/inventory/model"
	"go_wp/pkg/database"
)

const (
	// maxRequestIDLen 幂等键长度上限。
	maxRequestIDLen = 64
	// 进货历史分页。
	defaultHistoryPageSize = 50
	maxHistoryPageSize     = 200
)

// RegisterReceipt 登记采购收货（验收 3/4）：按行累加已入库数量，一次可收多行。
func (s *Service) RegisterReceipt(ctx context.Context, req *inventorydto.RegisterReceiptReq) (res *inventorydto.ReceiptResp, err error) {
	if req == nil {
		return nil, errors.New(inventoryenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	requestID, err := normalizeRequestID(req.RequestID)
	if err != nil {
		return nil, err
	}
	// ① 幂等重放：同一个键已有入库单 → 原样返回，不第二次动库存。
	replay, err := s.idempotentReceipt(ctx, projectID, requestID)
	if err != nil {
		return nil, err
	}
	if replay != nil {
		return s.receiptResp(ctx, replay, true)
	}
	if len(req.Lines) == 0 {
		return nil, errors.New(inventoryenums.ErrReceiptLinesRequired)
	}
	if len(req.Lines) > maxBatchLines {
		return nil, errors.New(inventoryenums.ErrStockLinesTooMany)
	}
	receivedAt, now := time.Now().UTC(), time.Now().UTC()
	if req.ReceivedAt != nil {
		receivedAt = req.ReceivedAt.UTC()
	}

	var (
		receipt    *inventorymodel.ReceiptEntity
		items      []*inventorymodel.ReceiptItemEntity
		stockLines []inventorydto.StockChangeLineReq
		order      *inventorymodel.PurchaseOrderEntity
	)
	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		// ②-1 锁采购单头：同一张单的并发收货在此串行化。
		locked, lerr := s.m.LockPurchaseOrderTx(ctx, tx, strings.TrimSpace(req.OrderID))
		if lerr != nil {
			return mapPurchaseOrderNotFound(lerr)
		}
		if locked.ProjectID != projectID {
			return errors.New(inventoryenums.ErrPurchaseOrderNotFound)
		}
		order = locked
		source, lerr := s.resolvePurchaseSource(ctx, projectID, locked.SourceID)
		if lerr != nil {
			return lerr
		}
		// 收货仓：本次指定 → 采购单上的收货仓（为空再兜底默认仓）。
		wantWarehouse := strings.TrimSpace(req.WarehouseID)
		if wantWarehouse == "" {
			wantWarehouse = locked.WarehouseID
		}
		wh, lerr := s.resolveWarehouse(ctx, projectID, wantWarehouse)
		if lerr != nil {
			return lerr
		}
		lines, lerr := s.m.ListPurchaseLinesTx(ctx, tx, locked.ID)
		if lerr != nil {
			return lerr
		}
		if len(lines) == 0 {
			return errors.New(inventoryenums.ErrPurchaseLinesRequired)
		}
		byID := make(map[string]*inventorymodel.PurchaseLineEntity, len(lines))
		outstanding := false
		for _, line := range lines {
			byID[line.ID] = line
			if line.ReceivedQuantity < line.Quantity {
				outstanding = true
			}
		}
		if !outstanding {
			// 全部收满的单不再受理（状态已是 received）。
			return errors.New(inventoryenums.ErrReceiptOrderDone)
		}
		receiptID := uuid.NewString()
		seq, cerr := s.m.CountOrderReceiptsTx(ctx, tx, locked.ID)
		if cerr != nil {
			return cerr
		}
		items = make([]*inventorymodel.ReceiptItemEntity, 0, len(req.Lines))
		stockLines = make([]inventorydto.StockChangeLineReq, 0, len(req.Lines))
		seenLineIDs := make(map[string]struct{}, len(req.Lines))
		for _, row := range req.Lines {
			lineID := strings.TrimSpace(row.LineID)
			if _, exists := seenLineIDs[lineID]; exists {
				return errors.New(inventoryenums.ErrReceiptLineDuplicate)
			}
			seenLineIDs[lineID] = struct{}{}
			line, ok := byID[lineID]
			if !ok {
				return errors.New(inventoryenums.ErrPurchaseLineNotFound)
			}
			if row.Quantity <= 0 {
				return errors.New(inventoryenums.ErrReceiptQuantityInvalid)
			}
			// 单价：本次实际到货价优先，缺省沿用采购行单价（成本价按它回写）。
			unitPrice := line.UnitPrice
			if row.UnitPrice != nil {
				if *row.UnitPrice <= 0 {
					return errors.New(inventoryenums.ErrPurchasePriceInvalid)
				}
				unitPrice = *row.UnitPrice
			}
			// ②-2 已入库数量原子递增：守卫写在 WHERE 里，受影响行数 0 即超收。
			affected, ierr := s.m.IncrPurchaseLineReceivedTx(ctx, tx, line.ID, row.Quantity, now)
			if ierr != nil {
				return ierr
			}
			if affected == 0 {
				return errors.New(inventoryenums.ErrReceiptOverReceive)
			}
			receiptLineID := line.ID
			items = append(items, &inventorymodel.ReceiptItemEntity{
				ID: uuid.NewString(), ReceiptID: receiptID, ProjectID: projectID,
				LineID: &receiptLineID, ProductID: line.ProductID, VariantID: line.VariantID,
				SKUCode: line.SKUCode, Quantity: row.Quantity, UnitPrice: unitPrice,
				CostUpdated: false, CostError: "", CreatedAt: now,
			})
			stockLines = append(stockLines, inventorydto.StockChangeLineReq{
				VariantID: line.VariantID, ProductID: line.ProductID,
				SKUCode: line.SKUCode, WarehouseID: wh.ID, Quantity: row.Quantity,
			})
		}
		// ②-3 状态由刚更新过的行重新推导（同一把锁之下，读到的是最新值）。
		fresh, lerr := s.m.ListPurchaseLinesTx(ctx, tx, locked.ID)
		if lerr != nil {
			return lerr
		}
		status := derivePurchaseStatus(fresh)
		receipt = &inventorymodel.ReceiptEntity{
			ID: receiptID, ProjectID: projectID,
			Code: fmt.Sprintf("%s-R%d", locked.Code, seq+1),
			Kind: inventoryenums.ReceiptKindPurchase, OrderID: &locked.ID,
			SourceID: source.ID, WarehouseID: wh.ID, RequestID: requestID,
			Status: inventoryenums.ReceiptStatusPending,
			Remark: strings.TrimSpace(req.Remark), OperatorID: strings.TrimSpace(req.OperatorID),
			ReceivedAt: receivedAt, Metadata: orJSON(nil, "{}"), CreatedAt: now,
		}
		if cerr := s.m.CreateReceiptTx(ctx, tx, receipt, items); cerr != nil {
			return cerr
		}
		order.Status = status
		return s.m.UpdatePurchaseOrderStatusTx(ctx, tx, locked.ID, status, now)
	})
	if err != nil {
		// 并发重复提交：两路同时通过 idempotentReceipt 预检，第二路在 request_id 唯一键上撞车。
		if database.IsUniqueViolation(err) {
			if replay, rerr := s.idempotentReceipt(ctx, projectID, requestID); rerr == nil && replay != nil {
				return s.receiptResp(ctx, replay, true)
			}
		}
		return nil, err
	}

	// ③ 库存变动：与 #16 完全同一套契约（真源行锁 + 流水 + 原因字典 + 来源引用）。
	// 重试保护：ChangeStock 成功后 SetReceiptMovement 失败时，下次重试先查流水是否已存在。
	var batchID string
	exists, merr := s.m.ExistsMovementBySource(ctx, projectID, inventoryenums.MovementSourcePurchaseOrder, order.Code)
	if merr != nil {
		_ = s.compensateReceipt(ctx, receipt.ID, order.ID, items)
		return nil, merr
	}
	if exists {
		rows, lerr := s.m.ListMovementRows(ctx, inventorymodel.MovementFilter{
			ProjectID: projectID, SourceType: inventoryenums.MovementSourcePurchaseOrder, SourceRef: order.Code,
		}, 1, 0)
		if lerr != nil || len(rows) == 0 {
			_ = s.compensateReceipt(ctx, receipt.ID, order.ID, items)
			return nil, lerr
		}
		batchID = rows[0].BatchID
	} else {
		change, cerr := s.ChangeStock(ctx, &inventorydto.ChangeStockReq{
			ProjectID: projectID, Direction: inventoryenums.DirectionIn,
			ReasonCode: reasonCodePurchaseIn,
			SourceType: inventoryenums.MovementSourcePurchaseOrder, SourceRef: order.Code,
			Remark: strings.TrimSpace(req.Remark), OperatorID: strings.TrimSpace(req.OperatorID),
			Lines: stockLines,
		})
		if cerr != nil {
			// ④ 库存没动成功：退回已入库数量并删除入库单（不留「记了账没动库存」）。
			_ = s.compensateReceipt(ctx, receipt.ID, order.ID, items)
			return nil, cerr
		}
		batchID = change.BatchID
	}
	// ⑤ 提交之后：记批次号 + 置 posted + 回写成本价。
	if serr := s.m.SetReceiptMovement(ctx, receipt.ID, batchID, inventoryenums.ReceiptStatusPosted); serr == nil {
		receipt.MovementBatchID = batchID
		receipt.Status = inventoryenums.ReceiptStatusPosted
	}
	s.applyReceiptCosts(ctx, items, receipt.OperatorID)
	return s.receiptResp(ctx, receipt, false)
}

// RegisterProductionInbound 自家工厂生产入库（验收 5）：无采购单，成本价手工填写。
//
// 与采购收货的唯一区别是「没有采购行」：来源必须是内部货源（自家工厂 / 集团内关联公司），
// 成本价由调用方手工给出 —— 生产没有采购单价可以引用。
func (s *Service) RegisterProductionInbound(ctx context.Context, req *inventorydto.ProductionInboundReq) (res *inventorydto.ReceiptResp, err error) {
	if req == nil {
		return nil, errors.New(inventoryenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	requestID, err := normalizeRequestID(req.RequestID)
	if err != nil {
		return nil, err
	}
	replay, err := s.idempotentReceipt(ctx, projectID, requestID)
	if err != nil {
		return nil, err
	}
	if replay != nil {
		return s.receiptResp(ctx, replay, true)
	}
	variantID := strings.TrimSpace(req.VariantID)
	if variantID == "" {
		return nil, errors.New(inventoryenums.ErrProductionVariantRequired)
	}
	if req.Quantity <= 0 {
		return nil, errors.New(inventoryenums.ErrReceiptQuantityInvalid)
	}
	if req.UnitCost == nil || *req.UnitCost < 0 {
		// 成本价必须手工填写：生产入库没有采购单价可引用，缺了就写不出成本口径。
		return nil, errors.New(inventoryenums.ErrProductionCostInvalid)
	}
	source, err := s.resolvePurchaseSource(ctx, projectID, req.SourceID)
	if err != nil {
		return nil, err
	}
	if source.Type != inventoryenums.SourceTypeInternal {
		// 外部供应商没有「无采购单的生产入库」这一说（它的路径是采购单）。
		return nil, errors.New(inventoryenums.ErrProductionSourceNotInternal)
	}
	wh, err := s.resolveWarehouse(ctx, projectID, req.WarehouseID)
	if err != nil {
		return nil, err
	}
	receivedAt, now := time.Now().UTC(), time.Now().UTC()
	if req.ReceivedAt != nil {
		receivedAt = req.ReceivedAt.UTC()
	}
	receiptID := uuid.NewString()
	code := productionReceiptCode(receiptID)
	receipt := &inventorymodel.ReceiptEntity{
		ID: receiptID, ProjectID: projectID, Code: code,
		Kind: inventoryenums.ReceiptKindProduction, OrderID: nil,
		SourceID: source.ID, WarehouseID: wh.ID, RequestID: requestID,
		Status: inventoryenums.ReceiptStatusPending,
		Remark: strings.TrimSpace(req.Remark), OperatorID: strings.TrimSpace(req.OperatorID),
		ReceivedAt: receivedAt, Metadata: orJSON(nil, "{}"), CreatedAt: now,
	}
	item := &inventorymodel.ReceiptItemEntity{
		ID: uuid.NewString(), ReceiptID: receiptID, ProjectID: projectID, LineID: nil,
		ProductID: strings.TrimSpace(req.ProductID), VariantID: variantID,
		SKUCode: strings.TrimSpace(req.SKUCode), Quantity: req.Quantity,
		UnitPrice: *req.UnitCost, CostUpdated: false, CostError: "", CreatedAt: now,
	}
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		return s.m.CreateReceiptTx(ctx, tx, receipt, []*inventorymodel.ReceiptItemEntity{item})
	}); err != nil {
		if database.IsUniqueViolation(err) {
			if replay, rerr := s.idempotentReceipt(ctx, projectID, requestID); rerr == nil && replay != nil {
				return s.receiptResp(ctx, replay, true)
			}
		}
		return nil, err
	}
	change, cerr := s.ChangeStock(ctx, &inventorydto.ChangeStockReq{
		ProjectID: projectID, Direction: inventoryenums.DirectionIn,
		ReasonCode: reasonCodeProductionIn,
		SourceType: inventoryenums.MovementSourceProduction, SourceRef: code,
		Remark: strings.TrimSpace(req.Remark), OperatorID: strings.TrimSpace(req.OperatorID),
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: variantID, ProductID: strings.TrimSpace(req.ProductID),
			SKUCode: strings.TrimSpace(req.SKUCode), WarehouseID: wh.ID, Quantity: req.Quantity,
		}},
	})
	if cerr != nil {
		_ = s.compensateReceipt(ctx, receipt.ID, "", []*inventorymodel.ReceiptItemEntity{item})
		return nil, cerr
	}
	if serr := s.m.SetReceiptMovement(ctx, receipt.ID, change.BatchID, inventoryenums.ReceiptStatusPosted); serr == nil {
		receipt.MovementBatchID = change.BatchID
		receipt.Status = inventoryenums.ReceiptStatusPosted
	}
	s.applyReceiptCosts(ctx, []*inventorymodel.ReceiptItemEntity{item}, receipt.OperatorID)
	return s.receiptResp(ctx, receipt, false)
}

// ListPurchaseHistory 某 SKU 的进货历史（验收 6）。
//
// 维度是 SKU（skuCode / variantId 二选一，都为空即列该工程的全部入库历史），
// 可按货源或采购单收窄；每行带单价快照、来源、收货仓与时间。
func (s *Service) ListPurchaseHistory(ctx context.Context, req *inventorydto.ListPurchaseHistoryReq) (list []*inventorydto.PurchaseHistoryResp, err error) {
	if req == nil {
		return nil, errors.New(inventoryenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	filter := inventorymodel.HistoryFilter{
		ProjectID: projectID,
		SKUCode:   strings.TrimSpace(req.SKUCode),
		VariantID: strings.TrimSpace(req.VariantID),
		SourceID:  strings.TrimSpace(req.SourceID),
		OrderID:   strings.TrimSpace(req.OrderID),
	}
	page, size := historyPageArgs(req.Page, req.Size)
	rows, err := s.m.ListHistoryRows(ctx, filter, size, (page-1)*size)
	if err != nil {
		return nil, err
	}
	list = make([]*inventorydto.PurchaseHistoryResp, 0, len(rows))
	for _, r := range rows {
		orderID := ""
		if r.OrderID != nil {
			orderID = *r.OrderID
		}
		list = append(list, &inventorydto.PurchaseHistoryResp{
			ReceiptID: r.ReceiptID, ReceiptCode: r.ReceiptCode, Kind: r.Kind,
			OrderID: orderID, OrderCode: r.OrderCode,
			SourceID: r.SourceID, SourceName: r.SourceName, SourceType: r.SourceType,
			WarehouseID: r.WarehouseID, WarehouseName: r.WarehouseName,
			VariantID: r.VariantID, SKUCode: r.SKUCode,
			Quantity: r.Quantity, UnitPrice: r.UnitPrice, CostUpdated: r.CostUpdated,
			MovementBatchID: r.MovementBatchID, Remark: r.Remark,
			OperatorID: r.OperatorID, ReceivedAt: r.ReceivedAt,
		})
	}
	return list, nil
}

// —— 内部工具 ——

// idempotentReceipt 幂等命中检查：命中返回既有入库单，未命中返回 (nil, nil)。
func (s *Service) idempotentReceipt(ctx context.Context, projectID, requestID string) (e *inventorymodel.ReceiptEntity, err error) {
	if requestID == "" {
		return nil, nil
	}
	got, gerr := s.m.FindReceiptByRequestID(ctx, projectID, requestID)
	if gerr == nil {
		return got, nil
	}
	if errors.Is(gerr, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return nil, gerr
}

// compensateReceipt 库存变动失败时的补偿：退回已入库数量、重算状态、删除入库单。
//
// 补偿本身失败只能记日志层面感知（调用方已经要返回原始错误）：这里把错误吞掉并返回，
// 因为此时「没有把库存动成」已经是确定的结果，残留的 pending 单据在后台可见、可人工核对。
func (s *Service) compensateReceipt(ctx context.Context, receiptID, orderID string, items []*inventorymodel.ReceiptItemEntity) (err error) {
	now := time.Now().UTC()
	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		for _, it := range items {
			if it.LineID == nil {
				continue
			}
			if _, derr := s.m.DecrPurchaseLineReceivedTx(ctx, tx, *it.LineID, it.Quantity, now); derr != nil {
				return derr
			}
		}
		if orderID != "" {
			lines, lerr := s.m.ListPurchaseLinesTx(ctx, tx, orderID)
			if lerr != nil {
				return lerr
			}
			if uerr := s.m.UpdatePurchaseOrderStatusTx(ctx, tx, orderID, derivePurchaseStatus(lines), now); uerr != nil {
				return uerr
			}
		}
		return s.m.DeleteReceiptTx(ctx, tx, receiptID)
	})
	return err
}

// applyReceiptCosts 入库单价 → SKU 成本价（验收 4：采购单价更新成本价；验收 5：手工成本）。
//
// 跨模块写发生在库存变动提交之后，失败不回滚真源，只把失败原因记在入库单行上
// （与 #16 的缓存同步同一口径）。
//
// operatorID（issue #19）：成本价回写会进商品侧的主数据变更记录，
// 记的操作人就是登记这次入库的人 —— 从会话带下来，不由客户端指定。
func (s *Service) applyReceiptCosts(ctx context.Context, items []*inventorymodel.ReceiptItemEntity, operatorID string) {
	for _, it := range items {
		if s.variantCost == nil {
			it.CostUpdated, it.CostError = false, inventoryenums.ErrVariantCostPortMissing
			_ = s.m.UpdateReceiptItemCost(ctx, it.ID, false, it.CostError)
			continue
		}
		if cerr := s.variantCost.UpdateVariantCost(ctx, it.VariantID, it.UnitPrice, operatorID); cerr != nil {
			it.CostUpdated, it.CostError = false, cerr.Error()
		} else {
			it.CostUpdated, it.CostError = true, ""
		}
		_ = s.m.UpdateReceiptItemCost(ctx, it.ID, it.CostUpdated, it.CostError)
	}
}

// receiptResp 组装入库单响应（行 + 货源 / 仓库 / 采购单展示信息）。
func (s *Service) receiptResp(ctx context.Context, e *inventorymodel.ReceiptEntity, idempotent bool) (res *inventorydto.ReceiptResp, err error) {
	items, err := s.m.ListReceiptItems(ctx, e.ID)
	if err != nil {
		return nil, err
	}
	resp := &inventorydto.ReceiptResp{
		ID: e.ID, Code: e.Code, Kind: e.Kind, SourceID: e.SourceID,
		WarehouseID: e.WarehouseID, Status: e.Status, MovementBatchID: e.MovementBatchID,
		Remark: e.Remark, OperatorID: e.OperatorID, ReceivedAt: e.ReceivedAt,
		Idempotent: idempotent, CostUpdated: true,
		Items: make([]*inventorydto.ReceiptItemResp, 0, len(items)),
	}
	if e.OrderID != nil {
		resp.OrderID = *e.OrderID
		if order, oerr := s.m.GetPurchaseOrder(ctx, *e.OrderID); oerr == nil {
			resp.OrderCode = order.Code
		}
	}
	if source, serr := s.m.GetSource(ctx, e.SourceID); serr == nil {
		resp.SourceName = source.Name
	}
	if wh, werr := s.m.GetWarehouse(ctx, e.WarehouseID); werr == nil {
		resp.WarehouseName = wh.Name
	}
	for _, it := range items {
		lineID := ""
		if it.LineID != nil {
			lineID = *it.LineID
		}
		if !it.CostUpdated {
			resp.CostUpdated = false
		}
		resp.Items = append(resp.Items, &inventorydto.ReceiptItemResp{
			LineID: lineID, ProductID: it.ProductID, VariantID: it.VariantID,
			SKUCode: it.SKUCode, Quantity: it.Quantity, UnitPrice: it.UnitPrice,
			CostUpdated: it.CostUpdated, CostError: it.CostError,
		})
	}
	return resp, nil
}

// normalizeRequestID 归一幂等键（空串表示不用幂等保护；给了就必须是安全字符集）。
func normalizeRequestID(raw string) (out string, err error) {
	out = strings.TrimSpace(raw)
	if out == "" {
		return "", nil
	}
	if len(out) > maxRequestIDLen {
		return "", errors.New(inventoryenums.ErrReceiptRequestInvalid)
	}
	for _, r := range out {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == '_' || r == '-' {
			continue
		}
		return "", errors.New(inventoryenums.ErrReceiptRequestInvalid)
	}
	return out, nil
}

// productionReceiptCode 生产入库单号（无采购单号可依附，用自身 id 的前 8 位）。
func productionReceiptCode(receiptID string) (code string) {
	short := strings.ToUpper(strings.ReplaceAll(receiptID, "-", ""))
	if len(short) > 8 {
		short = short[:8]
	}
	return "PROD-" + short
}

// historyPageArgs 归一进货历史分页参数。
func historyPageArgs(page, size int) (outPage, outSize int) {
	outPage, outSize = 1, defaultHistoryPageSize
	if page > 0 {
		outPage = page
	}
	if size > 0 {
		outSize = size
		if outSize > maxHistoryPageSize {
			outSize = maxHistoryPageSize
		}
	}
	return outPage, outSize
}
