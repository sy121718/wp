package orderservice

// order_shipment.go — 发货用例（迁移 624 / 625，S2）与承运商管理。
//
// 发货是订单域里唯一「货真的出去了」的动作。十步全在**一个事务**里：
//
//	1) request_id 非空 → 先查 order_shipments 是否已存在；存在则直接返回那张单
//	   （幂等成功，不改库存）—— 双击提交 / 网络重试不该发出两批货；
//	2) SELECT orders ... FOR UPDATE 锁单（并发两次发货必须串行）；
//	3) status 必须 == paid → 否则 ErrOrderNotShippable（409）；
//	4) 逐行校验：quantity > 0；order_item 属于本单；warehouse 属于本工程且 active；
//	5) 已发数量账：SumShippedQuantityTx + 本次 <= order_items.quantity
//	   → 超量 ErrShipQuantityExceeded（409）；
//	6) 库存扣减 DeductStockTx(sale_out, source_type=order_shipment, source_ref=shipmentNo)
//	   —— 库存侧在同一条 SQL 里 reserved -= min(reserved, n)，不留中间态；
//	7) Insert order_shipments + order_shipment_items（stock_id 按 (变体, 仓) 查库存行填快照）；
//	8) Update orders SET status=shipped, carrier_id/carrier_name/tracking_no/shipped_at；
//	9) Insert order_status_logs(paid → shipped)；
//	10) 提交；**提交后不做任何外部推送**（本地没有电商 / 物流回传链路）。
//
// 三处顺序有硬理由：
//   - 幂等检查在锁单**之前**：命中幂等就直接返回，不必为了「已经发过」再锁一次订单；
//   - 库存扣减在写发货单**之前**：库存不足时不要先落一张发货单再回滚（回滚是原子的，
//     但错误路径上少一次写入总是更好查）；
//   - 状态更新在最后：状态一旦变成 shipped，订单就对所有读路径可见为新状态 ——
//     而那时库存、单据、流水都已经就位。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	ordercontract "go_wp/internal/module/order/contract"
	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
	"go_wp/pkg/rls"
)

