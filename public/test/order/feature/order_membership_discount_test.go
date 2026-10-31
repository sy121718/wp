package feature

import (
	"context"
	"testing"

	membershipcontract "go_wp/internal/module/membership/contract"
	membershipdto "go_wp/internal/module/membership/dto"
	orderdto "go_wp/internal/module/order/dto"
)

type stubOrderMembership struct {
	percent int64
	asked   []uint64
}

func (s *stubOrderMembership) Resolve(_ context.Context, req *membershipdto.ResolveReq) (*membershipdto.MembershipResp, error) {
	s.asked = append(s.asked, req.UserID)
	return &membershipdto.MembershipResp{
		UserID: req.UserID, ProjectID: req.ProjectID, TierID: 2, TierName: "黄金会员",
		FreeShipping: true, DiscountPercent: s.percent,
	}, nil
}

var _ membershipcontract.Reader = (*stubOrderMembership)(nil)

func TestOrderMembershipDiscountRecordedSeparately(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "会员折扣商品", 100, 10)
	f.addCoupon(t, &orderdto.CouponSaveReq{Code: "member20", DiscountType: "percent", DiscountValue: 20})

	member := &stubOrderMembership{percent: 10}
	f.orders.SetMembershipReader(member)

	req := f.createBaseReq(vid, 1)
	req.CouponCode = "member20"
	req.ShippingTotal = 800
	res, err := f.orders.CreateOrder(context.Background(), req)
	if err != nil {
		t.Fatalf("带券 + 会员折扣建单失败: %v", err)
	}
	if res.Total != 7800 {
		t.Fatalf("总额应为 10000−2000−1000+800=7800 分，实际 %d", res.Total)
	}

	var row struct {
		Subtotal                int64 `gorm:"column:subtotal"`
		DiscountTotal           int64 `gorm:"column:discount_total"`
		MembershipDiscountTotal int64 `gorm:"column:membership_discount_total"`
		ShippingTotal           int64 `gorm:"column:shipping_total"`
		Total                   int64 `gorm:"column:total"`
	}
	if err := f.db.Table("orders").Where("id = ?", res.ID).Scan(&row).Error; err != nil {
		t.Fatalf("读订单头失败: %v", err)
	}
	if row.DiscountTotal != 2000 {
		t.Fatalf("券折扣应记 2000，实际 %d", row.DiscountTotal)
	}
	if row.MembershipDiscountTotal != 1000 {
		t.Fatalf("会员折扣应记 1000，实际 %d", row.MembershipDiscountTotal)
	}
	if row.ShippingTotal != 800 {
		t.Fatalf("运费应记 800，实际 %d", row.ShippingTotal)
	}
	if row.Total != row.Subtotal-row.DiscountTotal-row.MembershipDiscountTotal+row.ShippingTotal {
		t.Fatalf("三列与总额不平衡：%+v", row)
	}

	var line struct {
		LineSubtotal int64 `gorm:"column:line_subtotal"`
		LineDiscount int64 `gorm:"column:line_discount"`
		LineTotal    int64 `gorm:"column:line_total"`
	}
	if err := f.db.Table("order_items").Where("order_id = ?", res.ID).Scan(&line).Error; err != nil {
		t.Fatalf("读订单项失败: %v", err)
	}
	if line.LineDiscount != 3000 {
		t.Fatalf("行折扣应为券 + 会员折扣合计 3000，实际 %d", line.LineDiscount)
	}
	if line.LineTotal != 7000 {
		t.Fatalf("行实付应为 7000，实际 %d", line.LineTotal)
	}

	if len(member.asked) != 1 {
		t.Fatalf("会员身份应被解析一次，实际 %d 次", len(member.asked))
	}
	if member.asked[0] == 0 {
		t.Fatal("会员折扣在开号之前算：解析到的访客 id 是 0，新客户的折扣永远不生效")
	}
}

func TestOrderWithoutMembershipPortKeepsDiscountZero(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "无会员商品", 100, 10)

	res, err := f.orders.CreateOrder(context.Background(), f.createBaseReq(vid, 1))
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	if res.Total != 10000 {
		t.Fatalf("无券无会员折扣时总额应为小计 10000，实际 %d", res.Total)
	}

	var row struct {
		DiscountTotal           int64 `gorm:"column:discount_total"`
		MembershipDiscountTotal int64 `gorm:"column:membership_discount_total"`
	}
	if err := f.db.Table("orders").Where("id = ?", res.ID).Scan(&row).Error; err != nil {
		t.Fatalf("读订单头失败: %v", err)
	}
	if row.DiscountTotal != 0 || row.MembershipDiscountTotal != 0 {
		t.Fatalf("无券无会员时应两列都为 0，实际 %+v", row)
	}
}
