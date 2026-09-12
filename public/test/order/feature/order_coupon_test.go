package feature

// order_coupon_test.go — 优惠码的用例链路测试（BIZ-1）。
//
// 覆盖的是**不变量**而不是「方法能跑通」：
//   · 折扣由服务端算，客户端传进来的金额一律忽略（能定价的接口等于把收银台交出去）；
//   · 核销与建单同生共死：券用尽 → 订单也不该留下（不能出现「券用完了但单还在」）；
//   · 幂等：同一 requestId 重复提交既不重复扣库存，也不重复记核销；
//   · 试算是纯读：不占次数、不落核销；
//   · 有核销记录的券不许删（删了那些记录就指向一张查不到的券）。

import (
	"context"
	"strings"
	"testing"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
)

// addCoupon 建一张券（工程 id 由夹具补上）。
//
// Status 必须显式传 1：零值是「停用」—— 这是刻意的（停用是危险方向，
// 默认值不该让一张券悄悄生效），但调用方忘了传就会得到一张建好即停用的券。
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

// redemptionCount 该券的核销记录条数。
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

// orderCount 工程内订单总数（验证「拒绝时订单不该留下」）。
func (f *orderFixture) orderCount(t *testing.T) int64 {
	t.Helper()
	res, err := f.orders.ListOrders(context.Background(), &orderdto.ListOrderReq{ProjectID: f.projectID})
	if err != nil {
		t.Fatalf("读订单列表失败: %v", err)
	}
	return res.Total
}

// TestOrderCouponAppliesServerSideDiscount 带券建单：折扣按服务端口径算，客户端的金额被忽略。
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
	// 客户端乱传一个更低的折扣：必须被忽略（服务端试算说了算）。
	req.DiscountTotal = 9900
	res, err := f.orders.CreateOrder(context.Background(), req)
	if err != nil {
		t.Fatalf("带券建单失败: %v", err)
	}
	// 10000 分 - 20% = 8000 分
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

	// 券码大小写不敏感（归一化只发生在服务层一处，客户端写小写也认）。
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

// TestOrderCouponFixedTypeDiscountsExactAmount 固定金额券：直接减，且不会把总额减成负数。
func TestOrderCouponFixedTypeDiscountsExactAmount(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "贴纸", 30.00, 10)
	// 券面额大于小计：折扣必须被夹到小计（负数总额没有意义）。
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

// TestOrderCouponRedeemIsIdempotent 同一幂等键重复提交：不重复扣库存、不重复核销。
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

// TestOrderCouponExhaustedRollsBackOrder 用尽后整体拒绝：**订单也不该留下**。
//
// 这是核销与建单同事务的核心价值：允许「先核销后建单」的实现会留下
// 「券被扣了一次但单没下成」，而那种状态只能靠人工对账发现。
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
	// 库存也不该被扣（建单事务回滚在建单之前，扣库存在事务之后 ——
	// 券判定失败发生在写订单之前，所以这里连单都没建）。
	if got := f.stockOf(t, vid); got != 9 {
		t.Fatalf("库存应只被第一单扣一次（9），实际 %d", got)
	}
}

// TestOrderCouponRespectsMinSubtotal 门槛：小计不够时拒绝，且不落库。
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

// TestOrderCouponRejectsUnknownDisabledAndExpired 券不可用的三种典型情形。
func TestOrderCouponRejectsUnknownDisabledAndExpired(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "笔", 10.00, 10)
	ctx := context.Background()

	// 不存在的券码：不能静默按无折扣建单 —— 客户会以为券用上了。
	req := f.createBaseReq(vid, 1)
	req.CouponCode = "nope"
	if _, err := f.orders.CreateOrder(ctx, req); err == nil || !strings.Contains(err.Error(), orderenums.ErrCouponNotFound) {
		t.Fatalf("未知券码应拒绝，实际: %v", err)
	}

	// 停用的券：Status 零值即停用（默认值是危险方向）。
	f.orders.CreateCoupon(ctx, &orderdto.CouponSaveReq{
		ProjectID: f.projectID, Code: "off", DiscountType: "fixed", DiscountValue: 100, Status: 0,
	})
	req2 := f.createBaseReq(vid, 1)
	req2.CouponCode = "off"
	if _, err := f.orders.CreateOrder(ctx, req2); err == nil || !strings.Contains(err.Error(), orderenums.ErrCouponDisabled) {
		t.Fatalf("停用的券应拒绝，实际: %v", err)
	}

	// 已过期的券：时间窗是算出来的，不是存下来的状态列。
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

// TestOrderCouponValidateIsReadOnly 试算是纯读：不占次数、不落核销。
func TestOrderCouponValidateIsReadOnly(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	// 带门槛：这样才能同时验「够门槛可用」与「不够门槛不可用」两种结论。
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

	// 反复试算不应改变任何东西。
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

	// 门槛不足：不报错，只给「不可用 + 原因」（试算是给页面用的，不该变成异常）。
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

// TestOrderCouponDeleteRejectedWhenRedeemed 有核销记录的券不能删，但可以停用。
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

	// 停用是允许的（停用不删：历史核销记录还要读它）。
	upd, err := f.orders.UpdateCoupon(ctx, &orderdto.CouponSaveReq{
		ID: c.ID, Name: "双十一券", DiscountType: "fixed", DiscountValue: 500, Status: 0,
	})
	if err != nil {
		t.Fatalf("停用应成功: %v", err)
	}
	if upd.Status != 0 || upd.StatusLabel != "已停用" {
		t.Fatalf("停用状态不对: %+v", upd)
	}
	// 券码不可改：改码等于换一张券，历史核销记录会指向一个查不到的码。
	// 返回的是**归一化后的大写**：大小写不敏感只发生在服务层一处，
	// 返回归一化形式让调用方不必自己再转一次。
	if !strings.EqualFold(upd.Code, "used") {
		t.Fatalf("券码不该被修改，实际 %q", upd.Code)
	}
	if upd.Code != "USED" {
		t.Fatalf("券码应以归一化大写形式返回，实际 %q", upd.Code)
	}
}

// TestOrderCouponCreateRejectsInvalidRule 建券的规则校验：逐条拒绝而不是静默纠正。
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

	// 券码工程内唯一。
	if _, err := f.orders.CreateCoupon(ctx, base()); err != nil {
		t.Fatalf("首次建券应成功: %v", err)
	}
	if _, err := f.orders.CreateCoupon(ctx, base()); err == nil || !strings.Contains(err.Error(), orderenums.ErrCouponCodeTaken) {
		t.Fatalf("重复券码应拒绝，实际: %v", err)
	}
}
