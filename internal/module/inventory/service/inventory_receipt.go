// inventory_receipt.go — 入库登记：采购收货 / 自家工厂生产入库 / 进货历史（issue #18 验收 3/4/5/6）。
//
// 入库的事务形状（顺序是刻意的，不要重排）：
//
//	① 登记前：幂等键命中即原样返回（可重放保护，不第二次动库存）；
//	② **一个事务**里的全部写入：锁采购单头 → 按行原子递增已入库数量（守卫在 UPDATE 的
//	   WHERE 里）→ 写入库单与入库行 → 库存变动（复用 #16 的 ChangeStockTx，方向 in +
//	   原因字典 + 来源引用齐全，绝不旁路写 inventory_stocks）→ 单据置 posted 并记批次号
//	   → 用最新行重算推导状态。任一步失败整体回滚；
//	③ 提交后：经商品模块端口回写 SKU 成本价（那是商品模块的写，端口没有 Tx 形态，
//	   只能留在事务之外 —— 失败记在入库单行上，并可由幂等重放补做）。
//
// **跨模块的 DB 补偿已删除**（原 compensateReceipt / ④）：补偿只留给跨库 / 外部系统。
// 库存变动与本模块的记账在同一库同一事务里，失败直接整体回滚就是最干净的补偿 ——
// 原来「先提交记账、再动库存、失败再退回数量并删单」的写法一旦补偿也失败，就会留下
// 「记了账没动库存」，而错误还被 `_ =` 吞掉。
//
// 成本有**两条**写路径，顺序与归属都不同（2026-09-19 批次 A 收口，docs/14 §4）：
//
//	· 仓库侧（(仓库, SKU) 的当前成本价，迁移 244）—— 本次到货价随库存变动进
//	  ② 的同一条事务（见下面 stockLines 的 CostPrice），是本模块的真源；
//	· 商品侧（product_variants.cost_price）—— 提交后经 VariantCostPort 的独立步骤，
//	  属商品模块的兼容写回，失败只记在入库单行上。
//
// 之所以前者不能也放到提交后：那是同一张表的另一列，塞进提交后的独立步骤只会造出
// 「货到了、成本没写」的中间态，而这个中间态对采购侧核算毫无意义。
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
	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	inventorymodel "go_wp/internal/module/inventory/model"
	"go_wp/pkg/database"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
	"gorm.io/gorm"
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
		// 顺带补做上次没写成的**商品侧**成本回写（跨模块 best-effort，幂等：已写成的行跳过）。
		// 这就是 applyReceiptCosts 的重放入口 —— 重试同一 request_id 是它的自然触发方式。
		s.replayReceiptCosts(ctx, replay)
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
		receivedAt = req.ReceivedAt.Time().UTC()
	}

	var (
		receipt    *inventorymodel.ReceiptEntity
		items      []*inventorymodel.ReceiptItemEntity
		stockLines []inventorydto.StockChangeLineReq
	)
	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		// ②-1 锁采购单头：同一张单的并发收货在此串行化。
		locked, lerr := s.m.LockPurchaseOrderTx(ctx, tx, strings.TrimSpace(req.OrderID), projectID)
		if lerr != nil {
			return mapPurchaseOrderNotFound(lerr)
		}
		if locked.ProjectID != projectID {
			return errors.New(inventoryenums.ErrPurchaseOrderNotFound)
		}
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
		lines, lerr := s.m.ListPurchaseLinesTx(ctx, tx, locked.ID, projectID)
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
		seq, cerr := s.m.CountOrderReceiptsTx(ctx, tx, locked.ID, projectID)
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
			// 仓库侧编码归一旦校验（口径与理由见 inventory_stock_sku.go）：采购行上的
			// sku_code 是下单时的快照，本批收货的目标仓可能不是下单时的那个仓，
			// 因此按**本次收货仓**再归一一次（幂等，裸码原样通过），空串一律拒绝。
			skuCode, nerr := normalizeStockSKU(line.SKUCode, wh.Code)
			if nerr != nil {
				return nerr
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
			affected, ierr := s.m.IncrPurchaseLineReceivedTx(ctx, tx, line.ID, row.Quantity, projectID, now)
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
				SKUCode: skuCode, Quantity: row.Quantity, UnitPrice: unitPrice,
				CostUpdated: false, CostError: "", CreatedAt: now,
			})
			// 成本落到**仓库侧**（(仓库, SKU) 的当前值，迁移 244）：本次到货价
			// （未显式给就是采购行单价）随库存变动进同一条事务 —— 货与成本一起生效，
			// 不依赖提交之后的步骤，也就不会出现「货到了、成本还是空的」。
			lineCost := unitPrice
			stockLines = append(stockLines, inventorydto.StockChangeLineReq{
				VariantID: line.VariantID, ProductID: line.ProductID,
				SKUCode: skuCode, WarehouseID: wh.ID, Quantity: row.Quantity,
				CostPrice: &lineCost,
			})
		}
		// ②-3 状态由刚更新过的行重新推导（同一把锁之下，读到的是最新值）。
		fresh, lerr := s.m.ListPurchaseLinesTx(ctx, tx, locked.ID, projectID)
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
		if uerr := s.m.UpdatePurchaseOrderStatusTx(ctx, tx, locked.ID, status, projectID, now); uerr != nil {
			return uerr
		}

		// ②-4 库存变动：**同一个事务**（changeStockTrackingTx 用调用方的 tx，不自己开事务、
		// 错误原样返回）。这一处就是本次收口的核心 —— 原先它在事务提交之后跑，
		// 失败只能靠「退回已入库数量 + 删入库单」补偿，而补偿本身也被吞掉。
		//
		// 这里用模块内的 changeStockTrackingTx 而不是导出的 ChangeStockTx：后者按契约只回
		// error，而入库单要把这次变动的**批次号**写进同一行，用内部形态省一次回读。
		//
		// 不做「按采购单号查流水是否已存在」的判重：一张采购单可以分多次收货（部分到货是
		// 常态），第二批会命中第一批的流水而被误判成「已经入过库」，结果是库存不加、单据
		// 状态却推进 —— 账实不符，且没有任何报错。真正的重试保护在入口：同一 request_id
		// 命中既有入库单直接原样返回（idempotentReceipt），连库存变动都不会走到。
		change, _, _, cerr := s.changeStockTrackingTx(ctx, tx, &inventorydto.ChangeStockReq{
			ProjectID: projectID, Direction: inventoryenums.DirectionIn,
			ReasonCode: reasonCodePurchaseIn,
			SourceType: inventoryenums.MovementSourcePurchaseOrder, SourceRef: locked.Code,
			Remark: strings.TrimSpace(req.Remark), OperatorID: strings.TrimSpace(req.OperatorID),
			Lines: stockLines,
		}, nil)
		if cerr != nil {
			return cerr
		}

		// ②-5 单据置 posted 并记下批次号：仍在同一事务里。原先这一步在提交之后，
		// 而且错误被 `if serr := ...; serr == nil` 静默吞掉 —— 单据会停在 pending
		// 而没人知道（库存已经动了）。
		receipt.MovementBatchID = change.batchID
		receipt.Status = inventoryenums.ReceiptStatusPosted
		return s.m.SetReceiptMovementTx(ctx, tx, receipt.ID, change.batchID,
			inventoryenums.ReceiptStatusPosted, projectID)
	})
	if err != nil {
		// 并发重复提交：两路同时通过 idempotentReceipt 预检，第二路在 request_id 唯一键上撞车。
		if database.IsUniqueViolation(err) {
			if replay, rerr := s.idempotentReceipt(ctx, projectID, requestID); rerr == nil && replay != nil {
				s.replayReceiptCosts(ctx, replay)
				return s.receiptResp(ctx, replay, true)
			}
		}
		return nil, err
	}

	// ③ 提交之后：商品侧成本价回写（跨模块，见 applyReceiptCosts —— 留痕 + 可重放）。
	s.applyReceiptCosts(ctx, items, receipt.OperatorID)
	return s.receiptResp(ctx, receipt, false)
}

