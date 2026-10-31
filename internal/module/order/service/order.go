package orderservice

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	membershipcontract "go_wp/internal/module/membership/contract"
	membershipdto "go_wp/internal/module/membership/dto"
	ordercontract "go_wp/internal/module/order/contract"
	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
	productcontract "go_wp/internal/module/product/contract"
	usercontract "go_wp/internal/module/user/contract"
	webhookcontract "go_wp/internal/module/webhook/contract"
	"go_wp/pkg/database"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/rls"
	"go_wp/pkg/utils"
)

const (
	maxOrderItems = 100

	maxItemQuantity = 100000
)

func (s *Service) CreateOrder(ctx context.Context, req *orderdto.CreateOrderReq) (res *orderdto.CreateOrderResp, err error) {
	return s.createOrder(ctx, req, ordermodel.CreatedViaCheckout)
}

func (s *Service) CreateAPIOrder(ctx context.Context, req *orderdto.CreateOrderReq) (res *orderdto.CreateOrderResp, err error) {
	return s.createOrder(ctx, req, ordermodel.CreatedViaAPI)
}

func (s *Service) CreateAdminOrder(ctx context.Context, req *orderdto.CreateOrderReq) (res *orderdto.CreateOrderResp, err error) {
	return s.createOrder(ctx, req, ordermodel.CreatedViaAdmin)
}

func (s *Service) createOrder(ctx context.Context, req *orderdto.CreateOrderReq, createdVia string) (res *orderdto.CreateOrderResp, err error) {
	if err = validateCreateOrderReq(req); err != nil {
		return nil, err
	}

	if err = s.validateOrderCurrency(ctx, req); err != nil {
		return nil, err
	}
	projectID := strings.TrimSpace(req.ProjectID)

	existing, err := s.orderByRequestID(ctx, projectID, req.RequestID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}

	draft, err := s.buildOrderDraft(ctx, req, projectID, createdVia)
	if err != nil {
		return nil, err
	}

	var pendingMail *usercontract.GuestAccountMail
	if err = s.orders.Transaction(ctx, func(tx *gorm.DB) error {
		userID, mail, perr := s.provisionGuestAccountTx(ctx, tx, req)
		if perr != nil {
			return perr
		}
		if !sameUserID(userID, draft.head.UserID) {

			draft.head.UserID = userID
			s.applyLineMoney(ctx, draft, userID)
		}
		pendingMail = mail
		return s.persistOrderTx(ctx, tx, draft)
	}); err != nil {

		if database.IsUniqueViolation(err) {
			if dup, derr := s.orderByRequestID(ctx, projectID, req.RequestID); derr == nil && dup != nil {
				return dup, nil
			}
		}
		return nil, err
	}

	if pendingMail != nil && s.guest != nil && s.guest.SendGuestAccountMail(ctx, pendingMail) == nil {
		draft.accountMailed = true
	}

	return draft.response(), nil
}

func sameUserID(a, b *uint64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func validateCreateOrderReq(req *orderdto.CreateOrderReq) error {
	if req == nil {
		return orderenums.Err(orderenums.ErrInvalidParam)
	}
	if strings.TrimSpace(req.ProjectID) == "" {
		return orderenums.Err(orderenums.ErrProjectRequired)
	}
	email := strings.TrimSpace(req.CustomerEmail)
	if email == "" {
		return orderenums.Err(orderenums.ErrCustomerEmailRequired)
	}
	if !strings.Contains(email, "@") || len(email) < 3 {
		return orderenums.Err(orderenums.ErrCustomerEmailInvalid)
	}
	if len(req.Items) == 0 {
		return orderenums.Err(orderenums.ErrItemsRequired)
	}
	if len(req.Items) > maxOrderItems {
		return orderenums.Err(orderenums.ErrItemLimitExceeded)
	}
	return nil
}

func (s *Service) validateOrderCurrency(ctx context.Context, req *orderdto.CreateOrderReq) error {
	want := strings.ToUpper(strings.TrimSpace(req.Currency))
	if want == "" || s.currencies == nil {
		return nil
	}
	codes, err := s.currencies.ListCurrencyCodes(ctx)
	if err != nil {

		return nil
	}
	if len(codes) == 0 {

		return nil
	}
	for _, c := range codes {
		if strings.EqualFold(strings.TrimSpace(c), want) {
			return nil
		}
	}
	return orderenums.Err(orderenums.ErrCurrencyUnknown)
}

func (s *Service) orderByRequestID(ctx context.Context, projectID, requestID string) (*orderdto.CreateOrderResp, error) {
	reqID := strings.TrimSpace(requestID)
	if reqID == "" {
		return nil, nil
	}
	existing, err := s.orders.GetByRequestID(ctx, projectID, reqID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, nil
	}
	return &orderdto.CreateOrderResp{
		ID: existing.ID, OrderNo: existing.OrderNo, Status: existing.Status,
		Total: existing.Total, Currency: existing.Currency, Duplicated: true,
	}, nil
}

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
	return "", orderenums.Err(orderenums.ErrOrderNoTaken)
}

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

func centsToYuanLabel(cents int64) string {
	return fmt.Sprintf("%.2f", float64(cents)/100)
}

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

type orderDraft struct {
	projectID     string
	head          *ordermodel.OrderEntity
	items         []*ordermodel.OrderItemEntity
	appliedCoupon *ordermodel.CouponEntity
	accountMailed bool
	now           time.Time

	subtotal       int64
	couponDiscount int64
	shippingTotal  int64
}

