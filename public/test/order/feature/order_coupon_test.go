package feature

import (
	"context"
	"strings"
	"testing"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
)

func (f *orderFixture) addCoupon(t *testing.T, req *orderdto.CouponSaveReq) *orderdto.CouponResp {
	t.Helper()
	if req.Status == 0 {
		req.Status = 1
	}
	req.ProjectID = f.projectID
	res, err := f.orders.CreateCoupon(context.Background(), req)
	if err != nil {
		t.Fatalf("建优惠码失败: %v", err)
	}
	return res
}

func (f *orderFixture) redemptionCount(t *testing.T, couponID uint64) int64 {
	t.Helper()
	res, err := f.orders.ListCouponRedemptions(context.Background(), &orderdto.CouponRedemptionListReq{
		ProjectID: f.projectID, CouponID: couponID,
	})
	if err != nil {
		t.Fatalf("读核销记录失败: %v", err)
	}
	return res.Total
}

func (f *orderFixture) orderCount(t *testing.T) int64 {
	t.Helper()
	res, err := f.orders.ListOrders(context.Background(), &orderdto.ListOrderReq{ProjectID: f.projectID})
	if err != nil {
		t.Fatalf("读订单列表失败: %v", err)
	}
	return res.Total
}

func TestOrderNoCouponIgnoresClientDiscount(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "笔记本", 100.00, 10)
	req := f.createBaseReq(vid, 1)
	req.DiscountTotal = 10000
	res, err := f.orders.CreateOrder(context.Background(), req)
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	if res.Total != 10000 {
		t.Fatalf("无券时总额应为 10000 分，实际 %d", res.Total)
	}
}

func TestOrderCouponAppliesServerSideDiscount(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "马克杯", 100.00, 10)
	c := f.addCoupon(t, &orderdto.CouponSaveReq{
		Code: "save20", DiscountType: "percent", DiscountValue: 20,
	})

	req := f.createBaseReq(vid, 1)
	req.CouponCode = "save20"

	req.DiscountTotal = 9900
	res, err := f.orders.CreateOrder(context.Background(), req)
	if err != nil {
		t.Fatalf("带券建单失败: %v", err)
	}

	if res.Total != 8000 {
		t.Fatalf("总额应为 8000 分（服务端算的折扣），实际 %d", res.Total)
	}

	if n := f.redemptionCount(t, c.ID); n != 1 {
		t.Fatalf("应有 1 条核销记录，实际 %d", n)
	}
	updated, err := f.orders.GetCoupon(context.Background(), c.ID)
	if err != nil {
		t.Fatalf("读优惠码失败: %v", err)
	}
	if updated.UsedCount != 1 {
		t.Fatalf("已用次数应为 1，实际 %d", updated.UsedCount)
	}

	req2 := f.createBaseReq(vid, 1)
	req2.CouponCode = "SAVE20"
	res2, err := f.orders.CreateOrder(context.Background(), req2)
	if err != nil {
		t.Fatalf("大写券码应同样可用: %v", err)
	}
	if res2.Total != 8000 {
		t.Fatalf("大写券码应同样打折，实际 %d", res2.Total)
	}
}

func TestOrderCouponFixedTypeDiscountsExactAmount(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "贴纸", 30.00, 10)

	f.addCoupon(t, &orderdto.CouponSaveReq{Code: "big", DiscountType: "fixed", DiscountValue: 5000})
	req := f.createBaseReq(vid, 1)
	req.CouponCode = "big"
	res, err := f.orders.CreateOrder(context.Background(), req)
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	if res.Total != 0 {
		t.Fatalf("折扣应被夹到小计（3000 分），总额实际 %d", res.Total)
	}
}

func TestOrderCouponRedeemIsIdempotent(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "帽子", 50.00, 10)
	c := f.addCoupon(t, &orderdto.CouponSaveReq{Code: "once", DiscountType: "fixed", DiscountValue: 1000})

	req := f.createBaseReq(vid, 1)
	req.CouponCode = "once"
	req.RequestID = "req-coupon-1"
	first, err := f.orders.CreateOrder(context.Background(), req)
	if err != nil {
		t.Fatalf("首次建单失败: %v", err)
	}
	second, err := f.orders.CreateOrder(context.Background(), req)
	if err != nil {
		t.Fatalf("重复提交应返回既有单而不是报错: %v", err)
	}
	if !second.Duplicated || second.ID != first.ID {
		t.Fatalf("重复提交应命中幂等键，实际 duplicated=%v id=%d/%d", second.Duplicated, second.ID, first.ID)
	}
	if n := f.redemptionCount(t, c.ID); n != 1 {
		t.Fatalf("幂等重放不该重复核销，实际 %d 条", n)
	}
	updated, _ := f.orders.GetCoupon(context.Background(), c.ID)
	if updated.UsedCount != 1 {
		t.Fatalf("已用次数不该被重放加一次，实际 %d", updated.UsedCount)
	}
}

