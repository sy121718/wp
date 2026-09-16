package orderservice

// return_request.go — 退货申请的客户侧（BIZ-1，退货入库）。
//
// 客户能做的只有三件事：提交申请、撤销自己**还没被审核**的申请、查自己的申请。
// 三件事都按 userID 收口：片段层写入的 UserID 是身份的来源，这里**再核一次订单归属** ——
// 片段层写错了、被绕过了，这一层仍然拦得住。（安全边界不靠某一层独守。）

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"crypto/rand"
	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
	"go_wp/pkg/database"
	"go_wp/pkg/rls"
	"go_wp/pkg/utils"
	"gorm.io/gorm"
)

const (
	// maxReturnItems 单张申请单的明细行数上限（与订单项上限同量级）。
	maxReturnItems = 100
	// maxReturnQuantity 单行退货数量上限（与下单的数量上限同口径）。
	maxReturnQuantity = 100000
)

// returnableOrderStatuses 允许申请退货的订单状态。
//
// pending（还没付款）不在其中：没付钱的单没有可退的钱；
// cancelled 也不在：货还没出去，走「取消」即可（那条路径会直接归还库存）；
// refunded 同理 —— 已经退过款的单不能再退第二次。
var returnableOrderStatuses = map[string]bool{
	ordermodel.OrderStatusPaid:      true,
	ordermodel.OrderStatusShipped:   true,
	ordermodel.OrderStatusCompleted: true,
}

// returnStatusLabel 状态 → 中文文案。
//
// 只此一份：后台页、访客片段、将来的邮件都必须说同一句话；
// 同一个状态在三处各写一个名字，最后一定会出现「已入库待退款」与「待退款」并存。
func returnStatusLabel(status string) string {
	switch status {
	case ordermodel.ReturnStatusRequested:
		return "待审核"
	case ordermodel.ReturnStatusApproved:
		return "待收货"
	case ordermodel.ReturnStatusReceived:
		return "已入库待退款"
	case ordermodel.ReturnStatusCompleted:
		return "已完成"
	case ordermodel.ReturnStatusRejected:
		return "已拒绝"
	case ordermodel.ReturnStatusCancelled:
		return "已撤销"
	}
	return status
}

