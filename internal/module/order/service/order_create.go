package orderservice

// order_create.go — 建单（BIZ-1 销售侧）。
//
// 事务边界与补偿策略照采购入库（#18）的既有先例：
//   ① 先在**一个事务**里写订单头 + 订单项 + 流转流水（订单是主记录，也是扣减的依据）；
//   ② 提交之后再动库存（跨模块，不可能共用一个事务）；
//   ③ 库存不足或库存服务不可用 → **补偿**：把订单标记为已取消并记流水。
//
// 为什么不「先扣库存再写单」：扣减的 source_ref 要用订单号，而订单号不依赖订单 id，
// 两种顺序都能做。选「先写单」是因为它让失败**留痕** —— 补偿后库里留下一条
// 「因库存不足而失败」的已取消订单，能看出发生过什么；反过来先扣库存、写单失败，
// 就只能把库存悄悄归还，事后查不出任何痕迹。

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
	productcontract "go_wp/internal/module/product/contract"
	inventorydto "go_wp/internal/module/product/inventory/dto"
	userdto "go_wp/internal/module/user/dto"
)

const (
	// maxOrderItems 单笔订单的商品项上限。
	//
	// 上限不是性能问题而是**幂等与计算**问题：订单号与金额都在一次请求里算完，
	// 无上限的输入会让一次请求锁住任意多的库存行。
	maxOrderItems = 100
	// maxItemQuantity 单项数量上限（与库存的硬上限同量级）。
	maxItemQuantity = 100000
)