func TestOrderCouponExhaustedRollsBackOrder(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "水壶", 80.00, 10)
	f.addCoupon(t, &orderdto.CouponSaveReq{
		Code: "only1", DiscountType: "fixed", DiscountValue: 500, MaxUses: 1,
	})

	req1 := f.createBaseReq(vid, 1)
	req1.CouponCode = "only1"
	if _, err := f.orders.CreateOrder(context.Background(), req1); err != nil {
		t.Fatalf("第一单应成功: %v", err)
	}
	before := f.orderCount(t)

	req2 := f.createBaseReq(vid, 1)
	req2.CouponCode = "only1"
	_, err := f.orders.CreateOrder(context.Background(), req2)
	if err == nil {
		t.Fatal("券已用尽时必须拒绝建单")
	}
	if !strings.Contains(err.Error(), orderenums.ErrCouponExhausted) {
		t.Fatalf("应给出「已用完」的结论，实际: %v", err)
	}
	if after := f.orderCount(t); after != before {
		t.Fatalf("券不可用时订单不该落库（%d → %d）", before, after)
	}

	if got := f.reservedOf(t, vid); got != 1 {
		t.Fatalf("券用完时不该多预占（应为第一单的 1 件），实际 %d", got)
	}
	if got := f.stockOf(t, vid); got != 10 {
		t.Fatalf("在库数量不该被下单改动，期望 10，实际 %d", got)
	}
}

func TestOrderCouponRespectsMinSubtotal(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "钥匙扣", 20.00, 10)
	f.addCoupon(t, &orderdto.CouponSaveReq{
		Code: "over100", DiscountType: "fixed", DiscountValue: 1000, MinSubtotal: 10000,
	})
	req := f.createBaseReq(vid, 1)
	req.CouponCode = "over100"
	_, err := f.orders.CreateOrder(context.Background(), req)
	if err == nil || !strings.Contains(err.Error(), orderenums.ErrCouponMinSubtotal) {
		t.Fatalf("未达门槛应拒绝并给出具体原因，实际: %v", err)
	}
	if n := f.orderCount(t); n != 0 {
		t.Fatalf("被拒的请求不该留下订单，实际 %d 单", n)
	}
}

func TestOrderCouponRejectsUnknownDisabledAndExpired(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "笔", 10.00, 10)
	ctx := context.Background()

	req := f.createBaseReq(vid, 1)
	req.CouponCode = "nope"
	if _, err := f.orders.CreateOrder(ctx, req); err == nil || !strings.Contains(err.Error(), orderenums.ErrCouponNotFound) {
		t.Fatalf("未知券码应拒绝，实际: %v", err)
	}

	f.orders.CreateCoupon(ctx, &orderdto.CouponSaveReq{
		ProjectID: f.projectID, Code: "off", DiscountType: "fixed", DiscountValue: 100, Status: 0,
	})
	req2 := f.createBaseReq(vid, 1)
	req2.CouponCode = "off"
	if _, err := f.orders.CreateOrder(ctx, req2); err == nil || !strings.Contains(err.Error(), orderenums.ErrCouponDisabled) {
		t.Fatalf("停用的券应拒绝，实际: %v", err)
	}

	f.orders.CreateCoupon(ctx, &orderdto.CouponSaveReq{
		ProjectID: f.projectID, Code: "old", DiscountType: "fixed", DiscountValue: 100,
		Status: 1, StartsAt: "2020-01-01", EndsAt: "2020-12-31",
	})
	req3 := f.createBaseReq(vid, 1)
	req3.CouponCode = "old"
	if _, err := f.orders.CreateOrder(ctx, req3); err == nil || !strings.Contains(err.Error(), orderenums.ErrCouponExpired) {
		t.Fatalf("过期的券应拒绝，实际: %v", err)
	}
}