// RequestReturn 客户提交退货申请。
func (s *Service) RequestReturn(ctx context.Context, req *orderdto.ReturnRequestReq) (res *orderdto.ReturnResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, errors.New(orderenums.ErrProjectRequired)
	}
	if req.OrderID == 0 {
		return nil, errors.New(orderenums.ErrInvalidParam)
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		return nil, errors.New(orderenums.ErrReturnReasonRequired)
	}
	if len(req.Items) == 0 {
		return nil, errors.New(orderenums.ErrReturnItemsRequired)
	}
	if len(req.Items) > maxReturnItems {
		return nil, errors.New(orderenums.ErrInvalidParam)
	}
	projectID := strings.TrimSpace(req.ProjectID)

	// 幂等：同一个 requestID 只落一张申请单（客户在网络不稳时点两次是常态）。
	if reqID := strings.TrimSpace(req.RequestID); reqID != "" {
		existing, gerr := s.returns.GetByRequestID(ctx, projectID, reqID)
		if gerr != nil {
			return nil, gerr
		}
		if existing != nil {
			items, ierr := s.returns.ItemsByReturnID(ctx, existing.ID)
			if ierr != nil {
				return nil, ierr
			}
			return toReturnResp(existing, items, nil), nil
		}
	}

	order, oerr := s.orders.GetByID(ctx, req.OrderID, projectID)
	if oerr != nil {
		return nil, oerr
	}
	if order == nil || order.ProjectID != projectID {
		// 跨工程与不存在返回同一句话：区分开来就是一个订单 id 探测器。
		return nil, errors.New(orderenums.ErrOrderNotFound)
	}
	// 归属：登录访客只能退自己的单。userID 为 0 表示没带身份（片段层未登录或调用错误），
	// 一律拒绝 —— 「不传即放行」是这里最危险的默认值。
	if req.UserID == 0 {
		return nil, errors.New(orderenums.ErrInvalidParam)
	}
	if order.UserID == nil || *order.UserID != req.UserID {
		return nil, errors.New(orderenums.ErrOrderNotFound)
	}
	if !returnableOrderStatuses[order.Status] {
		return nil, errors.New(orderenums.ErrReturnOrderNotReturnable)
	}

	orderItems, ierr := s.items.ListByOrderID(ctx, order.ID)
	if ierr != nil {
		return nil, ierr
	}
	if len(orderItems) == 0 {
		return nil, errors.New(orderenums.ErrOrderHasNoItems)
	}
	byID := make(map[uint64]*ordermodel.OrderItemEntity, len(orderItems))
	for _, it := range orderItems {
		byID[it.ID] = it
	}
	// 可退数量与退款额在事务内、订单行锁下重算，避免并发超退。
	returnNo, nerr := s.newReturnNo(ctx, projectID)
	if nerr != nil {
		return nil, nerr
	}
	now := time.Now()
	userID := req.UserID
	head := &ordermodel.ReturnEntity{
		ProjectID:     projectID,
		OrderID:       order.ID,
		OrderNo:       order.OrderNo,
		ReturnNo:      returnNo,
		Status:        ordermodel.ReturnStatusRequested,
		Reason:        reason,
		UserID:        &userID,
		CustomerEmail: order.CustomerEmail,
		CustomerName:  order.CustomerName,
		RequestID:     strings.TrimSpace(req.RequestID),
		CreateTime:    now,
		UpdateTime:    now,
	}
	var items []*ordermodel.ReturnItemEntity
	var returnable map[uint64]int

	err = s.returns.Transaction(ctx, func(tx *gorm.DB) error {
		locked, lerr := s.orders.LockByIDTx(ctx, tx, order.ProjectID, order.ID)
		if lerr != nil {
			return lerr
		}
		if locked == nil {
			return errors.New(orderenums.ErrOrderNotFound)
		}
		returnable, lerr = s.returnableByItemTx(ctx, tx, locked.ID, orderItems)
		if lerr != nil {
			return lerr
		}
		var total int64
		items = make([]*ordermodel.ReturnItemEntity, 0, len(req.Items))
		for _, line := range req.Items {
			it := byID[line.OrderItemID]
			if it == nil {
				return errors.New(orderenums.ErrInvalidParam)
			}
			if line.Quantity <= 0 || line.Quantity > maxReturnQuantity {
				return errors.New(orderenums.ErrReturnQuantityInvalid)
			}
			if line.Quantity > returnable[it.ID] {
				return fmt.Errorf("%s：%s", orderenums.ErrReturnQuantityExceeded, it.ProductName)
			}
			amount := lineRefundAmount(it, line.Quantity)
			total += amount
			items = append(items, &ordermodel.ReturnItemEntity{
				OrderItemID:  it.ID,
				ProductID:    it.ProductID,
				VariantID:    it.VariantID,
				ProductName:  it.ProductName,
				VariantLabel: it.VariantLabel,
				SKU:          it.SKU,
				UnitPrice:    it.UnitPrice,
				Quantity:     line.Quantity,
				RefundAmount: amount,
				CreateTime:   now,
			})
		}
		head.RefundAmount = total
		if cerr := s.returns.CreateTx(ctx, tx, head); cerr != nil {
			return cerr
		}
		for _, it := range items {
			it.ReturnID = head.ID
		}
		return s.returns.CreateItemsTx(ctx, tx, items)
	})
	if err != nil {
		if reqID := strings.TrimSpace(req.RequestID); reqID != "" && database.IsUniqueViolation(err) {
			if existing, gerr := s.returns.GetByRequestID(ctx, projectID, reqID); gerr == nil && existing != nil {
				items, ierr := s.returns.ItemsByReturnID(ctx, existing.ID)
				if ierr != nil {
					return nil, ierr
				}
				return toReturnResp(existing, items, nil), nil
			}
		}
		return nil, err
	}
	return toReturnResp(head, items, returnable), nil
}