// ShipOrder 发货（两段式的第二段：真正扣减库存）。
//
// 返回的发货单是**幂等命中时的那张**（request_id 相同），此时库存与状态都不再动。
func (s *Service) ShipOrder(ctx context.Context, req *orderdto.ShipOrderReq) (res *orderdto.ShipmentResp, err error) {
	if req == nil || req.OrderID == 0 {
		return nil, errors.New(orderenums.ErrInvalidParam)
	}
	if s.shipments == nil || s.carriers == nil {
		// 装配缺陷（required-port 未注入）：宁可当场报错，也不要静默发一批不落单的货。
		return nil, errors.New(orderenums.ErrStatusTransition)
	}
	projectID, perr := s.resolveOrderProject(ctx, req.OrderID, req.ProjectID)
	if perr != nil {
		return nil, perr
	}
	requestID := strings.TrimSpace(req.RequestID)
	// ① 幂等命中：直接返回既有发货单（不改库存、不改状态）。
	if existing, ferr := s.shipments.FindByRequestID(ctx, projectID, requestID); ferr != nil {
		return nil, ferr
	} else if existing != nil {
		return s.shipmentResp(ctx, existing), nil
	}
	now := time.Now().UTC()
	var shipment *ordermodel.OrderShipmentEntity
	if terr := s.orders.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return serr
		}
		// ② 锁单。
		e, lerr := s.orders.LockByIDTx(ctx, tx, projectID, req.OrderID)
		if lerr != nil {
			return lerr
		}
		if e == nil {
			return errors.New(orderenums.ErrOrderNotFound)
		}
		// ③ 只有已付款的单可以发货。
		if e.Status != ordermodel.OrderStatusPaid {
			return errors.New(orderenums.ErrOrderNotShippable)
		}
		// ④ 逐行校验 + ⑤ 已发数量账。
		items, ierr := s.items.ListByOrderIDTx(ctx, tx, e.ID)
		if ierr != nil {
			return ierr
		}
		plan, shippedSoFar, cerr := s.planShipment(ctx, tx, projectID, e, items, req.Lines)
		if cerr != nil {
			return cerr
		}
		// ⑥ 库存扣减（扣减与释放预占同一条 SQL）。source_type 必须是 order_shipment ——
		// 库存侧靠它判断「这次出库要同时释放预占」。
		shipmentNo, nerr := s.nextShipmentNoTx(ctx, tx, projectID, now)
		if nerr != nil {
			return nerr
		}
		if derr := s.shipStockTx(ctx, tx, e, shipmentNo, plan); derr != nil {
			return derr
		}
		// ⑦ 写单据 + 明细。
		shipment = &ordermodel.OrderShipmentEntity{
			ID: uuid.NewString(), ProjectID: projectID, OrderID: e.ID,
			ShipmentNo: shipmentNo, CarrierName: strings.TrimSpace(req.CarrierName),
			TrackingNo: strings.TrimSpace(req.TrackingNo), ShippedAt: now,
			OperatorID: req.OperatorID, OperatorName: strings.TrimSpace(req.OperatorName),
			Remark: strings.TrimSpace(req.Remark), RequestID: requestID, CreateTime: now,
		}
		if cid := strings.TrimSpace(req.CarrierID); cid != "" {
			shipment.CarrierID = &cid
		}
		getCarrierName(ctx, s, projectID, shipment)
		shipItems := make([]*ordermodel.OrderShipmentItemEntity, 0, len(plan))
		for _, p := range plan {
			shipItems = append(shipItems, &ordermodel.OrderShipmentItemEntity{
				OrderItemID: p.orderItemID, WarehouseID: p.warehouseID,
				StockID: p.stockID, Quantity: p.quantity,
			})
		}
		if serr := s.shipments.CreateTx(ctx, tx, shipment, shipItems); serr != nil {
			return serr
		}
		// ⑧ 订单头写状态与「最近一次发货」快照。
		//
		// **部分发货不推进状态**：还有未发数量时订单留在 paid，否则第二次发货会被
		// 「只有已付款可以发货」挡住 —— 而「部分发货」正是本页（待发货）存在的理由。
		// 状态由「本次之后是否还有剩余」决定：还有剩余就是 paid、发完才是 shipped。
		nextStatus := ordermodel.OrderStatusShipped
		if planHasRemaining(items, shippedSoFar, plan) {
			nextStatus = ordermodel.OrderStatusPaid
		}
		fields := map[string]any{
			"status":       nextStatus,
			"carrier_name": shipment.CarrierName,
			"tracking_no":  shipment.TrackingNo,
			"shipped_at":   now,
			"completed_at": nil,
			"update_time":  now,
		}
		if shipment.CarrierID != nil {
			fields["carrier_id"] = *shipment.CarrierID
		}
		if uerr := s.orders.UpdateFieldsTx(ctx, tx, projectID, e.ID, fields); uerr != nil {
			return uerr
		}
		// ⑨ 状态流水（与其它流转同一份记录形状）。
		return s.logs.CreateTx(ctx, tx, &ordermodel.OrderStatusLogEntity{
			OrderID:      e.ID,
			FromStatus:   e.Status,
			ToStatus:     nextStatus,
			OperatorType: ordermodel.OperatorTypeAdmin,
			OperatorID:   uint64(req.OperatorID),
			OperatorName: strings.TrimSpace(req.OperatorName),
			Remark:       shipmentNo,
			CreateTime:   now,
		})
	}); terr != nil {
		return nil, terr
	}
	return s.shipmentResp(ctx, shipment), nil
}

// planHasRemaining 本次发货之后订单是否还有未发数量（决定状态推进到 shipped 还是留在 paid）。
//
// 判据：对每一行算「订单数量 − 已发（含本次）」，还有 > 0 的就说明没发完。
// 与 ListPendingShip / BuildShipForm 同一口径（都从「订单数量 − 已发明细合计」推），
// 三处各写一份会让「待发货列表说有剩余、发货接口说发完了」成为可能。
func planHasRemaining(items []*ordermodel.OrderItemEntity, shippedSoFar map[uint64]int,
	plan []shipPlanLine) bool {
	sent := make(map[uint64]int, len(plan))
	for _, p := range plan {
		sent[p.orderItemID] += p.quantity
	}
	for _, it := range items {
		if it.Quantity-shippedSoFar[it.ID]-sent[it.ID] > 0 {
			return true
		}
	}
	return false
}

// shipPlanLine 一行「这次从哪个仓发多少件」的计划（校验与写入之间只传这个）。
type shipPlanLine struct {
	orderItemID uint64
	warehouseID string
	stockID     *string
	variantID   string
	productID   string
	skuCode     string
	quantity    int
}