// CreateOrder 建单。
func (s *Service) CreateOrder(ctx context.Context, req *orderdto.CreateOrderReq) (res *orderdto.CreateOrderResp, err error) {
	if req == nil {
		return nil, errors.New(orderenums.ErrInvalidParam)
	}
	projectID := strings.TrimSpace(req.ProjectID)
	if projectID == "" {
		return nil, errors.New(orderenums.ErrProjectRequired)
	}
	email := strings.TrimSpace(req.CustomerEmail)
	if email == "" {
		return nil, errors.New(orderenums.ErrCustomerEmailRequired)
	}
	if !strings.Contains(email, "@") || len(email) < 3 {
		return nil, errors.New(orderenums.ErrCustomerEmailInvalid)
	}
	if len(req.Items) == 0 {
		return nil, errors.New(orderenums.ErrItemsRequired)
	}
	if len(req.Items) > maxOrderItems {
		return nil, errors.New(orderenums.ErrItemLimitExceeded)
	}

	// 幂等：同一 request_id 命中既有单就原样返回，绝不再扣一次库存。
	if reqID := strings.TrimSpace(req.RequestID); reqID != "" {
		existing, gerr := s.orders.GetByRequestID(ctx, projectID, reqID)
		if gerr != nil {
			return nil, gerr
		}
		if existing != nil {
			return &orderdto.CreateOrderResp{
				ID: existing.ID, OrderNo: existing.OrderNo, Status: existing.Status,
				Total: existing.Total, Currency: existing.Currency, Duplicated: true,
			}, nil
		}
	}

	// 商品事实：一次批量取，不逐条查。
	variantIDs := make([]string, 0, len(req.Items))
	for _, it := range req.Items {
		variantIDs = append(variantIDs, strings.TrimSpace(it.VariantID))
	}
	snapshots, err := s.product.VariantSnapshots(ctx, variantIDs)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]*productcontract.VariantSnapshot, len(snapshots))
	for _, sn := range snapshots {
		byID[sn.VariantID] = sn
	}

	// 落快照并算钱。**价格全部来自服务端**：请求体里没有价格字段，
	// 客户端能传价格的接口等于把收银台交给客人自己看。
	items := make([]*ordermodel.OrderItemEntity, 0, len(req.Items))
	var subtotal int64
	now := time.Now()
	for _, it := range req.Items {
		vid := strings.TrimSpace(it.VariantID)
		if vid == "" {
			return nil, errors.New(orderenums.ErrInvalidParam)
		}
		if it.Quantity <= 0 || it.Quantity > maxItemQuantity {
			return nil, errors.New(orderenums.ErrQuantityInvalid)
		}
		sn := byID[vid]
		if sn == nil {
			return nil, fmt.Errorf("%w: %s", errors.New(orderenums.ErrVariantNotFound), vid)
		}
		if sn.ProjectID != "" && sn.ProjectID != projectID {
			// 跨工程下单是越权，不是「查不到」。
			return nil, errors.New(orderenums.ErrVariantNotFound)
		}
		if !sn.Enabled {
			return nil, errors.New(orderenums.ErrVariantNotFound)
		}
		lineSubtotal := sn.Price * int64(it.Quantity)
		subtotal += lineSubtotal
		items = append(items, &ordermodel.OrderItemEntity{
			ProductID:    sn.ProductID,
			VariantID:    sn.VariantID,
			ProductName:  sn.ProductName,
			VariantLabel: sn.VariantLabel,
			SKU:          sn.SKU,
			UnitPrice:    sn.Price,
			Quantity:     it.Quantity,
			LineSubtotal: lineSubtotal,
			LineDiscount: 0,
			LineTax:      0,
			LineTotal:    lineSubtotal,
			CostPrice:    sn.CostPrice,
			CreateTime:   now,
		})
	}

	// 金额：小计 - 优惠 + 运费 + 税。优惠不得低于 0、也不得超过小计（负数总额没有意义）。
	discount := req.DiscountTotal
	if discount < 0 {
		discount = 0
	}
	if discount > subtotal {
		discount = subtotal
	}
	shipping := req.ShippingTotal
	if shipping < 0 {
		shipping = 0
	}
	total := subtotal - discount + shipping

	// 访客开号：该邮箱还没有账号就建一个（随机初始密码，邮件发给客户），并把新账号
	// 关联到订单 —— 否则访客下完单无处可查自己的订单。
	//
	// 失败**不阻断下单**：订单是主体、账号是附赠能力；开号失败时订单照常落库、user_id 留空
	// （客户仍可用这个邮箱走「忘记密码」自己开号）。
	// 邮箱已有账号时只关联、**绝不改密码** —— 那条安全边界在 user 模块里守着。
	userID := req.UserID
	if userID == nil && s.guest != nil {
		if gres, gerr := s.guest.EnsureGuestAccount(ctx, &userdto.GuestAccountReq{
			Email:      email,
			Name:       strings.TrimSpace(req.CustomerName),
			Locale:     req.Locale,
			RegisterIP: req.IPAddress,
		}); gerr == nil && gres != nil && gres.UserID != 0 {
			id := gres.UserID
			userID = &id
		}
	}

	orderNo, err := s.newOrderNo(ctx, projectID)
	if err != nil {
		return nil, err
	}

	head := &ordermodel.OrderEntity{
		ProjectID:          projectID,
		OrderNo:            orderNo,
		Status:             ordermodel.OrderStatusPending,
		UserID:             userID,
		CustomerEmail:      email,
		CustomerName:       strings.TrimSpace(req.CustomerName),
		CustomerPhone:      strings.TrimSpace(req.CustomerPhone),
		Currency:           "CNY",
		Subtotal:           subtotal,
		DiscountTotal:      discount,
		ShippingTotal:      shipping,
		TaxTotal:           0,
		Total:              total,
		ShipName:           strings.TrimSpace(req.Shipping.Name),
		ShipPhone:          strings.TrimSpace(req.Shipping.Phone),
		ShipProvince:       strings.TrimSpace(req.Shipping.Province),
		ShipCity:           strings.TrimSpace(req.Shipping.City),
		ShipDistrict:       strings.TrimSpace(req.Shipping.District),
		ShipAddress:        strings.TrimSpace(req.Shipping.Address),
		ShipZip:            strings.TrimSpace(req.Shipping.Zip),
		BillName:           strings.TrimSpace(req.Billing.Name),
		BillPhone:          strings.TrimSpace(req.Billing.Phone),
		BillProvince:       strings.TrimSpace(req.Billing.Province),
		BillCity:           strings.TrimSpace(req.Billing.City),
		BillDistrict:       strings.TrimSpace(req.Billing.District),
		BillAddress:        strings.TrimSpace(req.Billing.Address),
		BillZip:            strings.TrimSpace(req.Billing.Zip),
		PaymentMethod:      strings.TrimSpace(req.PaymentMethod),
		PaymentMethodTitle: strings.TrimSpace(req.PaymentMethodTitle),
		CreatedVia:         defaultString(req.CreatedVia, ordermodel.CreatedViaCheckout),
		IPAddress:          strings.TrimSpace(req.IPAddress),
		UserAgent:          strings.TrimSpace(req.UserAgent),
		RequestID:          strings.TrimSpace(req.RequestID),
		Remark:             strings.TrimSpace(req.Remark),
		AdminNote:          strings.TrimSpace(req.AdminNote),
		CreateBy:           req.CreateBy,
		CreateTime:         now,
		UpdateTime:         now,
	}
	if head.Attribution, err = marshalAttribution(req.Attribution); err != nil {
		return nil, err
	}

	// ① 订单（头 + 项 + 流水）一个事务。
	err = s.orders.Transaction(ctx, func(tx *gorm.DB) error {
		if cerr := s.orders.CreateTx(ctx, tx, head); cerr != nil {
			return cerr
		}
		for _, it := range items {
			it.OrderID = head.ID
		}
		if cerr := s.items.CreateBatchTx(ctx, tx, items); cerr != nil {
			return cerr
		}
		return s.logs.CreateTx(ctx, tx, &ordermodel.OrderStatusLogEntity{
			OrderID:      head.ID,
			FromStatus:   "",
			ToStatus:     ordermodel.OrderStatusPending,
			OperatorType: operatorTypeOf(head.CreatedVia),
			OperatorID:   head.CreateBy,
			Remark:       "建单",
			CreateTime:   now,
		})
	})
	if err != nil {
		return nil, err
	}

	// ② 扣库存（跨模块，落在订单事务之外）。
	lines := make([]inventorydto.StockChangeLineReq, 0, len(items))
	for _, it := range items {
		lines = append(lines, inventorydto.StockChangeLineReq{
			ProductID: it.ProductID,
			VariantID: it.VariantID,
			SKUCode:   it.SKU,
			Quantity:  it.Quantity,
		})
	}
	_, dErr := s.stock.DeductStock(ctx, &inventorydto.DeductStockReq{
		ProjectID:  projectID,
		ReasonCode: "sale_out",
		SourceType: "order",
		SourceRef:  head.OrderNo,
		Remark:     "订单出库",
		Lines:      lines,
	})
	if dErr != nil {
		// ③ 补偿：标记取消。库存不足是业务常态（并发抢最后一件），
		// 不能把「没扣到库存」的单留成待付款。
		reason := orderenums.ErrStockInsufficient
		msg := strings.ToLower(dErr.Error())
		if !strings.Contains(msg, "不足") && !strings.Contains(msg, "insufficient") {
			reason = orderenums.ErrStockUnavailable
		}
		_ = s.markAutoCancelled(ctx, head.ID, reason)
		return nil, errors.New(reason)
	}

	return &orderdto.CreateOrderResp{
		ID: head.ID, OrderNo: head.OrderNo, Status: head.Status,
		Total: head.Total, Currency: head.Currency, Duplicated: false,
	}, nil
}

