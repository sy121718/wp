package orderservice

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	ordercontract "go_wp/internal/module/order/contract"
	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
	"go_wp/pkg/utils"
)

func (s *Service) carrierProject(ctx context.Context, explicit string) (string, error) {
	if pid := strings.TrimSpace(explicit); pid != "" {
		return pid, nil
	}
	ids, err := s.projectIDs(ctx)
	if err != nil {
		return "", err
	}
	if len(ids) != 1 {
		return "", orderenums.Err(orderenums.ErrProjectRequired)
	}
	return ids[0], nil
}

const trackingPlaceholder = "{tracking_no}"

var invalidPlaceholder = regexp.MustCompile(`\{[A-Za-z0-9_]+\}`)

func (s *Service) ListCarriers(ctx context.Context, req *orderdto.ListCarrierReq) (list []*orderdto.CarrierResp, err error) {
	if s.carriers == nil {
		return nil, orderenums.Err(orderenums.ErrInvalidParam)
	}
	projectID, perr := s.carrierProject(ctx, req.ProjectID)
	if perr != nil {
		return nil, perr
	}
	rows, err := s.carriers.List(ctx, projectID, req.IncludeDisabled)
	if err != nil {
		return nil, err
	}
	list = make([]*orderdto.CarrierResp, 0, len(rows))
	for _, r := range rows {
		list = append(list, toCarrierResp(r))
	}
	return list, nil
}

func (s *Service) CreateCarrier(ctx context.Context, req *orderdto.CarrierCreateReq) (res *orderdto.CarrierResp, err error) {
	if s.carriers == nil || req == nil {
		return nil, orderenums.Err(orderenums.ErrInvalidParam)
	}
	code := strings.TrimSpace(req.Code)
	name := strings.TrimSpace(req.Name)
	if code == "" || name == "" {
		return nil, orderenums.Err(orderenums.ErrInvalidParam)
	}
	tpl, terr := normalizeTrackingTemplate(req.TrackingURLTemplate)
	if terr != nil {
		return nil, terr
	}
	projectID, perr := s.carrierProject(ctx, req.ProjectID)
	if perr != nil {
		return nil, perr
	}
	taken, cerr := s.carriers.CodeExists(ctx, projectID, code, "")
	if cerr != nil {
		return nil, cerr
	}
	if taken {
		return nil, orderenums.Err(orderenums.ErrCarrierCodeTaken)
	}
	now := time.Now().UTC()
	e := &ordermodel.OrderCarrierEntity{
		ID: uuid.NewString(), ProjectID: projectID,
		Code: code, Name: name, TrackingURLTemplate: tpl,
		Status: "active", Sort: req.Sort,
		Metadata: []byte("{}"), CreateTime: now, UpdateTime: now,
	}
	if err = s.carriers.Create(ctx, e); err != nil {
		return nil, err
	}
	return toCarrierResp(e), nil
}

func (s *Service) UpdateCarrier(ctx context.Context, req *orderdto.CarrierUpdateReq) (res *orderdto.CarrierResp, err error) {
	if s.carriers == nil || req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, orderenums.Err(orderenums.ErrInvalidParam)
	}
	projectID, perr := s.carrierProject(ctx, req.ProjectID)
	if perr != nil {
		return nil, perr
	}
	e, gerr := s.carriers.Get(ctx, strings.TrimSpace(req.ID), projectID)
	if gerr != nil {
		if errors.Is(gerr, gorm.ErrRecordNotFound) {
			return nil, orderenums.Err(orderenums.ErrCarrierNotFound)
		}
		return nil, gerr
	}
	if req.Code != nil {
		code := strings.TrimSpace(*req.Code)
		if code == "" {
			return nil, orderenums.Err(orderenums.ErrInvalidParam)
		}
		taken, cerr := s.carriers.CodeExists(ctx, projectID, code, e.ID)
		if cerr != nil {
			return nil, cerr
		}
		if taken {
			return nil, orderenums.Err(orderenums.ErrCarrierCodeTaken)
		}
		e.Code = code
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, orderenums.Err(orderenums.ErrInvalidParam)
		}
		e.Name = name
	}
	if req.TrackingURLTemplate != nil {
		tpl, terr := normalizeTrackingTemplate(*req.TrackingURLTemplate)
		if terr != nil {
			return nil, terr
		}
		e.TrackingURLTemplate = tpl
	}
	if req.Status != nil {
		status := strings.TrimSpace(*req.Status)
		if status != "active" && status != "disabled" {
			return nil, orderenums.Err(orderenums.ErrCarrierStatusInvalid)
		}
		e.Status = status
	}
	if req.Sort != nil {
		e.Sort = *req.Sort
	}
	e.UpdateTime = time.Now().UTC()
	if err = s.carriers.Update(ctx, e); err != nil {
		return nil, err
	}
	return toCarrierResp(e), nil
}

