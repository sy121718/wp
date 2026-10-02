// cart_coupon_checkout_test.go — 访客结算链路的优惠码入口（BIZ-10）。
//
// 缺陷：券的试算、核销、限次、并发防超发**全都实现好了**，但入口只开在后台代客建单页
// 与 /api/order/create —— `CartCheckoutReq` 没有券字段、片段不解析 `couponCode`，
// 于是 `resolveCoupon` 恒返回 (nil, 0)：前台访客拿着一张券无处可用。
//
// 判据：
//  1. 券码进结算请求 → 透传到建单 → 服务端试算折扣（金额真的降下来）；
//  2. 核销落库（used_count +1），与订单同一个事务；
//  3. 券不可用时**原因可区分**（不是笼统的「优惠码无效」），且订单不落库。
package feature

import (
	"context"
	"strings"
	"testing"
	"time"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
)

// seedCoupon 建一张可用的券（本工程、启用、无门槛、不限次）。
func seedCoupon(t *testing.T, f *cartFixture, code, discountType string, value int64) *ordermodel.CouponModel {
	t.Helper()
	m := ordermodel.NewCouponModel(f.db)
	now := time.Now()
	if err := m.Create(context.Background(), &ordermodel.CouponEntity{
		ProjectID: f.projectID, Code: code, Name: "测试券",
		DiscountType: discountType, DiscountValue: value,
		Status: 1, CreateTime: now, UpdateTime: now,
	}); err != nil {
		t.Fatalf("建券失败: %v", err)
	}
	return m
}

// TestCartCheckoutAppliesCoupon 券码从结算请求一路走到核销。
func TestCartCheckoutAppliesCoupon(t *testing.T) {
	f := newCartFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	coupons := seedCoupon(t, f, "SAVE10", ordermodel.CouponTypePercent, 10)

	_, variantID := f.addProduct(t, "券商品", 30, 10)
	snap := f.addToCart(t, variantID, 2, "") // 30 元 × 2 = 6000 分

	req := f.checkoutReq(snap.Cookie)
	req.CouponCode = "save10" // 小写：券码归一化（去空白转大写）由 order 侧负责
	res, err := f.cart.Checkout(ctx, req)
	if err != nil {
		t.Fatalf("带券结算失败: %v", err)
	}
	if !res.Paid {
		t.Fatalf("应支付成功，实际 Paid=false（%s）", res.PaymentError)
	}

	detail, err := f.orders.GetOrder(ctx, &orderdto.GetOrderReq{ProjectID: f.projectID, OrderID: res.OrderID})
	if err != nil {
		t.Fatalf("读订单失败: %v", err)
	}
	// 6000 分九折 → 折扣 600、实付 5400。金额一律整数分。
	if detail.Head.DiscountTotal != 600 {
		t.Fatalf("券折扣应为 600 分，实际 %d（券没透传到建单？）", detail.Head.DiscountTotal)
	}
	if detail.Head.Total != 5400 {
		t.Fatalf("券后应付 5400 分，实际 %d", detail.Head.Total)
	}

	got, err := coupons.GetByCode(ctx, f.projectID, "SAVE10")
	if err != nil {
		t.Fatalf("读券失败: %v", err)
	}
	if got.UsedCount != 1 {
		t.Fatalf("券应被核销一次，实际 used_count=%d", got.UsedCount)
	}
}

// TestCartCheckoutRejectsUnknownCoupon 未知券码：结算被拒、原因可区分、订单不落库。
func TestCartCheckoutRejectsUnknownCoupon(t *testing.T) {
	f := newCartFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, variantID := f.addProduct(t, "无券商品", 30, 10)
	snap := f.addToCart(t, variantID, 1, "")

	req := f.checkoutReq(snap.Cookie)
	req.CouponCode = "NOSUCHCODE"
	_, err := f.cart.Checkout(ctx, req)
	if err == nil {
		t.Fatal("未知券码应被拒绝")
	}
	if !strings.Contains(err.Error(), orderenums.ErrCouponNotFound) {
		t.Fatalf("原因应可区分（券不存在 = %s），实际 %v", orderenums.ErrCouponNotFound, err)
	}

	// 券在试算阶段就失败 → 整单不建：不能出现「建了单却没折扣」的半截状态。
	var count int64
	if cerr := f.db.Table("orders").Where("project_id = ?", f.projectID).Count(&count).Error; cerr != nil {
		t.Fatalf("统计订单失败: %v", cerr)
	}
	if count != 0 {
		t.Fatalf("券无效时不该落单，实际 %d 张订单", count)
	}
}