// CancelReturn 客户撤销自己尚未审核的申请。
func (s *Service) CancelReturn(ctx context.Context, req *orderdto.ReturnCancelReq) (err error) {
	if req == nil || req.ReturnID == 0 || req.UserID == 0 {
		return errors.New(orderenums.ErrInvalidParam)
	}
	// 定位跳（DB-009 第四批）：访客撤销只给退货单 id（归属校验在下面用实体字段做）。
	projectID, perr := s.locateReturnProject(ctx, req.ReturnID)
	if perr != nil {
		return perr
	}
	now := time.Now()
	return s.returns.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return serr
		}
		e, lerr := s.returns.LockByIDTx(ctx, tx, projectID, req.ReturnID)
		if lerr != nil {
			return lerr
		}
		if e == nil || e.UserID == nil || *e.UserID != req.UserID {
			// 不属于自己的申请与不存在的申请返回同一句话。
			return errors.New(orderenums.ErrReturnNotFound)
		}
		if e.Status != ordermodel.ReturnStatusRequested {
			// 已同意的申请不能自己撤：仓库可能已经在收货了。
			return errors.New(orderenums.ErrReturnNotCancellable)
		}
		return s.returns.UpdateFieldsTx(ctx, tx, e.ProjectID, e.ID, map[string]any{
			"status":      ordermodel.ReturnStatusCancelled,
			"admin_note":  strings.TrimSpace(req.Reason),
			"update_time": now,
		})
	})
}

// ListVisitorReturns 访客查自己的退货申请。
func (s *Service) ListVisitorReturns(ctx context.Context, req *orderdto.VisitorReturnListReq) (res *orderdto.ReturnListResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, errors.New(orderenums.ErrProjectRequired)
	}
	if req.UserID == 0 {
		return nil, errors.New(orderenums.ErrInvalidParam)
	}
	uid := req.UserID
	list, total, lerr := s.returns.List(ctx, ordermodel.ReturnFilter{
		ProjectID: req.ProjectID,
		OrderID:   req.OrderID,
		UserID:    &uid,
		Offset:    req.Offset,
		Limit:     req.Limit,
	})
	if lerr != nil {
		return nil, lerr
	}
	items, ierr := s.returns.ItemsByReturnIDs(ctx, idsOfReturns(list))
	if ierr != nil {
		return nil, ierr
	}
	byReturn := groupReturnItems(items)
	res = &orderdto.ReturnListResp{List: make([]*orderdto.ReturnResp, 0, len(list)), Total: total}
	for _, e := range list {
		res.List = append(res.List, toReturnResp(e, byReturn[e.ID], nil))
	}
	return res, nil
}

// returnableByItem 各订单项**当前可退数量**（购买数量 − 已占用）。
//
// 「已占用」= 所有非终态申请（requested / approved / received / completed）里该订单项的数量之和。
// 拒绝与撤销不占额度 —— 客户被拒之后当然可以改个理由重新申请。
//
// 算出来是**快照**：真正落库那一刻的并发竞争由「申请单落在同一张表、聚合随时可重算」兜住
// （两笔并发申请最多各自通过一次校验，但累计值仍在可退范围内 —— 见 return_flow_test 的并发用例）。
func (s *Service) returnableByItem(ctx context.Context, orderID uint64, items []*ordermodel.OrderItemEntity) (map[uint64]int, error) {
	ids, err := s.returns.IDsByOrder(ctx, orderID, ordermodel.ReturnActiveStatuses)
	if err != nil {
		return nil, err
	}
	itemIDs := make([]uint64, 0, len(items))
	for _, it := range items {
		itemIDs = append(itemIDs, it.ID)
	}
	sums, serr := s.returns.SumQuantityByOrderItems(ctx, ids, itemIDs)
	if serr != nil {
		return nil, serr
	}
	return returnableFromSums(items, sums), nil
}

// returnableByItemTx 事务内计算可退数量（与订单行锁配合）。
func (s *Service) returnableByItemTx(ctx context.Context, tx *gorm.DB, orderID uint64, items []*ordermodel.OrderItemEntity) (map[uint64]int, error) {
	ids, err := s.returns.IDsByOrderTx(ctx, tx, orderID, ordermodel.ReturnActiveStatuses)
	if err != nil {
		return nil, err
	}
	itemIDs := make([]uint64, 0, len(items))
	for _, it := range items {
		itemIDs = append(itemIDs, it.ID)
	}
	sums, serr := s.returns.SumQuantityByOrderItemsTx(ctx, tx, ids, itemIDs)
	if serr != nil {
		return nil, serr
	}
	return returnableFromSums(items, sums), nil
}