func normalizeTrackingTemplate(raw string) (string, error) {
	tpl := strings.TrimSpace(raw)
	if tpl == "" {
		return "", nil
	}
	for _, m := range invalidPlaceholder.FindAllString(tpl, -1) {
		if m != trackingPlaceholder {
			return "", orderenums.Err(orderenums.ErrCarrierTemplateInvalid)
		}
	}
	return tpl, nil
}

func trackingURL(tpl, trackingNo string) string {
	if tpl == "" || strings.TrimSpace(trackingNo) == "" {
		return ""
	}
	return strings.ReplaceAll(tpl, trackingPlaceholder, strings.TrimSpace(trackingNo))
}

func toCarrierResp(e *ordermodel.OrderCarrierEntity) *orderdto.CarrierResp {
	if e == nil {
		return nil
	}
	return &orderdto.CarrierResp{
		ID: e.ID, Code: e.Code, Name: e.Name,
		TrackingURLTemplate: e.TrackingURLTemplate,
		Status:              e.Status, Sort: e.Sort,
	}
}

type pendingShipProbeLine struct {
	orderID uint64
	line    ordercontract.ShipProbeLine
}

func (s *Service) ListPendingShip(ctx context.Context, req *orderdto.PendingShipReq) (res *orderdto.PendingShipPageResp, err error) {
	if s.shipments == nil {
		return nil, orderenums.Err(orderenums.ErrInvalidParam)
	}
	if req == nil {
		req = &orderdto.PendingShipReq{}
	}
	projectID, perr := s.carrierProject(ctx, req.ProjectID)
	if perr != nil {
		return nil, perr
	}
	limit := req.Limit
	if limit <= 0 || limit > pendingShipPageMax {
		limit = pendingShipPageDefault
	}
	offset := req.Offset
	if offset < 0 {
		offset = 0
	}
	orders, total, lerr := s.orders.ListPendingShipPage(ctx, ordermodel.PendingShipFilter{
		ProjectID: projectID, Keyword: strings.TrimSpace(req.Keyword),
		Offset: offset, Limit: limit,
	})
	if lerr != nil {
		return nil, lerr
	}

	rows := make([]*orderdto.PendingShipRow, 0, len(orders))
	probes := make([]pendingShipProbeLine, 0, len(orders)*2)
	for _, o := range orders {
		items, ierr := s.items.ListByOrderID(ctx, o.ID)
		if ierr != nil {
			return nil, ierr
		}
		if len(items) == 0 {
			continue
		}
		itemIDs := make([]uint64, 0, len(items))
		totalQty := 0
		for _, it := range items {
			itemIDs = append(itemIDs, it.ID)
			totalQty += it.Quantity
		}

		shippedByItem := s.shippedByOrderItem(ctx, o.ID, itemIDs)
		shippedQty := 0
		for _, n := range shippedByItem {
			shippedQty += n
		}
		remaining := totalQty - shippedQty
		if remaining <= 0 {

			continue
		}
		row := &orderdto.PendingShipRow{
			OrderID: o.ID, OrderNo: o.OrderNo,
			CustomerName:      o.CustomerName,
			CustomerEmail:     o.CustomerEmail,
			Currency:          o.Currency,
			Total:             o.Total,
			LineCount:         len(items),
			ItemQuantity:      totalQty,
			ShippedQuantity:   shippedQty,
			RemainingQuantity: remaining,
			OrderedAt:         utils.NewJSONTime(o.CreateTime),
			CreatedVia:        o.CreatedVia,

			Shippable: true,
		}

		if list, serr := s.shipments.ListByOrder(ctx, o.ID); serr == nil && len(list) > 0 {
			row.LatestShipmentNo = list[len(list)-1].ShipmentNo
		}
		for _, it := range items {
			left := it.Quantity - shippedByItem[it.ID]
			if left <= 0 {
				continue
			}
			line := ordercontract.ShipProbeLine{
				OrderItemID: it.ID, VariantID: it.VariantID, Quantity: left,
			}
			if it.WarehouseID != nil {
				line.WarehouseID = *it.WarehouseID
			}
			probes = append(probes, pendingShipProbeLine{orderID: o.ID, line: line})
		}
		rows = append(rows, row)
	}
	s.markPendingShippable(ctx, projectID, rows, probes)
	return &orderdto.PendingShipPageResp{List: rows, Total: total}, nil
}