// markAutoCancelled 库存失败后的补偿：把订单置为已取消并记一条流转。
//
// 补偿本身的失败**不再向上冒**：调用方已经要拿到「库存不足」这个结论了，
// 再叠一个补偿错误只会让原因变得看不出主次；订单留在 pending 会被后续的人工处理看到。
func (s *Service) markAutoCancelled(ctx context.Context, orderID uint64, reason string) error {
	now := time.Now()
	err := s.orders.Transaction(ctx, func(tx *gorm.DB) error {
		if uerr := s.orders.UpdateFieldsTx(ctx, tx, orderID, map[string]any{
			"status":        ordermodel.OrderStatusCancelled,
			"cancel_reason": reason,
			"update_time":   now,
		}); uerr != nil {
			return uerr
		}
		return s.logs.CreateTx(ctx, tx, &ordermodel.OrderStatusLogEntity{
			OrderID:      orderID,
			FromStatus:   ordermodel.OrderStatusPending,
			ToStatus:     ordermodel.OrderStatusCancelled,
			OperatorType: ordermodel.OperatorTypeSystem,
			Remark:       reason,
			CreateTime:   now,
		})
	})
	return err
}

// newOrderNo 生成订单号：时间前缀 + 随机后缀。
//
// 时间前缀便于人工识别与归档（客服报单号时能看出是哪天），随机后缀避免同秒碰撞；
// 唯一约束兜底，撞了就重试（概率极低，但重试比报错好）。
func (s *Service) newOrderNo(ctx context.Context, projectID string) (no string, err error) {
	const attempts = 5
	buf := make([]byte, 4)
	for i := 0; i < attempts; i++ {
		if _, rerr := rand.Read(buf); rerr != nil {
			return "", rerr
		}
		no = fmt.Sprintf("GWP%s%08X", time.Now().Format("20060102"), uint32(buf[0])<<24|uint32(buf[1])<<16|uint32(buf[2])<<8|uint32(buf[3]))
		existing, gerr := s.orders.GetByNo(ctx, projectID, no)
		if gerr != nil {
			return "", gerr
		}
		if existing == nil {
			return no, nil
		}
	}
	return "", errors.New(orderenums.ErrOrderNoTaken)
}

// operatorTypeOf 下单入口 → 操作人类型。
func operatorTypeOf(createdVia string) string {
	if createdVia == ordermodel.CreatedViaAdmin {
		return ordermodel.OperatorTypeAdmin
	}
	return ordermodel.OperatorTypeCustomer
}

func defaultString(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

// centsToYuanLabel 分 → 元的展示串（仅用于备注与提示，不参与计算）。
func centsToYuanLabel(cents int64) string {
	return fmt.Sprintf("%.2f", float64(cents)/100)
}

// marshalAttribution 归因快照 → JSONB。
//
// nil 表示没有访客上下文（后台代客下单没有追踪数据），落空对象而不是 NULL ——
// 列是 NOT NULL，且「没有归因数据」与「这一列不存在」对下游是两件事。
func marshalAttribution(a *orderdto.Attribution) (json.RawMessage, error) {
	if a == nil {
		return json.RawMessage("{}"), nil
	}
	b, err := json.Marshal(a)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}