func (d *orderDraft) response() *orderdto.CreateOrderResp {
	return &orderdto.CreateOrderResp{
		ID: d.head.ID, OrderNo: d.head.OrderNo, Status: d.head.Status,
		Total: d.head.Total, Currency: d.head.Currency, Duplicated: false,
		AccountMailed: d.accountMailed,
	}
}

func (s *Service) buildOrderDraft(ctx context.Context, req *orderdto.CreateOrderReq, projectID, createdVia string) (*orderDraft, error) {
	now := time.Now()
	email := strings.TrimSpace(req.CustomerEmail)

	items, subtotal, err := s.buildOrderItems(ctx, req, projectID, now)
	if err != nil {
		return nil, err
	}

	appliedCoupon, discount, err := s.resolveCoupon(ctx, projectID, req.CouponCode, subtotal)
	if err != nil {
		return nil, err
	}

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
	var fxRatePtr *string

	fxRate, fxBase := s.orderFXSnapshot(ctx, orderCurrencyOf(req), orderCurrency())
	if fxRate != "" {
		fxRatePtr = &fxRate
	}

	userID := s.resolveDraftUserID(ctx, req)

	orderNo, err := s.newOrderNo(ctx, projectID)
	if err != nil {
		return nil, err
	}

	head := &ordermodel.OrderEntity{
		ProjectID: projectID,
		OrderNo:   orderNo,
		Status:    ordermodel.OrderStatusPending,

		FulfillmentStatus: ordermodel.FulfillmentPending,
		UserID:            userID,
		CustomerEmail:     email,
		CustomerName:      strings.TrimSpace(req.CustomerName),
		CustomerPhone:     strings.TrimSpace(req.CustomerPhone),

		Currency: orderCurrencyOf(req),

		FXRate:     fxRatePtr,
		FXBaseCode: fxBase,
		Subtotal:   subtotal,

		DiscountTotal: discount,
		ShippingTotal: shipping,
		TaxTotal:      0,
		ShipName:      strings.TrimSpace(req.Shipping.Name),
		ShipPhone:     strings.TrimSpace(req.Shipping.Phone),
		ShipProvince:  strings.TrimSpace(req.Shipping.Province),
		ShipCity:      strings.TrimSpace(req.Shipping.City),
		ShipDistrict:  strings.TrimSpace(req.Shipping.District),
		ShipAddress:   strings.TrimSpace(req.Shipping.Address),
		ShipZip:       strings.TrimSpace(req.Shipping.Zip),

		ShipCountry:        strings.TrimSpace(req.Shipping.Country),
		BillName:           strings.TrimSpace(req.Billing.Name),
		BillPhone:          strings.TrimSpace(req.Billing.Phone),
		BillProvince:       strings.TrimSpace(req.Billing.Province),
		BillCity:           strings.TrimSpace(req.Billing.City),
		BillDistrict:       strings.TrimSpace(req.Billing.District),
		BillAddress:        strings.TrimSpace(req.Billing.Address),
		BillZip:            strings.TrimSpace(req.Billing.Zip),
		BillCountry:        strings.TrimSpace(req.Billing.Country),
		PaymentMethod:      strings.TrimSpace(req.PaymentMethod),
		PaymentMethodTitle: strings.TrimSpace(req.PaymentMethodTitle),
		CreatedVia:         createdVia,
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

	d := &orderDraft{
		projectID:      projectID,
		head:           head,
		items:          items,
		appliedCoupon:  appliedCoupon,
		subtotal:       subtotal,
		couponDiscount: discount,
		shippingTotal:  shipping,
		now:            now,
	}

	s.applyLineMoney(ctx, d, userID)
	return d, nil
}

func (s *Service) applyLineMoney(ctx context.Context, d *orderDraft, userID *uint64) {
	if d == nil || d.head == nil {
		return
	}
	membership := s.resolveMembershipDiscount(ctx, d.projectID, userID, d.subtotal, d.couponDiscount)
	allocateLineDiscounts(d.items, d.subtotal, d.couponDiscount+membership)
	d.head.MembershipDiscountTotal = membership
	d.head.DiscountTotal = d.couponDiscount
	d.head.Total = d.subtotal - d.couponDiscount - membership + d.shippingTotal
}

func (s *Service) resolveDraftUserID(ctx context.Context, req *orderdto.CreateOrderReq) *uint64 {
	if req == nil || req.UserID != nil || s.guest == nil {
		return req.UserID
	}
	if req.ProvisionGuestAccount != nil && !*req.ProvisionGuestAccount {
		return nil
	}
	if uid, found, err := s.guest.LookupGuestAccount(ctx, strings.TrimSpace(req.CustomerEmail)); err == nil && found && uid != 0 {
		return &uid
	}
	return nil
}

func (s *Service) buildOrderItems(ctx context.Context, req *orderdto.CreateOrderReq, projectID string, now time.Time) ([]*ordermodel.OrderItemEntity, int64, error) {
	variantIDs := make([]string, 0, len(req.Items))
	for _, it := range req.Items {
		variantIDs = append(variantIDs, strings.TrimSpace(it.VariantID))
	}

	snapshots, warehouseOf, err := s.resolveOrderSnapshots(ctx, req, projectID)
	if err != nil {
		return nil, 0, err
	}
	byID := make(map[string]*productcontract.VariantSnapshot, len(snapshots))
	for _, sn := range snapshots {
		byID[sn.VariantID] = sn
	}

	items := make([]*ordermodel.OrderItemEntity, 0, len(req.Items))
	var subtotal int64
	for _, it := range req.Items {
		vid := strings.TrimSpace(it.VariantID)
		if vid == "" {
			return nil, 0, orderenums.Err(orderenums.ErrInvalidParam)
		}
		if it.Quantity <= 0 || it.Quantity > maxItemQuantity {
			return nil, 0, orderenums.Err(orderenums.ErrQuantityInvalid)
		}
		sn := byID[vid]
		if sn == nil {
			return nil, 0, fmt.Errorf("%w: %s", orderenums.Err(orderenums.ErrVariantNotFound), vid)
		}
		if sn.ProjectID != "" && sn.ProjectID != projectID {

			return nil, 0, orderenums.Err(orderenums.ErrVariantNotFound)
		}
		if !sn.Enabled {
			return nil, 0, orderenums.Err(orderenums.ErrVariantNotFound)
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

			WarehouseID: warehouseOf[vid],
			CreateTime:  now,
		})
	}
	return items, subtotal, nil
}