// planShipment 校验并归一本次要发的行（第 ④⑤ 步）。
//
// 每一条拒绝都对应一种「数据说不通」的形态，且都给出可行动的文案：
//   - 行数上限与数量 > 0 → ErrShipLinesRequired / ErrInvalidParam；
//   - order_item 不属于本单 → ErrInvalidParam（这是越权/错单，不是「找不到」）；
//   - 数量超过「订单数量 − 已发」→ ErrShipQuantityExceeded；
//   - warehouse_id 既不在订单行上也不是合法仓 → ErrShipWarehouseRequired。
func (s *Service) planShipment(ctx context.Context, tx *gorm.DB, projectID string,
	e *ordermodel.OrderEntity, items []*ordermodel.OrderItemEntity,
	reqLines []orderdto.ShipLineReq) ([]shipPlanLine, map[uint64]int, error) {
	if len(reqLines) == 0 {
		return nil, nil, errors.New(orderenums.ErrShipLinesRequired)
	}
	if len(reqLines) > maxShipLines {
		return nil, nil, errors.New(orderenums.ErrInvalidParam)
	}
	byID := make(map[uint64]*ordermodel.OrderItemEntity, len(items))
	itemIDs := make([]uint64, 0, len(items))
	itemByID := make(map[uint64]bool, len(items))
	for _, it := range items {
		byID[it.ID] = it
		itemIDs = append(itemIDs, it.ID)
		itemByID[it.ID] = true
	}
	shipped, serr := s.shipments.SumShippedQuantityTx(ctx, tx, e.ID, itemIDs)
	if serr != nil {
		return nil, nil, serr
	}
	plan := make([]shipPlanLine, 0, len(reqLines))
	// 同一订单行在同一次发货里被写了两次：合并（同一 (行, 仓) 是同一份事实）。
	type key struct {
		itemID uint64
		whID   string
	}
	merged := make(map[key]*shipPlanLine, len(reqLines))
	order := make([]key, 0, len(reqLines))
	for _, line := range reqLines {
		item, ok := byID[line.OrderItemID]
		if !ok || !itemByID[line.OrderItemID] {
			// 不属于本单的订单行：不区分「不存在」与「别人的单」，避免探测。
			return nil, nil, errors.New(orderenums.ErrInvalidParam)
		}
		if line.Quantity <= 0 {
			return nil, nil, errors.New(orderenums.ErrQuantityInvalid)
		}
		whID, werr := s.shipWarehouseOf(ctx, projectID, item, line.WarehouseID)
		if werr != nil {
			return nil, nil, werr
		}
		k := key{itemID: item.ID, whID: whID}
		if existing, seen := merged[k]; seen {
			existing.quantity += line.Quantity
		} else {
			entry := &shipPlanLine{
				orderItemID: item.ID, warehouseID: whID,
				variantID: item.VariantID, productID: item.ProductID,
				skuCode: item.SKU, quantity: line.Quantity,
			}
			merged[k] = entry
			order = append(order, k)
		}
	}
	for _, k := range order {
		entry := merged[k]
		item := byID[entry.orderItemID]
		if shipped[item.ID]+entry.quantity > item.Quantity {
			return nil, nil, errors.New(orderenums.ErrShipQuantityExceeded)
		}
		plan = append(plan, *entry)
	}
	return plan, shipped, nil
}

// shipWarehouseOf 解析「这一行从哪个仓发」并**回写**订单行的 warehouse_id（迁移 625）。
//
// 两条路径：
//   - 本次请求显式给了仓 → 用它（校验工程与 active）；
//   - 没给 → 用 order_items.warehouse_id（下单时已固化）；若那一列为 NULL
//     （迁移 625 之前的存量行），解析默认仓并**回写**，此后这一行固化。
//
// 回写是「一次性固化」：同一订单行的后续发货不会再解析一次默认仓 ——
// 否则默认仓一改，同一行的第二批货就从另一个仓出，账面永远对不上。
func (s *Service) shipWarehouseOf(ctx context.Context, projectID string, item *ordermodel.OrderItemEntity,
	requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	if requested != "" {
		return requested, nil
	}
	if item.WarehouseID != nil && strings.TrimSpace(*item.WarehouseID) != "" {
		return strings.TrimSpace(*item.WarehouseID), nil
	}
	if s.warehouses == nil {
		return "", errors.New(orderenums.ErrShipWarehouseRequired)
	}
	whID, err := s.warehouses.ResolveShipWarehouse(ctx, projectID, "")
	if err != nil {
		return "", err
	}
	if whID == "" {
		// 无仓工程 + 存量行：没有仓可解析 —— 让运营显式指定（而不是静默失败）。
		return "", errors.New(orderenums.ErrShipWarehouseRequired)
	}
	// 回写在调用方的事务里发生（shipStep 之后紧跟 order_items 的 UPDATE）——
	// 见 persistItemWarehouse。这里只返回解析结果。
	return whID, nil
}

