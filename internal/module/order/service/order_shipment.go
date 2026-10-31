package orderservice

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

func (s *Service) ShipOrder(ctx context.Context, req *orderdto.ShipOrderReq) (res *orderdto.ShipmentResp, err error) {
	if req == nil || req.OrderID == 0 {
		return nil, orderenums.Err(orderenums.ErrInvalidParam)
	}
	if s.shipments == nil || s.carriers == nil {

		return nil, orderenums.Err(orderenums.ErrStatusTransition)
	}
	projectID, perr := s.resolveOrderProject(ctx, req.OrderID, req.ProjectID)
	if perr != nil {
		return nil, perr
	}
	requestID := strings.TrimSpace(req.RequestID)

	if existing, ferr := s.shipments.FindByRequestID(ctx, projectID, requestID); ferr != nil {
		return nil, ferr
	} else if existing != nil {
		return s.shipmentResp(ctx, existing), nil
	}
	now := time.Now().UTC()
	var shipment *ordermodel.OrderShipmentEntity

	var terr error
	for attempt := 0; attempt < 2; attempt++ {
		shipment = nil
		terr = s.shipOnce(ctx, &shipOnceArgs{
			req: req, projectID: projectID, requestID: requestID, now: now,
		}, &shipment)
		if terr == nil {
			break
		}
		if !isShipmentNoConflict(terr) {
			break
		}

		time.Sleep(time.Millisecond)
	}
	if terr != nil {
		return nil, terr
	}
	return s.shipmentResp(ctx, shipment), nil
}

type shipOnceArgs struct {
	req       *orderdto.ShipOrderReq
	projectID string
	requestID string
	now       time.Time
}

func (s *Service) shipOnce(ctx context.Context, args *shipOnceArgs, out **ordermodel.OrderShipmentEntity) error {
	req, projectID, requestID, now := args.req, args.projectID, args.requestID, args.now
	var shipment *ordermodel.OrderShipmentEntity
	err := s.orders.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return serr
		}

		e, lerr := s.orders.LockByIDTx(ctx, tx, projectID, req.OrderID)
		if lerr != nil {
			return lerr
		}
		if e == nil {
			return orderenums.Err(orderenums.ErrOrderNotFound)
		}

		if e.Status != ordermodel.OrderStatusPaid {
			return orderenums.Err(orderenums.ErrOrderNotShippable)
		}

		items, ierr := s.items.ListByOrderIDTx(ctx, tx, e.ID)
		if ierr != nil {
			return ierr
		}
		plan, shippedSoFar, cerr := s.planShipment(ctx, tx, projectID, e, items, req.Lines)
		if cerr != nil {
			return cerr
		}

		shipmentNo, nerr := s.nextShipmentNoTx(ctx, tx, projectID, now)
		if nerr != nil {
			return nerr
		}
		if derr := s.shipStockTx(ctx, tx, e, shipmentNo, plan); derr != nil {
			return derr
		}

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

		nextStatus := ordermodel.OrderStatusShipped
		if planHasRemaining(items, shippedSoFar, plan) {
			nextStatus = ordermodel.OrderStatusPaid
		}

		nextFulfillment := ordermodel.FulfillmentShipped
		if nextStatus == ordermodel.OrderStatusPaid {
			nextFulfillment = ordermodel.FulfillmentPartial
		}
		fields := map[string]any{
			"status":             nextStatus,
			"fulfillment_status": nextFulfillment,
			"carrier_name":       shipment.CarrierName,
			"tracking_no":        shipment.TrackingNo,
			"shipped_at":         now,
			"completed_at":       nil,
			"update_time":        now,
		}
		if shipment.CarrierID != nil {
			fields["carrier_id"] = *shipment.CarrierID
		}
		if uerr := s.orders.UpdateFieldsTx(ctx, tx, projectID, e.ID, fields); uerr != nil {
			return uerr
		}

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
	})
	if err != nil {
		return err
	}
	*out = shipment
	return nil
}

func isShipmentNoConflict(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "23505") && strings.Contains(msg, "uq_order_shipments_no")
}

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

type shipPlanLine struct {
	orderItemID uint64
	warehouseID string
	stockID     *string
	variantID   string
	productID   string
	skuCode     string
	quantity    int
}