func (s *Service) resolveOrderSnapshots(ctx context.Context, req *orderdto.CreateOrderReq,
	projectID string) (list []*productcontract.VariantSnapshot,
	warehouseOf map[string]*string, err error) {
	warehouseOf = make(map[string]*string, len(req.Items))
	if s.warehouses == nil {
		list, err = s.product.VariantSnapshots(ctx, variantIDsOf(req), projectID)
		return list, warehouseOf, err
	}
	refs := make([]productcontract.VariantWarehouseRef, 0, len(req.Items))
	for _, it := range req.Items {
		vid := strings.TrimSpace(it.VariantID)
		if vid == "" {
			continue
		}
		whID, rerr := s.warehouses.ResolveShipWarehouse(ctx, projectID, strings.TrimSpace(it.WarehouseID))
		if rerr != nil {
			return nil, nil, rerr
		}
		if whID != "" {
			id := whID
			warehouseOf[vid] = &id
		}
		refs = append(refs, productcontract.VariantWarehouseRef{VariantID: vid, WarehouseID: whID})
	}

	byWarehouse, ok := s.product.(productcontract.VariantSnapshotByWarehousePort)
	if !ok {
		list, err = s.product.VariantSnapshots(ctx, variantIDsOf(req), projectID)
		return list, warehouseOf, err
	}
	list, err = byWarehouse.VariantSnapshotsInWarehouse(ctx, refs, projectID)
	return list, warehouseOf, err
}

func variantIDsOf(req *orderdto.CreateOrderReq) []string {
	out := make([]string, 0, len(req.Items))
	for _, it := range req.Items {
		out = append(out, strings.TrimSpace(it.VariantID))
	}
	return out
}

func (s *Service) resolveCoupon(ctx context.Context, projectID, couponCode string, subtotal int64) (*ordermodel.CouponEntity, int64, error) {
	code := normalizeCouponCode(couponCode)
	if code == "" {
		return nil, 0, nil
	}
	ce, err := s.coupons.GetByCode(ctx, projectID, code)
	if err != nil {
		return nil, 0, err
	}
	if ce == nil {
		return nil, 0, orderenums.Err(orderenums.ErrCouponNotFound)
	}
	if reason := couponRuleCheck(ce, subtotal, time.Now()); reason != "" {
		return nil, 0, errors.New(reason)
	}
	return ce, couponDiscount(ce, subtotal), nil
}

func (s *Service) provisionGuestAccountTx(ctx context.Context, tx *gorm.DB, req *orderdto.CreateOrderReq) (*uint64, *usercontract.GuestAccountMail, error) {
	if req == nil || req.UserID != nil || s.guest == nil {
		return req.UserID, nil, nil
	}

	if req.ProvisionGuestAccount != nil && !*req.ProvisionGuestAccount {
		return req.UserID, nil, nil
	}
	gres, gerr := s.guest.EnsureGuestAccountTx(ctx, tx, &usercontract.GuestAccountInput{
		Email:      strings.TrimSpace(req.CustomerEmail),
		Name:       strings.TrimSpace(req.CustomerName),
		Locale:     req.Locale,
		RegisterIP: req.IPAddress,
	})
	if gerr != nil || gres == nil || gres.UserID == 0 {
		if gerr != nil {
			logger.Scene("order").Error(gerr, "访客开号失败（不阻断下单）")
		}
		return req.UserID, nil, nil
	}
	id := gres.UserID
	return &id, gres.PendingMail, nil
}

func orderCurrencyOf(req *orderdto.CreateOrderReq) string {
	if req != nil {
		if v := strings.ToUpper(strings.TrimSpace(req.Currency)); v != "" {
			return v
		}
	}
	return orderCurrency()
}

func orderCurrency() string {
	if v := strings.TrimSpace(i18n.GetDefaultCurrency()); v != "" {
		return v
	}
	return "CNY"
}

func (s *Service) persistOrderTx(ctx context.Context, tx *gorm.DB, d *orderDraft) error {
	if cerr := s.orders.CreateTx(ctx, tx, d.head); cerr != nil {
		return cerr
	}
	for _, it := range d.items {
		it.OrderID = d.head.ID
	}
	if cerr := s.items.CreateBatchTx(ctx, tx, d.items); cerr != nil {
		return cerr
	}
	if lerr := s.logs.CreateTx(ctx, tx, &ordermodel.OrderStatusLogEntity{
		OrderID:      d.head.ID,
		FromStatus:   "",
		ToStatus:     ordermodel.OrderStatusPending,
		OperatorType: operatorTypeOf(d.head.CreatedVia),
		OperatorID:   d.head.CreateBy,
		Remark:       "建单",
		CreateTime:   d.now,
	}); lerr != nil {
		return lerr
	}
	if rerr := s.redeemCouponTx(ctx, tx, d.appliedCoupon, d.head.ID, d.head.OrderNo, d.head.DiscountTotal, d.head.UserID, d.now); rerr != nil {
		return rerr
	}

	return s.reserveStockTx(ctx, tx, d)
}