func TestOrderCouponValidateIsReadOnly(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}

	f.addCoupon(t, &orderdto.CouponSaveReq{
		Code: "preview", DiscountType: "percent", DiscountValue: 10, MinSubtotal: 1000,
	})
	ctx := context.Background()

	res, err := f.orders.ValidateCoupon(ctx, &orderdto.CouponValidateReq{
		ProjectID: f.projectID, Code: "preview", Subtotal: 5000,
	})
	if err != nil {
		t.Fatalf("试算失败: %v", err)
	}
	if !res.Usable || res.DiscountAmount != 500 || res.Total != 4500 {
		t.Fatalf("试算结论不对: %+v", res)
	}

	for i := 0; i < 3; i++ {
		if _, err := f.orders.ValidateCoupon(ctx, &orderdto.CouponValidateReq{
			ProjectID: f.projectID, Code: "preview", Subtotal: 5000,
		}); err != nil {
			t.Fatalf("重复试算失败: %v", err)
		}
	}
	res2, _ := f.orders.ListCoupons(ctx, &orderdto.CouponListReq{ProjectID: f.projectID, Keyword: "preview"})
	if len(res2.List) != 1 || res2.List[0].UsedCount != 0 {
		t.Fatalf("试算不该占用次数: %+v", res2.List)
	}

	short, err := f.orders.ValidateCoupon(ctx, &orderdto.CouponValidateReq{
		ProjectID: f.projectID, Code: "preview", Subtotal: 100,
	})
	if err != nil {
		t.Fatalf("门槛不足不该返回 error: %v", err)
	}
	if short.Usable {
		t.Fatalf("门槛不足时应标记不可用: %+v", short)
	}
}

func TestOrderCouponDeleteRejectedWhenRedeemed(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "鼠标垫", 40.00, 10)
	c := f.addCoupon(t, &orderdto.CouponSaveReq{Code: "used", DiscountType: "fixed", DiscountValue: 500})
	ctx := context.Background()

	req := f.createBaseReq(vid, 1)
	req.CouponCode = "used"
	if _, err := f.orders.CreateOrder(ctx, req); err != nil {
		t.Fatalf("建单失败: %v", err)
	}

	err := f.orders.DeleteCoupon(ctx, c.ID)
	if err == nil || !strings.Contains(err.Error(), orderenums.ErrCouponInUse) {
		t.Fatalf("有核销记录的券应拒绝删除，实际: %v", err)
	}

	upd, err := f.orders.UpdateCoupon(ctx, &orderdto.CouponSaveReq{
		ID: c.ID, Name: "双十一券", DiscountType: "fixed", DiscountValue: 500, Status: 0,
	})
	if err != nil {
		t.Fatalf("停用应成功: %v", err)
	}
	if upd.Status != 0 || upd.StatusLabel != "已停用" {
		t.Fatalf("停用状态不对: %+v", upd)
	}

	if !strings.EqualFold(upd.Code, "used") {
		t.Fatalf("券码不该被修改，实际 %q", upd.Code)
	}
	if upd.Code != "USED" {
		t.Fatalf("券码应以归一化大写形式返回，实际 %q", upd.Code)
	}
}

func TestOrderCouponCreateRejectsInvalidRule(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	base := func() *orderdto.CouponSaveReq {
		return &orderdto.CouponSaveReq{ProjectID: f.projectID, Code: "x", DiscountType: "percent", DiscountValue: 10, Status: 1}
	}
	tests := []struct {
		name string
		mut  func(*orderdto.CouponSaveReq)
		want string
	}{
		{"缺券码", func(r *orderdto.CouponSaveReq) { r.Code = "" }, orderenums.ErrCouponCodeRequired},
		{"未知折扣类型", func(r *orderdto.CouponSaveReq) { r.DiscountType = "half" }, orderenums.ErrCouponTypeInvalid},
		{"百分比超范围", func(r *orderdto.CouponSaveReq) { r.DiscountValue = 150 }, orderenums.ErrCouponValueInvalid},
		{"百分比为 0", func(r *orderdto.CouponSaveReq) { r.DiscountValue = 0 }, orderenums.ErrCouponValueInvalid},
		{"负数门槛", func(r *orderdto.CouponSaveReq) { r.MinSubtotal = -1 }, orderenums.ErrCouponValueInvalid},
		{"时间窗倒置", func(r *orderdto.CouponSaveReq) { r.StartsAt, r.EndsAt = "2026-06-01", "2026-05-01" }, orderenums.ErrCouponWindowInvalid},
		{"时间格式非法", func(r *orderdto.CouponSaveReq) { r.EndsAt = "下个月" }, orderenums.ErrCouponWindowInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := base()
			tt.mut(req)
			_, err := f.orders.CreateCoupon(ctx, req)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("应给出 %q，实际: %v", tt.want, err)
			}
		})
	}

	if _, err := f.orders.CreateCoupon(ctx, base()); err != nil {
		t.Fatalf("首次建券应成功: %v", err)
	}
	if _, err := f.orders.CreateCoupon(ctx, base()); err == nil || !strings.Contains(err.Error(), orderenums.ErrCouponCodeTaken) {
		t.Fatalf("重复券码应拒绝，实际: %v", err)
	}
}

