package feature

// cart_shipping_membership_test.go — 站点级运费在**真实结算链路**里改变订单运费
// （站点基础运费 → 满额免运费门槛 → 会员免运费）。
//
// 这条测试覆盖单测覆盖不到的那一环：`cart.Checkout` 自己读站点设置（经 project 的
// ShippingPolicyReader）并把结果传给订单域。单测能证明 shippingTotalOf 算得对，
// 但「算得对却没传下去」「端口没接上」「设置键名对不上」同样会让运费不对 ——
// 而这三种失效都不报错、不打日志。
//
// 复算口径：商品 100 元（10000 分）；站点设置的基础运费与门槛按用例显式配置（分）。
// 商品金额一分不动（只影响运费）。

import (
	"context"
	"testing"

	membershipcontract "go_wp/internal/module/membership/contract"
	membershipdto "go_wp/internal/module/membership/dto"
	orderdto "go_wp/internal/module/order/dto"
)

// stubShippingMembershipFeature 免运费权益的替身（收窄契约只有一条只读方法）。
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

// shippingTotalOfOrder 读订单头的运费（分）。
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

// subtotalOfOrder 读订单头的商品小计（分）—— 用于断言「只影响运费」。
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

// orderUserID 读订单头的 user_id（访客结算会自动开号，所以它必然非空）。
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

// TestCartCheckoutFreeShippingChangesOrderShipping 会员免运费生效前后订单运费的变化。
//
// 站点配置：基础运费 8 元（800 分）、**不启用**满额免运费门槛。
// 同一笔车、同一个站点配置，唯一的差别是结算身份（未登录访客 → 已登录会员）：
//
//	· 未登录：会员端口不会被问，基础运费 800 照收；
//	· 已登录会员：free_shipping 权益生效，运费归零。
func TestCartCheckoutFreeShippingChangesOrderShipping(t *testing.T) {
	f := newCartFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, variantID := f.addProduct(t, "免运费商品", 100, 20)
	// 运费来自**站点设置**（不是请求字段）：门槛 0 = 不启用满额免运费，
	// 于是这一条只考察会员权益这一环。
	f.setShippingPolicy(t, 800, 0)

	member := &stubShippingMembershipFeature{free: true}
	f.cart.SetMembershipReader(member)

	// ① 未登录访客（UserID 为空）：照收站点基础运费。
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

	// 访客结算会自动开号：拿那个账号当第二个用例的登录身份（真实链路里就是这么发生的）。
	memberID := f.orderUserID(t, guestRes.OrderID)

	// ② 已登录会员：同一笔基础运费被免掉。
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

	// 只影响运费：商品金额不动（100 元 × 1 = 10000 分，两单一致）。
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

// TestCartCheckoutThresholdFreeShipping 满额免运费门槛在真实链路里生效（非会员也免）。
//
// 同一笔车（10000 分）、同一个访客身份，唯一的变量是**站点设置里的门槛**：
//
//	· 门槛 10000 分 → 刚好达标 → 运费 0；
//	· 门槛 20000 分 → 不达标 → 收基础运费 800。
//
// 这一对正反用例同时钉住了「门槛 > 0 才启用」「达标才免」两件事 —— 只测达标的那一侧时，
// 一个「永远免运费」的实现（把门槛写反/写成 >= 0）也会全绿。
func TestCartCheckoutThresholdFreeShipping(t *testing.T) {
	f := newCartFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, variantID := f.addProduct(t, "满额免运费商品", 100, 20)

	// ① 门槛刚好等于小计：达标 → 免运费（**访客**，与会员权益无关）。
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

	// ② 门槛高于小计：不达标 → 照收基础运费。
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

// TestCartCheckoutWithoutShippingPolicy 站点未配置运费（默认 settings）：运费恒 0。
//
// 这是站点策略接入之前的既有行为，也是「端口/配置缺失」时的降级形态：
// 一份空设置不该让结算多收一笔钱，也不该让结算失败。
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