func (s *Service) reserveStockTx(ctx context.Context, tx *gorm.DB, d *orderDraft) error {
	lines := make([]ordercontract.StockLine, 0, len(d.items))
	for _, it := range d.items {
		if it.WarehouseID == nil || strings.TrimSpace(*it.WarehouseID) == "" {
			continue
		}
		lines = append(lines, ordercontract.StockLine{
			ProductID:   it.ProductID,
			VariantID:   it.VariantID,
			SKUCode:     it.SKU,
			Quantity:    it.Quantity,
			WarehouseID: *it.WarehouseID,
		})
	}
	if len(lines) == 0 {
		return nil
	}
	rErr := s.stock.ReserveStockTx(ctx, tx, &ordercontract.StockReservation{
		OrderID:    int64(d.head.ID),
		ProjectID:  d.projectID,
		SourceType: "order",
		SourceRef:  d.head.OrderNo,
		Remark:     "订单预占",
		Lines:      lines,
	})
	if rErr == nil {
		return nil
	}
	return mapStockError(rErr)
}

func mapStockError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "ErrStockInsufficient"):
		return orderenums.Err(orderenums.ErrStockInsufficient)
	case strings.Contains(msg, "ErrReserveUnderflow"):

		return orderenums.Err(orderenums.ErrStockUnavailable)
	case strings.HasPrefix(msg, "Err") || strings.HasPrefix(msg, "order.err."):

		return err
	default:

		return orderenums.Err(orderenums.ErrStockUnavailable)
	}
}

