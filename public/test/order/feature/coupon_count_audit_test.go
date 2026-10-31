package feature

import (
	"context"
	"testing"

	orderdto "go_wp/internal/module/order/dto"
)

func TestCouponCountAuditReportsDrift(t *testing.T) {
	f := newOrderFixture(t)
	ctx := context.Background()
	coupon := f.addCoupon(t, &orderdto.CouponSaveReq{
		Code: "AUDIT10", Name: "对账券", DiscountType: "fixed", DiscountValue: 100,
		MaxUses: 100, Status: 1,
	})
	_, variantID := f.addProduct(t, "对账商品", 1000, 10)
	req := f.createBaseReq(variantID, 1)
	req.CouponCode = "AUDIT10"
	if _, err := f.orders.CreateOrder(ctx, req); err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	if got := f.redemptionCount(t, coupon.ID); got != 1 {
		t.Fatalf("核销记录应为 1 条，实际 %d", got)
	}

	res, err := f.orders.AuditCouponCounts(ctx, &orderdto.CouponCountAuditReq{ProjectID: f.projectID})
	if err != nil {
		t.Fatalf("对账失败: %v", err)
	}
	if res.Mismatched != 0 {
		t.Fatalf("正常核销后不该有差异，实际 %+v", res.Items)
	}
	if res.Checked < 1 {
		t.Fatalf("对账分母应包含这张券，实际 %d", res.Checked)
	}

	if err := f.db.Exec("UPDATE coupons SET used_count = used_count + 3 WHERE id = ?", coupon.ID).Error; err != nil {
		t.Fatalf("制造偏差失败: %v", err)
	}
	res, err = f.orders.AuditCouponCounts(ctx, &orderdto.CouponCountAuditReq{ProjectID: f.projectID})
	if err != nil {
		t.Fatalf("对账失败: %v", err)
	}
	if res.Mismatched != 1 || len(res.Items) != 1 {
		t.Fatalf("应报出 1 张不一致的券，实际 %+v", res.Items)
	}
	item := res.Items[0]
	if item.CouponID != coupon.ID || item.UsedCount != 4 || item.ActualCount != 1 || item.Diff != 3 {
		t.Fatalf("差异明细不正确: %+v", item)
	}

	var usedCount int64
	if err := f.db.Raw("SELECT used_count FROM coupons WHERE id = ?", coupon.ID).Scan(&usedCount).Error; err != nil {
		t.Fatalf("读计数失败: %v", err)
	}
	if usedCount != 4 {
		t.Fatalf("对账不应改动数据，实际 used_count=%d", usedCount)
	}
}

func TestCouponCountAuditFindsCounterWithoutRedemption(t *testing.T) {
	f := newOrderFixture(t)
	ctx := context.Background()
	coupon := f.addCoupon(t, &orderdto.CouponSaveReq{
		Code: "NODETAIL", Name: "无明细券", DiscountType: "fixed", DiscountValue: 100,
		MaxUses: 5, Status: 1,
	})
	if err := f.db.Exec("UPDATE coupons SET used_count = 2 WHERE id = ?", coupon.ID).Error; err != nil {
		t.Fatalf("制造偏差失败: %v", err)
	}
	res, err := f.orders.AuditCouponCounts(ctx, &orderdto.CouponCountAuditReq{ProjectID: f.projectID})
	if err != nil {
		t.Fatalf("对账失败: %v", err)
	}
	found := false
	for _, item := range res.Items {
		if item.CouponID == coupon.ID {
			found = true
			if item.ActualCount != 0 || item.Diff != 2 {
				t.Fatalf("无明细的偏差应报实际 0 / 差异 2，实际 %+v", item)
			}
		}
	}
	if !found {
		t.Fatalf("没有核销记录的偏差券也应被报出，实际 %+v", res.Items)
	}
}
