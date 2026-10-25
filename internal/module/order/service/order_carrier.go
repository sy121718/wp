package orderservice

// order_carrier.go — 承运商管理（迁移 624，S1/S2）与「待发货」读模型。
//
// 承运商是**可选主数据**：自营配送 / 客户自提的订单没有承运商。因此它是独立一张表，
// 而不是订单上的几个字段 —— 同一个承运商会被几十张单引用，「改个名字要改几十行」
// 是它必须被抽出来的唯一理由。
//
// 停用（status = disabled）而不是删除：历史发货单上的 carrier_id 还指着它，
// 删掉会让「这单是哪家送的」变成一条悬空外键。

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
)

// carrierProject 承运商 / 待发货页的工程作用域。
//
// 与订单用例的 resolveOrderProject 不同：这里没有订单 id 可探测归属，只能靠调用方给的
// projectID（后台页一定带工程上下文）；空串时退化成「唯一工程」——与库存域的既有口径一致
// （单站部署下工程只有一个）。两个以上工程而调用方没给，那是装配/表单缺陷，直接拒绝。
func (s *Service) carrierProject(ctx context.Context, explicit string) (string, error) {
	if pid := strings.TrimSpace(explicit); pid != "" {
		return pid, nil
	}
	ids, err := s.projectIDs(ctx)
	if err != nil {
		return "", err
	}
	if len(ids) != 1 {
		return "", errors.New(orderenums.ErrProjectRequired)
	}
	return ids[0], nil
}

// trackingPlaceholder 跟踪页模板里唯一允许的占位符。
const trackingPlaceholder = "{tracking_no}"

// invalidPlaceholder 除 {tracking_no} 之外的花括号占位符（`{xxx}` 形态）。
var invalidPlaceholder = regexp.MustCompile(`\{[A-Za-z0-9_]+\}`)