func returnableFromSums(items []*ordermodel.OrderItemEntity, sums map[uint64]int) map[uint64]int {
	out := make(map[uint64]int, len(items))
	for _, it := range items {
		left := it.Quantity - sums[it.ID]
		if left < 0 {
			left = 0
		}
		out[it.ID] = left
	}
	return out
}

// newReturnNo 生成退货单号：RTR + 日期 + 随机后缀（唯一约束兜底，撞了重试）。
func (s *Service) newReturnNo(ctx context.Context, projectID string) (no string, err error) {
	const attempts = 5
	buf := make([]byte, 4)
	for i := 0; i < attempts; i++ {
		if _, rerr := rand.Read(buf); rerr != nil {
			return "", rerr
		}
		no = fmt.Sprintf("RTR%s%08X", time.Now().Format("20060102"),
			uint32(buf[0])<<24|uint32(buf[1])<<16|uint32(buf[2])<<8|uint32(buf[3]))
		list, _, lerr := s.returns.List(ctx, ordermodel.ReturnFilter{ProjectID: projectID, Keyword: no, Limit: 1})
		if lerr != nil {
			return "", lerr
		}
		if len(list) == 0 {
			return no, nil
		}
	}
	return "", errors.New(orderenums.ErrOrderNoTaken)
}

// idsOfReturns 取退货单 id 列表（批量取明细用）。
func idsOfReturns(list []*ordermodel.ReturnEntity) []uint64 {
	ids := make([]uint64, 0, len(list))
	for _, e := range list {
		ids = append(ids, e.ID)
	}
	return ids
}

// groupReturnItems 明细按退货单分组（避免逐单查库）。
func groupReturnItems(items []*ordermodel.ReturnItemEntity) map[uint64][]*ordermodel.ReturnItemEntity {
	out := make(map[uint64][]*ordermodel.ReturnItemEntity)
	for _, it := range items {
		out[it.ReturnID] = append(out[it.ReturnID], it)
	}
	return out
}

// toReturnResp 实体 → 视图（金额与状态文案都在这里算好，模板零算术）。
func toReturnResp(e *ordermodel.ReturnEntity, items []*ordermodel.ReturnItemEntity, returnable map[uint64]int) *orderdto.ReturnResp {
	if e == nil {
		return nil
	}
	res := &orderdto.ReturnResp{
		ID: e.ID, ProjectID: e.ProjectID,
		OrderID: e.OrderID, OrderNo: e.OrderNo, ReturnNo: e.ReturnNo,
		Status: e.Status, StatusLabel: returnStatusLabel(e.Status),
		Reason:        e.Reason,
		RefundAmount:  e.RefundAmount,
		RefundLabel:   yuanText(e.RefundAmount),
		UserID:        e.UserID,
		CustomerEmail: e.CustomerEmail,
		CustomerName:  e.CustomerName,
		AdminNote:     e.AdminNote,
		ReviewerName:  e.ReviewerName,
		ReviewedAt:    utils.NewJSONTimePtr(e.ReviewedAt),
		ReceivedAt:    utils.NewJSONTimePtr(e.ReceivedAt),
		RefundedAt:    utils.NewJSONTimePtr(e.RefundedAt),
		TransactionID: e.TransactionID,
		CreateTime:    utils.NewJSONTime(e.CreateTime),
		UpdateTime:    utils.NewJSONTime(e.UpdateTime),
		Items:         make([]*orderdto.ReturnItemResp, 0, len(items)),
	}
	for _, it := range items {
		line := &orderdto.ReturnItemResp{
			OrderItemID: it.OrderItemID,
			ProductID:   it.ProductID,
			VariantID:   it.VariantID,
			ProductName: it.ProductName, VariantLabel: it.VariantLabel, SKU: it.SKU,
			UnitPrice: it.UnitPrice, UnitPriceLabel: yuanText(it.UnitPrice),
			Quantity: it.Quantity, ReceivedQuantity: it.ReceivedQuantity,
			RefundAmount: it.RefundAmount, RefundLabel: yuanText(it.RefundAmount),
		}
		if returnable != nil {
			line.Returnable = returnable[it.OrderItemID]
		}
		res.Items = append(res.Items, line)
	}
	return res
}

// yuanText 分 → 「x.xx 元」（与订单其它展示同口径）。
func yuanText(cents int64) string {
	return centsToYuanLabel(cents) + " 元"
}