func pendingShipBlockReason(line ordercontract.ShipProbeLine, fact ordercontract.ShipAvailability) string {
	switch {
	case line.VariantID == "":
		return "variant_missing"
	case fact.Unmatched:
		return "unmatched"
	case fact.NoWarehouse:
		return "warehouse_missing"
	case !fact.Infinite && fact.Available < line.Quantity:
		return "stock_insufficient"
	default:
		return ""
	}
}

func (s *Service) markPendingShippable(ctx context.Context, projectID string,
	rows []*orderdto.PendingShipRow, probes []pendingShipProbeLine) {
	if s.stockProbe == nil || len(probes) == 0 {
		return
	}
	lines := make([]ordercontract.ShipProbeLine, 0, len(probes))
	for _, p := range probes {
		lines = append(lines, p.line)
	}
	facts, err := s.stockProbe.ShipAvailabilityByLine(ctx, projectID, lines)
	if err != nil {
		return
	}
	for _, p := range probes {
		reason := pendingShipBlockReason(p.line, facts[p.line.OrderItemID])
		if reason == "" {
			continue
		}
		for _, r := range rows {
			if r.OrderID != p.orderID || !r.Shippable {
				continue
			}
			r.Shippable = false
			r.ShippableReason = reason
		}
	}
}

func (s *Service) shippedByOrderItem(ctx context.Context, orderID uint64, itemIDs []uint64) map[uint64]int {
	if s.shipments == nil || len(itemIDs) == 0 {
		return map[uint64]int{}
	}
	out, err := s.shipments.SumShippedQuantity(ctx, orderID, itemIDs)
	if err != nil {
		return map[uint64]int{}
	}
	return out
}

func (s *Service) BuildShipForm(ctx context.Context, req *orderdto.ListShipmentReq) (form *orderdto.ShipForm, err error) {
	if req == nil || req.OrderID == 0 {
		return nil, orderenums.Err(orderenums.ErrInvalidParam)
	}
	projectID, perr := s.resolveOrderProject(ctx, req.OrderID, req.ProjectID)
	if perr != nil {
		return nil, perr
	}
	var e *ordermodel.OrderEntity
	if e, err = s.orders.GetByID(ctx, req.OrderID, projectID); err != nil {
		return nil, err
	}
	if e == nil {
		return nil, orderenums.Err(orderenums.ErrOrderNotFound)
	}
	items, ierr := s.items.ListByOrderID(ctx, e.ID)
	if ierr != nil {
		return nil, ierr
	}
	itemIDs := make([]uint64, 0, len(items))
	for _, it := range items {
		itemIDs = append(itemIDs, it.ID)
	}
	shipped := s.shippedByOrderItem(ctx, e.ID, itemIDs)

	warehouseName := s.warehouseNames(ctx, projectID)
	form = &orderdto.ShipForm{
		OrderID: e.ID, OrderNo: e.OrderNo, CustomerName: e.CustomerName, Total: e.Total,

		RequestID: uuid.NewString(),
		Lines:     make([]*orderdto.ShipFormLine, 0, len(items)),
	}
	for _, it := range items {
		line := &orderdto.ShipFormLine{
			OrderItemID: it.ID, ProductName: it.ProductName, VariantLabel: it.VariantLabel,
			SKU: it.SKU, Quantity: it.Quantity, Shipped: shipped[it.ID],
			Remaining: it.Quantity - shipped[it.ID],
		}
		if it.WarehouseID != nil {
			line.WarehouseID = *it.WarehouseID
			line.WarehouseName = warehouseName[*it.WarehouseID]
		}
		if line.Remaining > 0 {
			form.Lines = append(form.Lines, line)
		}
	}

	if s.carriers != nil {
		if list, cerr := s.carriers.List(ctx, projectID, false); cerr == nil {
			form.Carriers = make([]*orderdto.CarrierResp, 0, len(list))
			for _, c := range list {
				form.Carriers = append(form.Carriers, toCarrierResp(c))
			}
		}
	}
	return form, nil
}

func (s *Service) warehouseNames(ctx context.Context, projectID string) map[string]string {
	out := map[string]string{}

	lister := s.warehouseSource
	if lister == nil {
		if s.warehouses == nil {
			return out
		}
		casted, ok := s.warehouses.(ordercontract.ReturnWarehouseSource)
		if !ok {
			return out
		}
		lister = casted
	}
	list, err := lister.ListReturnWarehouses(ctx, projectID)
	if err != nil {
		return out
	}
	for _, w := range list {
		out[w.ID] = w.Name
	}
	return out
}