// ListCarriers 某工程的承运商（默认只列启用中的：停用的不该出现在发货表单里）。
func (s *Service) ListCarriers(ctx context.Context, req *orderdto.ListCarrierReq) (list []*orderdto.CarrierResp, err error) {
	if s.carriers == nil {
		return nil, errors.New(orderenums.ErrInvalidParam)
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

// CreateCarrier 新建承运商（代码工程内唯一，大小写不敏感）。
func (s *Service) CreateCarrier(ctx context.Context, req *orderdto.CarrierCreateReq) (res *orderdto.CarrierResp, err error) {
	if s.carriers == nil || req == nil {
		return nil, errors.New(orderenums.ErrInvalidParam)
	}
	code := strings.TrimSpace(req.Code)
	name := strings.TrimSpace(req.Name)
	if code == "" || name == "" {
		return nil, errors.New(orderenums.ErrInvalidParam)
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
		return nil, errors.New(orderenums.ErrCarrierCodeTaken)
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

// UpdateCarrier 修改承运商（逐字段可选）。
func (s *Service) UpdateCarrier(ctx context.Context, req *orderdto.CarrierUpdateReq) (res *orderdto.CarrierResp, err error) {
	if s.carriers == nil || req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, errors.New(orderenums.ErrInvalidParam)
	}
	projectID, perr := s.carrierProject(ctx, req.ProjectID)
	if perr != nil {
		return nil, perr
	}
	e, gerr := s.carriers.Get(ctx, strings.TrimSpace(req.ID), projectID)
	if gerr != nil {
		if errors.Is(gerr, gorm.ErrRecordNotFound) {
			return nil, errors.New(orderenums.ErrCarrierNotFound)
		}
		return nil, gerr
	}
	if req.Code != nil {
		code := strings.TrimSpace(*req.Code)
		if code == "" {
			return nil, errors.New(orderenums.ErrInvalidParam)
		}
		taken, cerr := s.carriers.CodeExists(ctx, projectID, code, e.ID)
		if cerr != nil {
			return nil, cerr
		}
		if taken {
			return nil, errors.New(orderenums.ErrCarrierCodeTaken)
		}
		e.Code = code
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, errors.New(orderenums.ErrInvalidParam)
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
			return nil, errors.New(orderenums.ErrCarrierStatusInvalid)
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

// normalizeTrackingTemplate 校验并归一跟踪页模板。
//
// 只认 {tracking_no} 一个占位符：模板里出现 `{order_no}` 这类别的占位符时，
// 页面上会原样显示 `{order_no}`（没人替换它）—— 那比「没有跟踪页」更让人困惑。
func normalizeTrackingTemplate(raw string) (string, error) {
	tpl := strings.TrimSpace(raw)
	if tpl == "" {
		return "", nil
	}
	for _, m := range invalidPlaceholder.FindAllString(tpl, -1) {
		if m != trackingPlaceholder {
			return "", errors.New(orderenums.ErrCarrierTemplateInvalid)
		}
	}
	return tpl, nil
}

// trackingURL 用模板与运单号现算跟踪页地址（模板为空 / 运单号为空时返回空串）。
func trackingURL(tpl, trackingNo string) string {
	if tpl == "" || strings.TrimSpace(trackingNo) == "" {
		return ""
	}
	return strings.ReplaceAll(tpl, trackingPlaceholder, strings.TrimSpace(trackingNo))
}

// toCarrierResp 实体 → 响应。
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

// ListPendingShip 待发货列表（迁移 624 / 625，S2）：只列 **paid 且还有未发数量** 的订单。
//
// 三条口径：
//
//	· 状态必须是 paid：pending 还没付款、shipped 已经发过（部分发货的剩余部分仍留在 paid，
//	  那正是本页要抓的）；completed / cancelled / refunded 都不用发；
//	· 「还有未发」= Σ订单行数量 > Σ已发数量。全发完的单应当离开这一屏，
//	  留着只会让人以为「还没发」；
//	· 每个数字都由 service 算好（模板不做减法）。剩余 = 件数 − 已发。
func (s *Service) ListPendingShip(ctx context.Context, projectID string) (rows []*orderdto.PendingShipRow, err error) {
	if s.shipments == nil {
		return nil, errors.New(orderenums.ErrInvalidParam)
	}
	var perr error
	if projectID, perr = s.carrierProject(ctx, projectID); perr != nil {
		return nil, perr
	}
	orders, err := s.orders.ListByStatus(ctx, projectID, ordermodel.OrderStatusPaid)
	if err != nil {
		return nil, err
	}
	rows = make([]*orderdto.PendingShipRow, 0, len(orders))
	for _, o := range orders {
		items, ierr := s.items.ListByOrderID(ctx, o.ID)
		if ierr != nil {
			return nil, ierr
		}
		if len(items) == 0 {
			continue
		}
		itemIDs := make([]uint64, 0, len(items))
		total := 0
		for _, it := range items {
			itemIDs = append(itemIDs, it.ID)
			total += it.Quantity
		}
		shipped := 0
		// 已发数量账走明细求和（不是缓存列）：一张订单的发货单数量是个位数，
		// 逐个订单查一次是可接受的（本页一屏几十单），而缓存列会在删单后静默失准。
		for _, n := range s.shippedByOrderItem(ctx, o.ID, itemIDs) {
			shipped += n
		}
		if total-shipped <= 0 {
			continue
		}
		row := &orderdto.PendingShipRow{
			OrderID: o.ID, OrderNo: o.OrderNo,
			CustomerName:      o.CustomerName,
			Total:             o.Total,
			ItemQuantity:      total,
			ShippedQuantity:   shipped,
			RemainingQuantity: total - shipped,
			OrderedAt:         o.CreateTime.Format(time.RFC3339),
		}
		// 最近一次发货单号（部分发货的单在本页也要能看出来发过哪几张）。
		if list, lerr := s.shipments.ListByOrder(ctx, o.ID); lerr == nil && len(list) > 0 {
			row.LatestShipmentNo = list[len(list)-1].ShipmentNo
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// shippedByOrderItem 某订单的已发数量账（itemID → 已发件数）。
//
// 单独一个非事务版本：ListPendingShip 是只读列表，不必为每条订单开事务。
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

// BuildShipForm 发货表单的数据：订单头只读信息 + 每行的「订单数量 / 已发 / 剩余 / 默认仓」
// + 承运商下拉。
//
// 四个数字全部算好交给模板：「本次」输入框的 max 就是 Remaining —— 让模板算
// `Quantity - Shipped` 会在页面上再实现一次口径（template_logic_test.go 的判据）。
func (s *Service) BuildShipForm(ctx context.Context, req *orderdto.ListShipmentReq) (form *orderdto.ShipForm, err error) {
	if req == nil || req.OrderID == 0 {
		return nil, errors.New(orderenums.ErrInvalidParam)
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
		return nil, errors.New(orderenums.ErrOrderNotFound)
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
	// 仓库名：一次性取全部仓库，按 id 建映射（逐行查会 N+1）。
	warehouseName := s.warehouseNames(ctx, projectID)
	form = &orderdto.ShipForm{
		OrderID: e.ID, OrderNo: e.OrderNo, CustomerName: e.CustomerName, Total: e.Total,
		// 幂等键在这里生成（不是页面）：页面只渲染它，重试时的语义由 service 判 ——
		// 同一 request_id 命中既有单据即幂等成功。
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
	// 承运商下拉（只列启用中的）。
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

// warehouseNames 工程内「仓库 id → 名称」映射（表单展示用）。
//
// 走 WarehouseResolver 背后的仓库列表口时不必新开依赖：订单侧已经有 ReturnWarehouseSource
// （退货页的仓库下拉），那个端口本来就回答「这个工程有哪些仓」—— 本函数只需要它的名字。
// 未注入时退化成空映射：仓的选择靠 id，名称只是帮助辨认，缺它不该让表单打不开。
func (s *Service) warehouseNames(ctx context.Context, projectID string) map[string]string {
	out := map[string]string{}
	if s.warehouses == nil {
		return out
	}
	lister, ok := s.warehouses.(ordercontract.ReturnWarehouseSource)
	if !ok {
		return out
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
