package feature

import (
	"context"
	"testing"

	membershipcontract "go_wp/internal/module/membership/contract"
	membershipdto "go_wp/internal/module/membership/dto"
	orderdto "go_wp/internal/module/order/dto"
)

type stubShippingMembershipFeature struct {
	free  bool
	asked []uint64
}

func (s *stubShippingMembershipFeature) Resolve(_ context.Context, req *membershipdto.ResolveReq) (*membershipdto.MembershipResp, error) {
	s.asked = append(s.asked, req.UserID)
	return &membershipdto.MembershipResp{
		UserID: req.UserID, ProjectID: req.ProjectID, TierID: 2, TierName: "黄金会员",
		FreeShipping: s.free,
	}, nil
}

var _ membershipcontract.Reader = (*stubShippingMembershipFeature)(nil)

func (f *cartFixture) shippingTotalOfOrder(t *testing.T, orderID uint64) int64 {
	t.Helper()
	detail, err := f.orders.GetOrder(context.Background(), &orderdto.GetOrderReq{
		ProjectID: f.projectID, OrderID: orderID,
	})
	if err != nil {
		t.Fatalf("读订单失败: %v", err)
	}
	return detail.Head.ShippingTotal
}

func (f *cartFixture) subtotalOfOrder(t *testing.T, orderID uint64) int64 {
	t.Helper()
	detail, err := f.orders.GetOrder(context.Background(), &orderdto.GetOrderReq{
		ProjectID: f.projectID, OrderID: orderID,
	})
	if err != nil {
		t.Fatalf("读订单失败: %v", err)
	}
	return detail.Head.Subtotal
}

func (f *cartFixture) orderUserID(t *testing.T, orderID uint64) uint64 {
	t.Helper()
	detail, err := f.orders.GetOrder(context.Background(), &orderdto.GetOrderReq{
		ProjectID: f.projectID, OrderID: orderID,
	})
	if err != nil || detail.Head.UserID == nil {
		t.Fatalf("读订单 user_id 失败: %v", err)
	}
	return *detail.Head.UserID
}

func TestCartCheckoutFreeShippingChangesOrderShipping(t *testing.T) {
	f := newCartFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, variantID := f.addProduct(t, "免运费商品", 100, 20)

	f.setShippingPolicy(t, 800, 0)

	member := &stubShippingMembershipFeature{free: true}
	f.cart.SetMembershipReader(member)

	guestSnap := f.addToCart(t, variantID, 1, "")
	guestReq := f.checkoutReq(guestSnap.Cookie)
	guestReq.RequestID = "req-ship-guest"
	guestRes, err := f.cart.Checkout(ctx, guestReq)
	if err != nil {
		t.Fatalf("访客结算失败: %v", err)
	}
	if got := f.shippingTotalOfOrder(t, guestRes.OrderID); got != 800 {
		t.Fatalf("非会员应照收 800 分运费，实际 %d", got)
	}
	if len(member.asked) != 0 {
		t.Fatalf("未登录访客不该被拿去问会员身份，实际问了 %d 次", len(member.asked))
	}

	memberID := f.orderUserID(t, guestRes.OrderID)

	memberSnap := f.addToCart(t, variantID, 1, "")
	memberReq := f.checkoutReq(memberSnap.Cookie)
	memberReq.RequestID = "req-ship-member"
	memberReq.UserID = &memberID
	memberRes, err := f.cart.Checkout(ctx, memberReq)
	if err != nil {
		t.Fatalf("会员结算失败: %v", err)
	}
	if got := f.shippingTotalOfOrder(t, memberRes.OrderID); got != 0 {
		t.Fatalf("会员免运费后运费应为 0，实际 %d", got)
	}
	if len(member.asked) != 1 || member.asked[0] != memberID {
		t.Fatalf("会员身份应按结算身份解析一次（id=%d），实际 %v", memberID, member.asked)
	}

	if got := f.subtotalOfOrder(t, memberRes.OrderID); got != 10000 {
		t.Fatalf("商品小计不应被会员权益改动，实际 %d", got)
	}
	detail, err := f.orders.GetOrder(ctx, &orderdto.GetOrderReq{ProjectID: f.projectID, OrderID: memberRes.OrderID})
	if err != nil {
		t.Fatalf("读订单失败: %v", err)
	}
	if detail.Head.Total != 10000 {
		t.Fatalf("免运费后总额应等于小计 10000，实际 %d", detail.Head.Total)
	}
}

func TestCartCheckoutThresholdFreeShipping(t *testing.T) {
	f := newCartFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, variantID := f.addProduct(t, "满额免运费商品", 100, 20)

	f.setShippingPolicy(t, 800, 10000)
	snap := f.addToCart(t, variantID, 1, "")
	req := f.checkoutReq(snap.Cookie)
	req.RequestID = "req-threshold-hit"
	res, err := f.cart.Checkout(ctx, req)
	if err != nil {
		t.Fatalf("结算失败: %v", err)
	}
	if got := f.shippingTotalOfOrder(t, res.OrderID); got != 0 {
		t.Fatalf("小计 10000 达门槛 10000 应免运费，实际 %d", got)
	}

	f.setShippingPolicy(t, 800, 20000)
	snap = f.addToCart(t, variantID, 1, "")
	req = f.checkoutReq(snap.Cookie)
	req.RequestID = "req-threshold-miss"
	res, err = f.cart.Checkout(ctx, req)
	if err != nil {
		t.Fatalf("结算失败: %v", err)
	}
	if got := f.shippingTotalOfOrder(t, res.OrderID); got != 800 {
		t.Fatalf("小计 10000 未达门槛 20000 应照收 800，实际 %d", got)
	}
}

func TestCartCheckoutWithoutShippingPolicy(t *testing.T) {
	f := newCartFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, variantID := f.addProduct(t, "无运费商品", 100, 20)
	f.setShippingPolicy(t, 0, 0)

	snap := f.addToCart(t, variantID, 1, "")
	req := f.checkoutReq(snap.Cookie)
	req.RequestID = "req-no-shipping"
	res, err := f.cart.Checkout(ctx, req)
	if err != nil {
		t.Fatalf("结算失败: %v", err)
	}
	if got := f.shippingTotalOfOrder(t, res.OrderID); got != 0 {
		t.Fatalf("未配置运费时应为 0，实际 %d", got)
	}
	if got := f.subtotalOfOrder(t, res.OrderID); got != 10000 {
		t.Fatalf("商品小计应为 10000，实际 %d", got)
	}
}