func (s *Service) planShipment(ctx context.Context, tx *gorm.DB, projectID string,
	e *ordermodel.OrderEntity, items []*ordermodel.OrderItemEntity,
	reqLines []orderdto.ShipLineReq) ([]shipPlanLine, map[uint64]int, error) {
	if len(reqLines) == 0 {
		return nil, nil, orderenums.Err(orderenums.ErrShipLinesRequired)
	}
	if len(reqLines) > maxShipLines {
		return nil, nil, orderenums.Err(orderenums.ErrInvalidParam)
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

	type key struct {
		itemID uint64
		whID   string
	}
	merged := make(map[key]*shipPlanLine, len(reqLines))
	order := make([]key, 0, len(reqLines))
	for _, line := range reqLines {
		item, ok := byID[line.OrderItemID]
		if !ok || !itemByID[line.OrderItemID] {

			return nil, nil, orderenums.Err(orderenums.ErrInvalidParam)
		}
		if line.Quantity <= 0 {
			return nil, nil, orderenums.Err(orderenums.ErrQuantityInvalid)
		}
		whID, persist, werr := s.shipWarehouseOf(ctx, projectID, item, line.WarehouseID)
		if werr != nil {
			return nil, nil, werr
		}
		if persist {

			if perr := s.persistItemWarehouseTx(ctx, tx, item.ID, whID); perr != nil {
				return nil, nil, perr
			}
			id := whID
			item.WarehouseID = &id
		}
		k := key{itemID: item.ID, whID: whID}
		if existing, seen := merged[k]; seen {
			existing.quantity += line.Quantity
		} else {

			snapStockID, serr := s.stockRowID(ctx, projectID, item.VariantID, whID)
			if serr != nil {
				return nil, nil, serr
			}
			var stockPtr *string
			if snapStockID != "" {
				stockPtr = &snapStockID
			}
			entry := &shipPlanLine{
				orderItemID: item.ID, warehouseID: whID,
				stockID:   stockPtr,
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
			return nil, nil, orderenums.Err(orderenums.ErrShipQuantityExceeded)
		}
		plan = append(plan, *entry)
	}
	return plan, shipped, nil
}

func (s *Service) shipWarehouseOf(ctx context.Context, projectID string, item *ordermodel.OrderItemEntity,
	requested string) (warehouseID string, needPersist bool, err error) {
	if s.warehouses == nil {

		return "", false, orderenums.Err(orderenums.ErrShipWarehouseRequired)
	}
	requested = strings.TrimSpace(requested)
	if requested != "" {

		whID, rerr := s.warehouses.ResolveShipWarehouse(ctx, projectID, requested)
		if rerr != nil {
			return "", false, rerr
		}
		if whID == "" {
			return "", false, orderenums.Err(orderenums.ErrShipWarehouseRequired)
		}
		return whID, false, nil
	}
	if item.WarehouseID != nil && strings.TrimSpace(*item.WarehouseID) != "" {
		return strings.TrimSpace(*item.WarehouseID), false, nil
	}
	whID, rerr := s.warehouses.ResolveShipWarehouse(ctx, projectID, "")
	if rerr != nil {
		return "", false, rerr
	}
	if whID == "" {

		return "", false, orderenums.Err(orderenums.ErrShipWarehouseRequired)
	}

	return whID, true, nil
}

func (s *Service) stockRowID(ctx context.Context, projectID, variantID, warehouseID string) (string, error) {
	if s.stockRowLocator == nil {
		return "", nil
	}
	return s.stockRowLocator.LocateStockRow(ctx, projectID, variantID, warehouseID)
}

func (s *Service) persistItemWarehouseTx(ctx context.Context, tx *gorm.DB, itemID uint64, warehouseID string) error {
	if strings.TrimSpace(warehouseID) == "" {
		return nil
	}
	return s.items.SetWarehouseIDIfNullTx(ctx, tx, itemID, warehouseID)
}

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

		SourceType: "order_shipment",

		SourceRef: shipmentNo,
		Remark:    "订单发货",
		Lines:     lines,
	})
	if derr != nil {

		return mapStockError(derr)
	}
	return nil
}

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

func (s *Service) ListShipments(ctx context.Context, req *orderdto.ListShipmentReq) (list []*orderdto.ShipmentResp, err error) {
	if req == nil || req.OrderID == 0 {
		return nil, orderenums.Err(orderenums.ErrInvalidParam)
	}
	if s.shipments == nil {
		return nil, orderenums.Err(orderenums.ErrInvalidParam)
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

const maxShipLines = 200