func allocateLineDiscounts(items []*ordermodel.OrderItemEntity, subtotal, discount int64) {
	if len(items) == 0 {
		return
	}
	if discount <= 0 || subtotal <= 0 {
		for _, it := range items {
			it.LineDiscount = 0
			it.LineTotal = it.LineSubtotal
		}
		return
	}
	last := len(items) - 1
	var allocated int64
	for i, it := range items {
		if i == last {
			continue
		}
		it.LineDiscount = discount * it.LineSubtotal / subtotal
		if it.LineDiscount > it.LineSubtotal {

			it.LineDiscount = it.LineSubtotal
		}
		allocated += it.LineDiscount
	}

	rest := discount - allocated
	if rest < 0 {
		rest = 0
	}
	items[last].LineDiscount = min64(rest, items[last].LineSubtotal)

	deficit := rest - items[last].LineDiscount
	for i := last - 1; i >= 0 && deficit > 0; i-- {
		room := items[i].LineSubtotal - items[i].LineDiscount
		if room <= 0 {
			continue
		}
		add := min64(room, deficit)
		items[i].LineDiscount += add
		deficit -= add
	}

	for _, it := range items {

		if it.LineDiscount < 0 {
			it.LineDiscount = 0
		}
		if it.LineDiscount > it.LineSubtotal {
			it.LineDiscount = it.LineSubtotal
		}
		it.LineTotal = it.LineSubtotal - it.LineDiscount
	}
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func lineRefundAmount(it *ordermodel.OrderItemEntity, qty int) int64 {
	if it == nil || qty <= 0 {
		return 0
	}
	if it.Quantity <= 0 {
		return 0
	}
	return it.LineTotal * int64(qty) / int64(it.Quantity)
}

func (s *Service) SetMembershipReader(reader membershipcontract.Reader) {
	if s == nil {
		return
	}
	s.membership = reader
}

func membershipDiscountAmount(subtotal, remaining, percent int64) int64 {
	if percent <= 0 || subtotal <= 0 || remaining <= 0 {
		return 0
	}
	if percent > 100 {
		percent = 100
	}
	amount := subtotal * percent / 100
	if amount > remaining {
		amount = remaining
	}
	if amount < 0 {
		return 0
	}
	return amount
}

func (s *Service) resolveMembershipDiscount(ctx context.Context, projectID string, userID *uint64, subtotal, couponDiscount int64) (discount int64) {
	if s.membership == nil || userID == nil || *userID == 0 {
		return 0
	}
	if strings.TrimSpace(projectID) == "" {
		return 0
	}
	member, err := s.membership.Resolve(ctx, &membershipdto.ResolveReq{
		ProjectID: projectID,
		UserID:    *userID,
	})
	if err != nil {
		logger.Scene("order").
			With("projectId", projectID).With("userId", *userID).
			Warn("会员身份解析失败，本单按无会员折扣建单（折扣降级为 0，不拒单）")
		return 0
	}
	if member == nil {
		return 0
	}
	remaining := subtotal - couponDiscount
	if remaining < 0 {
		remaining = 0
	}
	return membershipDiscountAmount(subtotal, remaining, member.DiscountPercent)
}

func (s *Service) PayOrder(ctx context.Context, req *orderdto.PayOrderReq) (res *orderdto.PayOrderResp, err error) {
	if req == nil || req.OrderID == 0 {
		return nil, orderenums.Err(orderenums.ErrInvalidParam)
	}
	method := strings.TrimSpace(req.PaymentMethod)
	if method == "" {

		return nil, orderenums.Err(orderenums.ErrPaymentMethodRequired)
	}
	title := strings.TrimSpace(req.PaymentMethodTitle)
	txnID := strings.TrimSpace(req.TransactionID)
	remark := strings.TrimSpace(req.Remark)
	now := time.Now()

	res = &orderdto.PayOrderResp{}

	projectID, perr := s.locateOrderProject(ctx, req.OrderID)
	if perr != nil {
		return nil, perr
	}
	var paidEvent *OrderPaidEvent
	err = s.orders.Transaction(ctx, func(tx *gorm.DB) error {
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

		switch e.Status {
		case ordermodel.OrderStatusPaid, ordermodel.OrderStatusShipped, ordermodel.OrderStatusCompleted:
			res.ID, res.OrderNo, res.Status = e.ID, e.OrderNo, e.Status
			res.TransactionID = e.TransactionID
			res.AlreadyPaid = true
			return nil
		case ordermodel.OrderStatusCancelled, ordermodel.OrderStatusRefunded:

			remark := defaultString(remark, "支付成功但订单已终态，待人工核对")
			if txnID != "" {
				remark += "（流水号 " + txnID + "）"
			}
			if cerr := s.logs.CreateTx(ctx, tx, &ordermodel.OrderStatusLogEntity{
				OrderID:      e.ID,
				FromStatus:   e.Status,
				ToStatus:     e.Status,
				OperatorType: defaultString(req.OperatorType, ordermodel.OperatorTypeSystem),
				OperatorID:   req.OperatorID,
				OperatorName: strings.TrimSpace(req.OperatorName),
				Remark:       remark,
				CreateTime:   now,
			}); cerr != nil {
				return cerr
			}
			res.ID, res.OrderNo, res.Status = e.ID, e.OrderNo, e.Status
			res.TransactionID = txnID
			res.NeedsManualReview = true
			return nil
		}
		if !canTransition(e.Status, ordermodel.OrderStatusPaid) {
			return errors.New(transitionError(e.Status, ordermodel.OrderStatusPaid))
		}

		fields := map[string]any{
			"status":         ordermodel.OrderStatusPaid,
			"paid_at":        now,
			"update_time":    now,
			"payment_method": method,

			"fulfillment_status": ordermodel.FulfillmentPaid,
		}

		if title != "" {
			fields["payment_method_title"] = title
		}
		if txnID != "" {
			fields["transaction_id"] = txnID
		}
		if uerr := s.orders.UpdateFieldsTx(ctx, tx, e.ProjectID, e.ID, fields); uerr != nil {
			return uerr
		}
		if cerr := s.logs.CreateTx(ctx, tx, &ordermodel.OrderStatusLogEntity{
			OrderID:      e.ID,
			FromStatus:   e.Status,
			ToStatus:     ordermodel.OrderStatusPaid,
			OperatorType: defaultString(req.OperatorType, ordermodel.OperatorTypeSystem),
			OperatorID:   req.OperatorID,
			OperatorName: strings.TrimSpace(req.OperatorName),
			Remark:       defaultString(remark, "支付成功"),
			CreateTime:   now,
		}); cerr != nil {
			return cerr
		}
		res.ID, res.OrderNo, res.Status = e.ID, e.OrderNo, ordermodel.OrderStatusPaid
		res.TransactionID = txnID
		paidEvent = &OrderPaidEvent{
			OrderID:       e.ID,
			OrderNo:       e.OrderNo,
			Status:        ordermodel.OrderStatusPaid,
			Currency:      e.Currency,
			Total:         e.Total,
			Subtotal:      e.Subtotal,
			DiscountTotal: e.DiscountTotal,
			ShippingTotal: e.ShippingTotal,
			TaxTotal:      e.TaxTotal,
			PaymentMethod: method,
			TransactionID: txnID,
			PaidAt:        now,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if paidEvent != nil {
		s.dispatchEvent(ctx, webhookcontract.EventOrderPaid, paidEvent)
	}
	return res, nil
}

var allowedTransitions = map[string]map[string]bool{
	ordermodel.OrderStatusPending: {
		ordermodel.OrderStatusPaid:      true,
		ordermodel.OrderStatusCancelled: true,
	},
	ordermodel.OrderStatusPaid: {
		ordermodel.OrderStatusShipped:   true,
		ordermodel.OrderStatusCancelled: true,
		ordermodel.OrderStatusRefunded:  true,
	},
	ordermodel.OrderStatusShipped: {
		ordermodel.OrderStatusCompleted: true,
		ordermodel.OrderStatusRefunded:  true,
	},
	ordermodel.OrderStatusCompleted: {
		ordermodel.OrderStatusRefunded: true,
	},
	ordermodel.OrderStatusCancelled: {},
	ordermodel.OrderStatusRefunded:  {},
}

func isKnownStatus(s string) bool {
	for _, v := range []string{
		ordermodel.OrderStatusPending, ordermodel.OrderStatusPaid, ordermodel.OrderStatusShipped,
		ordermodel.OrderStatusCompleted, ordermodel.OrderStatusCancelled, ordermodel.OrderStatusRefunded,
	} {
		if s == v {
			return true
		}
	}
	return false
}

func canTransition(from, to string) bool {
	edges, ok := allowedTransitions[from]
	if !ok {
		return false
	}
	return edges[to]
}

func (s *Service) ChangeStatus(ctx context.Context, req *orderdto.ChangeStatusReq) (err error) {
	if req == nil || req.OrderID == 0 {
		return orderenums.Err(orderenums.ErrInvalidParam)
	}
	to := strings.TrimSpace(req.ToStatus)
	if !isKnownStatus(to) {
		return orderenums.Err(orderenums.ErrStatusInvalid)
	}

	if to == ordermodel.OrderStatusCancelled || to == ordermodel.OrderStatusRefunded {
		return orderenums.Err(orderenums.ErrStatusTransition)
	}

	projectID, perr := s.locateOrderProject(ctx, req.OrderID)
	if perr != nil {
		return perr
	}
	now := time.Now()
	return s.orders.Transaction(ctx, func(tx *gorm.DB) error {
		return s.changeStatusTx(ctx, tx, projectID, req, now)
	})
}

func (s *Service) changeStatusTx(ctx context.Context, tx *gorm.DB, projectID string,
	req *orderdto.ChangeStatusReq, now time.Time) error {
	to := strings.TrimSpace(req.ToStatus)
	{
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
		if !canTransition(e.Status, to) {
			return errors.New(transitionError(e.Status, to))
		}
		fields := map[string]any{
			"status":             to,
			"update_time":        now,
			"fulfillment_status": ordermodel.FulfillmentOfStatus(to),
		}

		switch to {
		case ordermodel.OrderStatusPaid:
			fields["paid_at"] = now
		case ordermodel.OrderStatusCompleted:
			fields["completed_at"] = now
		case ordermodel.OrderStatusShipped:
			fields["completed_at"] = nil
		}
		if uerr := s.orders.UpdateFieldsTx(ctx, tx, e.ProjectID, e.ID, fields); uerr != nil {
			return uerr
		}
		return s.logs.CreateTx(ctx, tx, &ordermodel.OrderStatusLogEntity{
			OrderID:      e.ID,
			FromStatus:   e.Status,
			ToStatus:     to,
			OperatorType: defaultString(req.OperatorType, ordermodel.OperatorTypeAdmin),
			OperatorID:   req.OperatorID,
			OperatorName: strings.TrimSpace(req.OperatorName),
			Remark:       strings.TrimSpace(req.Remark),
			CreateTime:   now,
		})
	}
}

func (s *Service) ChangeStatusBatch(ctx context.Context, reqs []*orderdto.ChangeStatusReq) (err error) {
	if len(reqs) == 0 {
		return orderenums.Err(orderenums.ErrInvalidParam)
	}
	now := time.Now()
	return s.orders.Transaction(ctx, func(tx *gorm.DB) error {
		for _, req := range reqs {
			if req == nil || req.OrderID == 0 {
				return orderenums.Err(orderenums.ErrInvalidParam)
			}
			to := strings.TrimSpace(req.ToStatus)
			if !isKnownStatus(to) {
				return orderenums.Err(orderenums.ErrStatusInvalid)
			}
			if to == ordermodel.OrderStatusCancelled || to == ordermodel.OrderStatusRefunded {
				return orderenums.Err(orderenums.ErrStatusTransition)
			}
			projectID, perr := s.locateOrderProject(ctx, req.OrderID)
			if perr != nil {
				return perr
			}
			if terr := s.changeStatusTx(ctx, tx, projectID, req, now); terr != nil {
				return terr
			}
		}
		return nil
	})
}

func (s *Service) CancelOrder(ctx context.Context, req *orderdto.CancelOrderReq) (res *orderdto.CancelOrderResp, err error) {
	if req == nil || req.OrderID == 0 {
		return nil, orderenums.Err(orderenums.ErrInvalidParam)
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		return nil, orderenums.Err(orderenums.ErrCancelReasonRequired)
	}

	scopeProjectID, perr := s.resolveOrderProject(ctx, req.OrderID, req.ProjectID)
	if perr != nil {
		return nil, perr
	}
	now := time.Now()
	err = s.orders.Transaction(ctx, func(tx *gorm.DB) error {
		return s.cancelOrderTx(ctx, tx, scopeProjectID, req, reason, scopeProjectID, now)
	})
	if err != nil {
		return nil, err
	}
	return &orderdto.CancelOrderResp{}, nil
}

func (s *Service) cancelOrderTx(ctx context.Context, tx *gorm.DB, scopeProjectID string,
	req *orderdto.CancelOrderReq, reason, _ string, now time.Time) error {
	{
		if serr := rls.ScopeTx(tx, scopeProjectID); serr != nil {
			return serr
		}
		e, lerr := s.orders.LockByIDTx(ctx, tx, scopeProjectID, req.OrderID)
		if lerr != nil {
			return lerr
		}
		if e == nil {
			return orderenums.Err(orderenums.ErrOrderNotFound)
		}
		if !canTransition(e.Status, ordermodel.OrderStatusCancelled) {
			return errors.New(transitionError(e.Status, ordermodel.OrderStatusCancelled))
		}
		if uerr := s.orders.UpdateFieldsTx(ctx, tx, e.ProjectID, e.ID, map[string]any{
			"status":        ordermodel.OrderStatusCancelled,
			"cancel_reason": reason,
			"update_time":   now,

			"fulfillment_status": ordermodel.FulfillmentCancelled,
		}); uerr != nil {
			return uerr
		}
		if lerr := s.logs.CreateTx(ctx, tx, &ordermodel.OrderStatusLogEntity{
			OrderID:      e.ID,
			FromStatus:   e.Status,
			ToStatus:     ordermodel.OrderStatusCancelled,
			OperatorType: defaultString(req.OperatorType, ordermodel.OperatorTypeAdmin),
			OperatorID:   req.OperatorID,
			OperatorName: strings.TrimSpace(req.OperatorName),
			Remark:       reason,
			CreateTime:   now,
		}); lerr != nil {
			return lerr
		}

		if rerr := s.releaseOrderReservationTx(ctx, tx, e, "取消订单释放预占："+reason); rerr != nil {
			return rerr
		}

		if s.coupons != nil {
			if _, crerr := s.coupons.ReleaseRedemptionByOrderTx(ctx, tx, e.ID); crerr != nil {
				return crerr
			}
		}
		return nil
	}
}

func (s *Service) CancelOrderBatch(ctx context.Context, reqs []*orderdto.CancelOrderReq) (err error) {
	if len(reqs) == 0 {
		return orderenums.Err(orderenums.ErrInvalidParam)
	}
	now := time.Now()
	return s.orders.Transaction(ctx, func(tx *gorm.DB) error {
		for _, req := range reqs {
			if req == nil || req.OrderID == 0 {
				return orderenums.Err(orderenums.ErrInvalidParam)
			}
			reason := strings.TrimSpace(req.Reason)
			if reason == "" {
				return orderenums.Err(orderenums.ErrCancelReasonRequired)
			}
			scopeProjectID, perr := s.resolveOrderProject(ctx, req.OrderID, req.ProjectID)
			if perr != nil {
				return perr
			}
			if terr := s.cancelOrderTx(ctx, tx, scopeProjectID, req, reason, scopeProjectID, now); terr != nil {
				return terr
			}
		}
		return nil
	})
}

func (s *Service) RefundOrder(ctx context.Context, req *orderdto.RefundOrderReq) (err error) {
	if req == nil || req.OrderID == 0 {
		return orderenums.Err(orderenums.ErrInvalidParam)
	}

	projectID, perr := s.locateOrderProject(ctx, req.OrderID)
	if perr != nil {
		return perr
	}
	return s.orders.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return serr
		}
		return s.refundOrderTx(ctx, tx, projectID, req, true)
	})
}

func (s *Service) refundOrderTx(ctx context.Context, tx *gorm.DB, projectID string,
	req *orderdto.RefundOrderReq, restock bool) error {
	now := time.Now()
	e, lerr := s.orders.LockByIDTx(ctx, tx, projectID, req.OrderID)
	if lerr != nil {
		return lerr
	}
	if e == nil {
		return orderenums.Err(orderenums.ErrOrderNotFound)
	}
	if !canTransition(e.Status, ordermodel.OrderStatusRefunded) {
		return errors.New(transitionError(e.Status, ordermodel.OrderStatusRefunded))
	}

	unshipped := e.Status == ordermodel.OrderStatusPaid
	fields := map[string]any{
		"status":      ordermodel.OrderStatusRefunded,
		"update_time": now,

		"fulfillment_status": ordermodel.FulfillmentRefunded,
	}
	if tid := strings.TrimSpace(req.TransactionID); tid != "" {
		fields["transaction_id"] = tid
	}
	if uerr := s.orders.UpdateFieldsTx(ctx, tx, e.ProjectID, e.ID, fields); uerr != nil {
		return uerr
	}

	if restock && unshipped {
		if rerr := s.releaseOrderReservationTx(ctx, tx, e, strings.TrimSpace(req.Reason)); rerr != nil {
			return rerr
		}
	}
	return s.logs.CreateTx(ctx, tx, &ordermodel.OrderStatusLogEntity{
		OrderID:      e.ID,
		FromStatus:   e.Status,
		ToStatus:     ordermodel.OrderStatusRefunded,
		OperatorType: defaultString(req.OperatorType, ordermodel.OperatorTypeAdmin),
		OperatorID:   req.OperatorID,
		OperatorName: strings.TrimSpace(req.OperatorName),
		Remark:       strings.TrimSpace(req.Reason),
		CreateTime:   now,
	})
}

func (s *Service) releaseOrderReservationTx(ctx context.Context, tx *gorm.DB,
	e *ordermodel.OrderEntity, remark string) error {
	items, ierr := s.items.ListByOrderIDTx(ctx, tx, e.ID)
	if ierr != nil {
		return ierr
	}
	if len(items) == 0 {
		return nil
	}
	itemIDs := make([]uint64, 0, len(items))
	for _, it := range items {
		itemIDs = append(itemIDs, it.ID)
	}
	shipped := map[uint64]int{}
	if s.shipments != nil {
		var serr error
		shipped, serr = s.shipments.SumShippedQuantityTx(ctx, tx, e.ID, itemIDs)
		if serr != nil {
			return serr
		}
	}
	lines := make([]ordercontract.StockLine, 0, len(items))
	for _, it := range items {
		if it.WarehouseID == nil || strings.TrimSpace(*it.WarehouseID) == "" {
			continue
		}
		remaining := it.Quantity - shipped[it.ID]
		if remaining <= 0 {
			continue
		}
		lines = append(lines, ordercontract.StockLine{
			ProductID: it.ProductID, VariantID: it.VariantID, SKUCode: it.SKU,
			Quantity: remaining, WarehouseID: *it.WarehouseID,
		})
	}
	if len(lines) == 0 {
		return nil
	}
	if remark == "" {
		remark = "订单取消 / 退款释放预占"
	}
	if rerr := s.stock.ReleaseStockTx(ctx, tx, &ordercontract.StockReservation{
		OrderID: int64(e.ID), ProjectID: e.ProjectID,
		SourceType: "order", SourceRef: e.OrderNo, Remark: remark, Lines: lines,
	}); rerr != nil {

		if strings.Contains(rerr.Error(), "ErrStockNotFound") ||
			strings.Contains(rerr.Error(), "ErrReserveUnderflow") {
			return nil
		}
		return mapStockError(rerr)
	}
	return nil
}

func (s *Service) restockUnreturnedTx(ctx context.Context, tx *gorm.DB, e *ordermodel.OrderEntity, remark string) error {
	items, ierr := s.items.ListByOrderIDTx(ctx, tx, e.ID)
	if ierr != nil {
		return ierr
	}
	if len(items) == 0 {
		return orderenums.Err(orderenums.ErrOrderHasNoItems)
	}
	received, rerr := s.receivedByItemTx(ctx, tx, e.ProjectID, e.ID, items)
	if rerr != nil {
		return rerr
	}
	lines := restockLinesFor(items, received)
	if len(lines) == 0 {
		return nil
	}
	return s.stock.ChangeStockTx(ctx, tx, &ordercontract.StockAdjustment{
		ProjectID:  e.ProjectID,
		ReasonCode: "return_in",
		SourceType: "order",
		SourceRef:  e.OrderNo,
		Remark:     remark,
		Lines:      lines,
	})
}

func transitionError(from, to string) string {
	switch from {
	case ordermodel.OrderStatusCancelled:
		return orderenums.ErrAlreadyCancelled
	case ordermodel.OrderStatusRefunded:
		return orderenums.ErrAlreadyRefunded
	case ordermodel.OrderStatusCompleted:
		if to == ordermodel.OrderStatusCancelled {
			return orderenums.ErrOrderNotCancellable
		}
	case ordermodel.OrderStatusShipped:
		if to == ordermodel.OrderStatusCancelled {
			return orderenums.ErrOrderNotCancellable
		}
	case ordermodel.OrderStatusPending, ordermodel.OrderStatusPaid:
		if to == ordermodel.OrderStatusRefunded {
			return orderenums.ErrOrderNotRefundable
		}
	}
	return orderenums.ErrStatusTransition
}

const maxAdminNoteLen = 500

func (s *Service) UpdateOrderNote(ctx context.Context, req *orderdto.UpdateOrderNoteReq) (res *orderdto.OrderResp, err error) {
	if req == nil || req.OrderID == 0 {
		return nil, orderenums.Err(orderenums.ErrInvalidParam)
	}
	note := strings.TrimSpace(req.AdminNote)
	if len(note) > maxAdminNoteLen {
		return nil, orderenums.Err(orderenums.ErrNoteTooLong)
	}

	projectID, perr := s.locateOrderProject(ctx, req.OrderID)
	if perr != nil {
		return nil, perr
	}
	head, err := s.orders.GetByID(ctx, req.OrderID, projectID)
	if err != nil {
		return nil, err
	}
	if head == nil {
		return nil, orderenums.Err(orderenums.ErrOrderNotFound)
	}
	if err = s.orders.UpdateFields(ctx, head.ProjectID, head.ID, map[string]any{
		"admin_note":  note,
		"update_time": time.Now(),
	}); err != nil {
		return nil, err
	}
	updated, err := s.orders.GetByID(ctx, head.ID, head.ProjectID)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, orderenums.Err(orderenums.ErrOrderNotFound)
	}
	return toOrderResp(updated), nil
}