// RegisterProductionInbound 自家工厂生产入库（验收 5）：无采购单，成本价手工填写。
//
// 与采购收货的唯一区别是「没有采购行」：来源必须是内部货源（自家工厂 / 集团内关联公司），
// 成本价由调用方手工给出 —— 生产没有采购单价可以引用。
//
// 手工成本同样是**显式成本**：按批次 A 的口径（「其它入库不动成本，除非显式传成本」）
// 落到 (仓库, SKU) 的当前值上，与采购收货共用同一条写入路径。
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
		// 与采购收货同一口径：重放顺带补做上次没写成的商品侧成本回写。
		s.replayReceiptCosts(ctx, replay)
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
	// 仓库侧编码归一旦校验（口径与理由见 inventory_stock_sku.go）：生产入库没有采购行，
	// 编码只能来自调用方（页面 / 接口），这里就是它的唯一入口 —— 空串一律拒绝，
	// 带仓码前缀的按目标仓短码幂等剥掉。
	skuCode, err := normalizeStockSKU(req.SKUCode, wh.Code)
	if err != nil {
		return nil, err
	}
	receivedAt, now := time.Now().UTC(), time.Now().UTC()
	if req.ReceivedAt != nil {
		receivedAt = req.ReceivedAt.Time().UTC()
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
		SKUCode: skuCode, Quantity: req.Quantity,
		UnitPrice: *req.UnitCost, CostUpdated: false, CostError: "", CreatedAt: now,
	}
	// 与采购收货同一个事务形状：建入库单 + 库存变动 + 单据置 posted 与批次号，
	// 任一步失败整体回滚（没有跨模块 DB 补偿）。
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if cerr := s.m.CreateReceiptTx(ctx, tx, receipt, []*inventorymodel.ReceiptItemEntity{item}); cerr != nil {
			return cerr
		}
		change, _, _, cerr := s.changeStockTrackingTx(ctx, tx, &inventorydto.ChangeStockReq{
			ProjectID: projectID, Direction: inventoryenums.DirectionIn,
			ReasonCode: reasonCodeProductionIn,
			SourceType: inventoryenums.MovementSourceProduction, SourceRef: code,
			Remark: strings.TrimSpace(req.Remark), OperatorID: strings.TrimSpace(req.OperatorID),
			Lines: []inventorydto.StockChangeLineReq{{
				VariantID: variantID, ProductID: strings.TrimSpace(req.ProductID),
				SKUCode: skuCode, WarehouseID: wh.ID, Quantity: req.Quantity,
				// 生产入库的成本是手工填的（没有采购单价可引用），按**显式成本**落到
				// (仓库, SKU) 的当前值上 —— 与采购收货同一条写入路径。
				CostPrice: req.UnitCost,
			}},
		}, nil)
		if cerr != nil {
			return cerr
		}
		receipt.MovementBatchID = change.batchID
		receipt.Status = inventoryenums.ReceiptStatusPosted
		return s.m.SetReceiptMovementTx(ctx, tx, receipt.ID, change.batchID,
			inventoryenums.ReceiptStatusPosted, projectID)
	}); err != nil {
		if database.IsUniqueViolation(err) {
			if replay, rerr := s.idempotentReceipt(ctx, projectID, requestID); rerr == nil && replay != nil {
				s.replayReceiptCosts(ctx, replay)
				return s.receiptResp(ctx, replay, true)
			}
		}
		return nil, err
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
			OperatorID: r.OperatorID, ReceivedAt: utils.NewJSONTime(r.ReceivedAt),
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

// applyReceiptCosts 入库单价 → **商品侧** SKU 成本价（验收 4；验收 5：手工成本）。
//
// 注意这里只是商品侧（product_variants.cost_price）的兼容写回：本轮起成本的主载体是
// **仓库侧**的 (仓库, SKU) 当前成本价（inventory_stocks.cost_price，迁移 244），
// 它已经在入库事务里随库存一起写好（见 RegisterReceipt 的 stockLines）——
// 本函数不负责它，也不该被当成「成本写入是否成功」的唯一判据。
//
// **为什么它在事务之外**：这是**另一个模块**（商品域）的写，端口 VariantCostPort 只有
// 非 Tx 形态、实现也在商品模块内自行开事务 —— 跨模块句柄传不过去，所以按 AGENTS.md
// 「补偿只用于跨库/外部系统」的口径，它属于事务边界之外的 best-effort 动作：必须
// **幂等 + 留痕 + 可重放**，三样都在这里：
//
//	· 幂等：已写成（CostUpdated = true）的行直接跳过，重复调用不会二次改写；
//	· 留痕：结果写进入库单行的 cost_updated / cost_error 列（后台可见），
//	  连这一列本身写失败也要落日志（不再用 `_ =` 把错误丢掉）；
//	· 可重放：入口是 replayReceiptCosts（幂等重放同一 request_id 时触发）。
//
// operatorID（issue #19）：成本价回写会进商品侧的主数据变更记录，
// 记的操作人就是登记这次入库的人 —— 从会话带下来，不由客户端指定。
func (s *Service) applyReceiptCosts(ctx context.Context, items []*inventorymodel.ReceiptItemEntity, operatorID string) {
	for _, it := range items {
		// 幂等守卫：上一次已经写成的行不再重写（重放路径会带上全部行）。
		if it.CostUpdated {
			continue
		}
		if s.variantCost == nil {
			it.CostUpdated, it.CostError = false, inventoryenums.ErrVariantCostPortMissing
			s.recordReceiptCost(ctx, it)
			continue
		}
		if cerr := s.variantCost.UpdateVariantCost(ctx, it.ProjectID, it.VariantID, it.UnitPrice, operatorID); cerr != nil {
			it.CostUpdated, it.CostError = false, cerr.Error()
		} else {
			it.CostUpdated, it.CostError = true, ""
		}
		s.recordReceiptCost(ctx, it)
	}
}

// recordReceiptCost 把一次成本回写的结果记进入库单行（留痕）。
//
// 写失败不再静默吞掉：这是「跨模块 best-effort」的台账，丢了它就再也说不清
// 某一行到底回写成功没有（而库存已经在事务里动过了）。
func (s *Service) recordReceiptCost(ctx context.Context, it *inventorymodel.ReceiptItemEntity) {
	if err := s.m.UpdateReceiptItemCost(ctx, it.ID, it.CostUpdated, it.CostError, it.ProjectID); err != nil {
		logger.Scene("inventory").With("receipt_item_id", it.ID).With("variant_id", it.VariantID).
			Error(err, "入库单行的成本回写结果写入失败（该行的 cost_updated 与事实不符，需人工核对）")
	}
}

// replayReceiptCosts 幂等重放入口：把某张入库单上**还没写成**的商品侧成本补做一遍。
//
// 触发方式是同一 request_id 的重试（幂等命中既有入库单时调用）—— 这是运维手上
// 现成且安全的重放手段：applyReceiptCosts 有幂等守卫，已写成的行不会被动第二次。
// 它是事务外动作可接受的唯一理由（跨模块写 + 端口没有 Tx 形态），见 applyReceiptCosts。
func (s *Service) replayReceiptCosts(ctx context.Context, receipt *inventorymodel.ReceiptEntity) {
	if receipt == nil {
		return
	}
	items, err := s.m.ListReceiptItems(ctx, receipt.ID, receipt.ProjectID)
	if err != nil {
		logger.Scene("inventory").With("receipt_id", receipt.ID).
			Error(err, "重放成本回写前读取入库单行失败")
		return
	}
	s.applyReceiptCosts(ctx, items, receipt.OperatorID)
}

// receiptResp 组装入库单响应（行 + 货源 / 仓库 / 采购单展示信息）。
func (s *Service) receiptResp(ctx context.Context, e *inventorymodel.ReceiptEntity, idempotent bool) (res *inventorydto.ReceiptResp, err error) {
	items, err := s.m.ListReceiptItems(ctx, e.ID, e.ProjectID)
	if err != nil {
		return nil, err
	}
	resp := &inventorydto.ReceiptResp{
		ID: e.ID, Code: e.Code, Kind: e.Kind, SourceID: e.SourceID,
		WarehouseID: e.WarehouseID, Status: e.Status, MovementBatchID: e.MovementBatchID,
		Remark: e.Remark, OperatorID: e.OperatorID, ReceivedAt: utils.NewJSONTime(e.ReceivedAt),
		Idempotent: idempotent, CostUpdated: true,
		Items: make([]*inventorydto.ReceiptItemResp, 0, len(items)),
	}
	if e.OrderID != nil {
		resp.OrderID = *e.OrderID
		if order, oerr := s.m.GetPurchaseOrder(ctx, *e.OrderID, e.ProjectID); oerr == nil {
			resp.OrderCode = order.Code
		}
	}
	if source, serr := s.m.GetSource(ctx, e.SourceID, e.ProjectID); serr == nil {
		resp.SourceName = source.Name
	}
	if wh, werr := s.m.GetWarehouse(ctx, e.WarehouseID, e.ProjectID); werr == nil {
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