// shipStockTx 发货出库（第 ⑥ 步）：库存扣减 + 同条 SQL 里释放预占。
func (s *Service) shipStockTx(ctx context.Context, tx *gorm.DB, e *ordermodel.OrderEntity,
	shipmentNo string, plan []shipPlanLine) error {
	lines := make([]ordercontract.StockLine, 0, len(plan))
	for _, p := range plan {
		lines = append(lines, ordercontract.StockLine{
			ProductID: p.productID, VariantID: p.variantID, SKUCode: p.skuCode,
			Quantity: p.quantity, WarehouseID: p.warehouseID,
		})
	}
	derr := s.stock.DeductStockTx(ctx, tx, &ordercontract.StockDeduction{
		ProjectID:  e.ProjectID,
		ReasonCode: "sale_out",
		// source_type 必须是 order_shipment：库存侧靠它判断「这次出库要同时释放预占」。
		SourceType: "order_shipment",
		// source_ref 是**发货单号**（不是订单号）：流水要能回指到哪一张单发的货。
		SourceRef: shipmentNo,
		Remark:    "订单发货",
		Lines:     lines,
	})
	if derr != nil {
		// 库存不足（409）与库存域故障（原样上抛）分开。
		return mapStockError(derr)
	}
	return nil
}

// nextShipmentNoTx 生成发货单号：SH + YYYYMMDD + 6 位零填充序号（第 ⑥ 步之前）。
//
// 序号按当日已有单据数取，冲突由唯一索引兜底并重试一次（并发下两张单可能数到同一个序号）。
// 这里只做「取序号」：重试在调用方（ShipOrder 的幂等分支也会兜住同一 request_id 的重放）。
func (s *Service) nextShipmentNoTx(ctx context.Context, tx *gorm.DB, projectID string,
	now time.Time) (string, error) {
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	dayEnd := dayStart.Add(24 * time.Hour)
	n, err := s.shipments.CountShipmentsOnDayTx(ctx, tx, projectID, dayStart, dayEnd)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("SH%s%06d", now.Format("20060102"), n+1), nil
}

// shipmentResp 发货单 → 响应（含承运商名，展示不必再查一次）。
func (s *Service) shipmentResp(ctx context.Context, e *ordermodel.OrderShipmentEntity) *orderdto.ShipmentResp {
	if e == nil {
		return nil
	}
	resp := &orderdto.ShipmentResp{
		ID: e.ID, ShipmentNo: e.ShipmentNo, OrderID: e.OrderID,
		CarrierName: e.CarrierName, TrackingNo: e.TrackingNo,
		ShippedAt:  e.ShippedAt.Format(time.RFC3339),
		OperatorID: e.OperatorID, OperatorName: e.OperatorName,
		Remark: e.Remark, RequestID: e.RequestID,
	}
	if e.CarrierID != nil {
		resp.CarrierID = *e.CarrierID
	}
	return resp
}

// getCarrierName 补齐承运商名称快照（req.CarrierID 给了但 CarrierName 为空时）。
//
// 名称快照是必需事实：承运商改名 / 停用 / 删除之后，历史单据仍要显示当时的名字。
// 查不到时保留调用方传的名字（可能为空），**不报错** —— 承运商只是一个可选属性。
func getCarrierName(ctx context.Context, s *Service, projectID string, e *ordermodel.OrderShipmentEntity) {
	if e == nil || e.CarrierID == nil || strings.TrimSpace(e.CarrierName) != "" {
		return
	}
	if s.carriers == nil {
		return
	}
	if c, err := s.carriers.Get(ctx, *e.CarrierID, projectID); err == nil && c != nil {
		e.CarrierName = c.Name
	}
}

// ListShipments 某订单的发货记录（客户与后台都看这一份）。
func (s *Service) ListShipments(ctx context.Context, req *orderdto.ListShipmentReq) (list []*orderdto.ShipmentResp, err error) {
	if req == nil || req.OrderID == 0 {
		return nil, errors.New(orderenums.ErrInvalidParam)
	}
	if s.shipments == nil {
		return nil, errors.New(orderenums.ErrInvalidParam)
	}
	projectID, perr := s.resolveOrderProject(ctx, req.OrderID, req.ProjectID)
	if perr != nil {
		return nil, perr
	}
	rows, err := s.shipments.ListByOrder(ctx, req.OrderID)
	if err != nil {
		return nil, err
	}
	list = make([]*orderdto.ShipmentResp, 0, len(rows))
	for _, r := range rows {
		if r.ProjectID != projectID {
			continue
		}
		list = append(list, s.shipmentResp(ctx, r))
	}
	return list, nil
}

// maxShipLines 单次发货的行数上限（一批锁住的行数必须有界，与库存侧 maxBatchLines 同口径）。
const maxShipLines = 200