const (
	pendingOrderTTL = 30 * time.Minute

	pendingOrderExpiryInterval = 15 * time.Minute

	pendingOrderExpiryBatch = 100
)

func (s *Service) ExpirePendingOrders(ctx context.Context, olderThan time.Duration, batchSize int) (expired int, err error) {
	if olderThan <= 0 {
		olderThan = pendingOrderTTL
	}
	if batchSize <= 0 {
		batchSize = pendingOrderExpiryBatch
	}

	projectIDs, perr := s.projectIDs(ctx)
	if perr != nil {
		return 0, perr
	}
	cutoff := time.Now().Add(-olderThan)
	for _, projectID := range projectIDs {
		if ctx.Err() != nil {
			break
		}
		list, lerr := s.orders.ListPendingCreatedBefore(ctx, projectID, cutoff, batchSize)
		if lerr != nil {
			return expired, lerr
		}
		for _, o := range list {
			if ctx.Err() != nil {
				return expired, nil
			}
			_, cerr := s.CancelOrder(ctx, &orderdto.CancelOrderReq{
				OrderID:      o.ID,
				ProjectID:    projectID,
				Reason:       "待付款超时自动取消",
				OperatorType: ordermodel.OperatorTypeSystem,
			})
			if cerr != nil {
				logger.Scene("order").With("order_id", o.ID).With("order_no", o.OrderNo).
					Warn("待付款超时取消失败（下轮重试）: " + cerr.Error())
				continue
			}
			expired++
		}
	}
	if expired > 0 {
		logger.Scene("order").With("expired", expired).Info("待付款超时订单已自动取消")
	}
	return expired, nil
}