func TestOrderCancelReleasesCoupon(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "用券可取消", 100.00, 10)
	c := f.addCoupon(t, &orderdto.CouponSaveReq{Code: "cancel10", DiscountType: "fixed", DiscountValue: 1000})
	ctx := context.Background()

	req := f.createBaseReq(vid, 2)
	req.CouponCode = "CANCEL10"
	res, err := f.orders.CreateOrder(ctx, req)
	if err != nil {
		t.Fatalf("带券建单失败: %v", err)
	}
	if res.Total != 19000 {
		t.Fatalf("带券总额应为 19000 分，实际 %d", res.Total)
	}

	if got := f.couponUsedCount(t, c.ID); got != 1 {
		t.Fatalf("建单后 used_count 应为 1，实际 %d", got)
	}
	if n := f.redemptionCount(t, c.ID); n != 1 {
		t.Fatalf("建单后应有 1 条核销明细，实际 %d", n)
	}

	if got := f.reservedOf(t, vid); got != 2 {
		t.Fatalf("建单后预占应为 2，实际 %d", got)
	}

	if _, err := f.orders.CancelOrder(ctx, &orderdto.CancelOrderReq{
		OrderID: res.ID, Reason: "买家改主意", OperatorName: "tester",
	}); err != nil {
		t.Fatalf("取消失败: %v", err)
	}

	if got := f.couponUsedCount(t, c.ID); got != 0 {
		t.Fatalf("取消后 used_count 应回退为 0，实际 %d", got)
	}
	if n := f.redemptionCount(t, c.ID); n != 0 {
		t.Fatalf("取消后核销明细应清空，实际 %d 条", n)
	}

	detail, err := f.orders.GetOrder(ctx, &orderdto.GetOrderReq{ProjectID: f.projectID, OrderID: res.ID})
	if err != nil {
		t.Fatalf("读订单详情失败: %v", err)
	}
	if detail.Head == nil || detail.Head.Status != ordermodel.OrderStatusCancelled {
		t.Fatalf("取消后状态应为 cancelled，实际 %+v", detail.Head)
	}

	if got := f.stockOf(t, vid); got != 10 {
		t.Fatalf("取消后库存应归还到 10，实际 %d", got)
	}

	if _, err := f.orders.CancelOrder(ctx, &orderdto.CancelOrderReq{
		OrderID: res.ID, Reason: "再来一次",
	}); err == nil {
		t.Fatalf("重复取消应被拒绝")
	}
	if got := f.couponUsedCount(t, c.ID); got != 0 {
		t.Fatalf("重复取消不应再动券计数，实际 %d", got)
	}

	req2 := f.createBaseReq(vid, 1)
	req2.CouponCode = "cancel10"
	if _, err := f.orders.CreateOrder(ctx, req2); err != nil {
		t.Fatalf("取消回退后券应可再次使用: %v", err)
	}
	if got := f.couponUsedCount(t, c.ID); got != 1 {
		t.Fatalf("再次使用后 used_count 应为 1，实际 %d", got)
	}
	if n := f.redemptionCount(t, c.ID); n != 1 {
		t.Fatalf("再次使用后应有 1 条核销明细，实际 %d", n)
	}
}

func (f *orderFixture) couponUsedCount(t *testing.T, couponID uint64) int {
	t.Helper()
	res, err := f.orders.GetCoupon(context.Background(), couponID)
	if err != nil {
		t.Fatalf("读优惠码失败: %v", err)
	}
	return res.UsedCount
}