func StartPendingOrderExpiryScheduler(svc *Service) {
	if utils.IsTestProcess() {
		return
	}
	if svc == nil {
		return
	}
	go func() {
		run := func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			_, _ = svc.ExpirePendingOrders(ctx, pendingOrderTTL, pendingOrderExpiryBatch)
		}
		run()
		ticker := time.NewTicker(pendingOrderExpiryInterval)
		defer ticker.Stop()
		for range ticker.C {
			run()
		}
	}()
}

var _ productcontract.PurchaseChecker = (*Service)(nil)

func (s *Service) HasPurchasedProduct(ctx context.Context, projectID string, userID uint64, productID string) (purchased bool, err error) {
	productID = strings.TrimSpace(productID)
	if userID == 0 || productID == "" {
		return false, nil
	}
	if _, perr := uuid.Parse(productID); perr != nil {
		return false, nil
	}
	return s.orders.HasPurchasedProduct(ctx, strings.TrimSpace(projectID), userID, productID)
}

var _ membershipcontract.PurchaseSource = (*Service)(nil)

func (s *Service) SpentTotalsByUser(ctx context.Context, projectID string) (totals map[uint64]int64, err error) {
	pid := strings.TrimSpace(projectID)
	if pid == "" {
		return nil, orderenums.Err(orderenums.ErrProjectRequired)
	}
	return s.orders.SpentTotalsByProject(ctx, pid)
}
