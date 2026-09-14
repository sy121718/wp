package feature

// cart_checkout_test.go — 访客购物车与结算的链路测试。
//
// 覆盖的是**不变量**而不是「方法能跑通」：
//   · 结算必须落成一张已付款的订单，且订单项是商品事实的快照；
//   · 归因 cookie 必须在下单那一刻定格进 orders.attribution（含 first-touch 与轨迹）；
//   · 幂等键命中不得第二次扣库存；
//   · 篡改过的购物车 cookie 一律当空车（签名是唯一的信任来源）；
//   · 支付失败**不能把订单丢掉** —— 单号必须回到访客手里。

import (
	"context"
	"encoding/json"
	"net/url"
	"testing"

	cartdto "go_wp/internal/module/cart/dto"
	cartenums "go_wp/internal/module/cart/enums"
	orderdto "go_wp/internal/module/order/dto"
	ordermodel "go_wp/internal/module/order/model"
)

// TestCartCheckoutCreatesPaidOrderAndDeductsStock 加购 → 结算 → 已付款订单 + 库存扣减 + 清空购物车。
func TestCartCheckoutCreatesPaidOrderAndDeductsStock(t *testing.T) {
	f := newCartFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, variantID := f.addProduct(t, "购物车商品", 30, 10)

	snap := f.addToCart(t, variantID, 2, "")
	if snap.Cookie == "" {
		t.Fatal("加购必须返回新的购物车 cookie —— 它是购物车唯一的状态载体")
	}
	if snap.ItemCount != 2 {
		t.Fatalf("件数应为 2，实际 %d", snap.ItemCount)
	}
	// 30 元 × 2 = 6000 分。金额一律整数分，绝不出现浮点。
	if snap.Total != 6000 {
		t.Fatalf("小计应为 6000 分，实际 %d", snap.Total)
	}

	res, err := f.cart.Checkout(ctx, f.checkoutReq(snap.Cookie))
	if err != nil {
		t.Fatalf("结算失败: %v", err)
	}
	if !res.Paid {
		t.Fatalf("模拟通道应当直接支付成功，实际 Paid=false（%s）", res.PaymentError)
	}
	if res.OrderNo == "" || res.OrderID == 0 {
		t.Fatalf("结算结果必须带单号与订单 id: %+v", res)
	}

	detail, err := f.orders.GetOrder(ctx, &orderdto.GetOrderReq{ProjectID: f.projectID, OrderID: res.OrderID})
	if err != nil {
		t.Fatalf("读订单失败: %v", err)
	}
	if detail.Head.Status != ordermodel.OrderStatusPaid {
		t.Fatalf("订单状态应为 paid，实际 %s", detail.Head.Status)
	}
	if detail.Head.PaidAt == nil {
		t.Fatal("已付款订单必须有 paid_at（对账与时效统计都靠它）")
	}
	if detail.Head.PaymentMethod != "paypal" {
		t.Fatalf("支付方式应落 paypal，实际 %q", detail.Head.PaymentMethod)
	}
	if detail.Head.TransactionID == "" {
		t.Fatal("支付流水号必须落库，否则退款时无法在通道侧定位这笔钱")
	}
	if detail.Head.UserID == nil {
		t.Fatal("访客结算应当自动开号并关联订单，否则客户无处查自己的订单")
	}

	// 订单项是快照：单价来自服务端商品事实，不是请求里的值。
	if len(detail.Items) != 1 {
		t.Fatalf("订单项应为 1 行，实际 %d", len(detail.Items))
	}
	if detail.Items[0].UnitPrice != 3000 || detail.Items[0].Quantity != 2 {
		t.Fatalf("订单项快照不对: %+v", detail.Items[0])
	}

	if got := f.stockOf(t, variantID); got != 8 {
		t.Fatalf("库存应扣 2 件（10 → 8），实际 %d", got)
	}

	// 结算返回的 cookie 必须是空车：东西已经变成订单，留着只会让人再点一次结账。
	if res.Cookie == "" {
		t.Fatal("结算成功后必须返回可回写的空购物车 cookie")
	}
	after, err := f.cart.View(ctx, &cartdto.CartViewReq{ProjectID: f.projectID, Cookie: res.Cookie})
	if err != nil {
		t.Fatalf("读结算后的购物车失败: %v", err)
	}
	if !after.Empty {
		t.Fatalf("结算后购物车应为空，实际 %d 行", after.LineCount)
	}
}

// TestCartCheckoutPersistsAttributionFromCookies 归因 cookie 在下单那一刻定格进订单。
func TestCartCheckoutPersistsAttributionFromCookies(t *testing.T) {
	f := newCartFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, variantID := f.addProduct(t, "归因商品", 15, 10)
	snap := f.addToCart(t, variantID, 1, "")

	// 轨迹 cookie 是 encodeURIComponent(JSON)，用 url.QueryEscape 生成同样的形状。
	trailJSON, err := json.Marshal([][]any{
		{"/p/1", "商品页", 1699999100},
		{"/cart", "购物车", 1699999200},
	})
	if err != nil {
		t.Fatalf("构造轨迹失败: %v", err)
	}

	req := f.checkoutReq(snap.Cookie)
	req.Tracking = cartdto.TrackCookies{
		Current: "t=utm&s=google&m=cpc&c=spring_sale&n=logo_a&k=shoes&i=cmp-1&r=www.google.com&ts=1700000000&id_gclid=ABC123&cid=ABC123",
		First:   "t=referral&s=partner.example&m=referral&r=partner.example&rp=/landing&ts=1699000000",
		Session: "e=/home&p=4&st=1699999000&d=mobile&sc=390x844",
		Visitor: "n=3&f=1698000000",
		Trail:   url.QueryEscape(string(trailJSON)),
	}

	res, err := f.cart.Checkout(ctx, req)
	if err != nil {
		t.Fatalf("结算失败: %v", err)
	}
	detail, err := f.orders.GetOrder(ctx, &orderdto.GetOrderReq{ProjectID: f.projectID, OrderID: res.OrderID})
	if err != nil {
		t.Fatalf("读订单失败: %v", err)
	}
	a := detail.Head.Attribution
	if a == nil {
		t.Fatal("带归因 cookie 的下单必须把归因落进 orders.attribution")
	}

	// 本次触达（last-touch 口径）
	if a.SourceType != "utm" || a.Referrer != "www.google.com" {
		t.Fatalf("本次触达归因不对: type=%q referrer=%q", a.SourceType, a.Referrer)
	}
	if a.UTM.Source != "google" || a.UTM.Medium != "cpc" || a.UTM.Campaign != "spring_sale" {
		t.Fatalf("UTM 家族不对: %+v", a.UTM)
	}
	if a.UTM.ID != "cmp-1" {
		t.Fatalf("utm_id 应落到 UTM.ID，实际 %q", a.UTM.ID)
	}
	if a.Ad.GCLID != "ABC123" || a.Ad.ClickID != "ABC123" {
		t.Fatalf("广告点击 id 不对: %+v", a.Ad)
	}

	// 首次触达（first-touch 口径）必须与本次触达**分开**存：
	// 「先被引荐链接带来、几天后点广告下单」这两个答案本来就不同。
	if a.First.SourceType != "referral" || a.First.Referrer != "partner.example" {
		t.Fatalf("首触归因不对: %+v", a.First)
	}
	if a.First.SourceType == a.SourceType {
		t.Fatal("首触与本次触达被写成了同一份 —— 那 first-touch 分析就没得做了")
	}
	if a.First.Landing != "/landing" {
		t.Fatalf("首触落地页应为 /landing，实际 %q", a.First.Landing)
	}
	if a.Landing != "/landing" {
		t.Fatalf("Landing 应是首次到站那一页，实际 %q", a.Landing)
	}
	if a.First.At == "" {
		t.Fatal("首触时间要落 RFC3339（否则分析里无法做获客周期）")
	}

	// 会话事实
	if a.Session.Entry != "/home" || a.Session.Pages != 4 || a.Session.Count != 3 {
		t.Fatalf("会话事实不对: %+v", a.Session)
	}
	if a.Session.DurationSeconds <= 0 {
		t.Fatalf("会话时长应由开始时间推出来，实际 %d", a.Session.DurationSeconds)
	}

	// 设备：UA 由服务端填（请求真正带来的那个），类型与屏幕由脚本给。
	if a.Device.Type != "mobile" || a.Device.Screen != "390x844" {
		t.Fatalf("设备信息不对: %+v", a.Device)
	}
	if a.Device.UserAgent != req.UserAgent {
		t.Fatalf("UA 应取请求头，实际 %q", a.Device.UserAgent)
	}

	// 浏览轨迹：停留秒数由相邻两条的时间差推出。
	if len(a.Trail) != 2 {
		t.Fatalf("轨迹应有 2 条，实际 %d（%+v）", len(a.Trail), a.Trail)
	}
	if a.Trail[0].URL != "/p/1" || a.Trail[0].Title != "商品页" {
		t.Fatalf("轨迹第一条不对: %+v", a.Trail[0])
	}
	if a.Trail[0].Seconds != 100 {
		t.Fatalf("第一页停留应为 1699999200-1699999100=100 秒，实际 %d", a.Trail[0].Seconds)
	}
}

// TestCartCheckoutIsIdempotentByRequestID 同一幂等键重复结算只落一单、只扣一次库存。
func TestCartCheckoutIsIdempotentByRequestID(t *testing.T) {
	f := newCartFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, variantID := f.addProduct(t, "幂等商品", 20, 10)
	snap := f.addToCart(t, variantID, 2, "")

	first, err := f.cart.Checkout(ctx, f.checkoutReq(snap.Cookie))
	if err != nil {
		t.Fatalf("首次结算失败: %v", err)
	}
	// 第二次用**同一个 cookie 与同一个 requestId**（模拟访客连点两次 / 浏览器重发）。
	second, err := f.cart.Checkout(ctx, f.checkoutReq(snap.Cookie))
	if err != nil {
		t.Fatalf("重复结算不该报错（幂等键命中应原样返回既有单）: %v", err)
	}
	if second.OrderID != first.OrderID || second.OrderNo != first.OrderNo {
		t.Fatalf("幂等键命中应返回同一张单: %d/%s vs %d/%s",
			first.OrderID, first.OrderNo, second.OrderID, second.OrderNo)
	}

	list, err := f.orders.ListOrders(ctx, &orderdto.ListOrderReq{ProjectID: f.projectID, Limit: 50})
	if err != nil {
		t.Fatalf("查订单列表失败: %v", err)
	}
	if list.Total != 1 {
		t.Fatalf("重复结算只应落一单，实际 %d 单", list.Total)
	}
	if got := f.stockOf(t, variantID); got != 8 {
		t.Fatalf("库存只该被扣一次（10 → 8），实际 %d —— 幂等失效就是重复扣库存", got)
	}
	if !second.Paid {
		t.Fatal("重复结算时订单已经是已付款，结果应仍然报告已支付")
	}
}

// TestCartTamperedCookieIsTreatedAsEmpty 篡改过的购物车 cookie 一律当空车。
func TestCartTamperedCookieIsTreatedAsEmpty(t *testing.T) {
	f := newCartFixture(t)
	if f == nil {
		return
	}
	_, variantID := f.addProduct(t, "防篡改商品", 10, 20)

	good := f.addToCart(t, variantID, 5, "")
	if good.ItemCount != 5 {
		t.Fatalf("前置条件：应有 5 件，实际 %d", good.ItemCount)
	}
	// 改掉签名段的最后一个字符（篡改者能改的就是 cookie 值本身）。
	forged := good.Cookie[:len(good.Cookie)-1] + "0"
	if forged == good.Cookie {
		forged = good.Cookie[:len(good.Cookie)-1] + "1"
	}

	snap := f.addToCart(t, variantID, 1, forged)
	if snap.ItemCount != 1 {
		t.Fatalf("篡改过的 cookie 必须被丢弃（当空车），实际件数 %d —— 签名失效等于购物车可伪造", snap.ItemCount)
	}
}

// TestCartCheckoutKeepsOrderWhenPaymentFails 支付失败时订单必须留下（停在待付款，带单号）。
func TestCartCheckoutKeepsOrderWhenPaymentFails(t *testing.T) {
	f := newCartFixtureWithGateway(t, failingGateway{})
	if f == nil {
		return
	}
	ctx := context.Background()
	_, variantID := f.addProduct(t, "支付失败商品", 40, 5)
	snap := f.addToCart(t, variantID, 1, "")

	res, err := f.cart.Checkout(ctx, f.checkoutReq(snap.Cookie))
	if err != nil {
		t.Fatalf("支付失败**不该**让结算报错（订单已经建好了）: %v", err)
	}
	if res.Paid {
		t.Fatal("通道失败时不应报告已支付")
	}
	if res.PaymentError == "" {
		t.Fatal("通道失败必须给出面向访客的原因文案")
	}
	if res.OrderNo == "" {
		t.Fatal("支付失败也必须把单号给到访客 —— 否则他手里什么都不剩，而订单已经扣过库存")
	}
	if res.Cookie == "" {
		t.Fatal("支付失败也要清空购物车：东西已经变成订单了")
	}

	detail, err := f.orders.GetOrder(ctx, &orderdto.GetOrderReq{ProjectID: f.projectID, OrderID: res.OrderID})
	if err != nil {
		t.Fatalf("读订单失败: %v", err)
	}
	if detail.Head.Status != ordermodel.OrderStatusPending {
		t.Fatalf("支付未完成的订单应停在 pending，实际 %s", detail.Head.Status)
	}
	if detail.Head.PaidAt != nil {
		t.Fatal("没收到钱的订单不该有 paid_at")
	}
	// 待付款的订单照样占着库存：不占的话客户回头付款时货可能已经卖光。
	if got := f.stockOf(t, variantID); got != 4 {
		t.Fatalf("待付款订单的库存应已扣减（5 → 4），实际 %d", got)
	}
}

// TestCartAddWithinLimitsRejected 加购的边界：超库存与不存在的变体。
func TestCartAddWithinLimitsRejected(t *testing.T) {
	f := newCartFixture(t)
	if f == nil {
		return
	}
	_, variantID := f.addProduct(t, "边界商品", 10, 3)

	_, err := f.cart.Add(context.Background(), &cartdto.CartAddReq{
		ProjectID: f.projectID, VariantID: variantID, Quantity: 5, Cookie: "",
	})
	if err == nil || err.Error() != cartenums.ErrOutOfStock {
		t.Fatalf("超过可用量的加购应被拒（库存 3，请求 5），实际 %v", err)
	}

	_, err = f.cart.Add(context.Background(), &cartdto.CartAddReq{
		ProjectID: f.projectID, VariantID: "99999999-9999-9999-9999-999999999999", Quantity: 1,
	})
	if err == nil || err.Error() != cartenums.ErrVariantNotFound {
		t.Fatalf("不存在的变体应被拒，实际 %v", err)
	}
}

// TestCartSetQuantityZeroRemovesLine 数量置 0 即移除该行。
func TestCartSetQuantityZeroRemovesLine(t *testing.T) {
	f := newCartFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, v1 := f.addProduct(t, "移除商品A", 10, 10)
	_, v2 := f.addProduct(t, "移除商品B", 20, 10)

	snap := f.addToCart(t, v1, 2, "")
	snap = f.addToCart(t, v2, 1, snap.Cookie)
	if snap.LineCount != 2 || snap.ItemCount != 3 {
		t.Fatalf("前置条件：应 2 行 3 件，实际 %d 行 %d 件", snap.LineCount, snap.ItemCount)
	}

	after, err := f.cart.SetQuantity(ctx, &cartdto.CartSetQuantityReq{
		ProjectID: f.projectID, VariantID: v1, Quantity: 0, Cookie: snap.Cookie,
	})
	if err != nil {
		t.Fatalf("移除失败: %v", err)
	}
	if after.LineCount != 1 || after.ItemCount != 1 {
		t.Fatalf("移除一件后应剩 1 行 1 件，实际 %d 行 %d 件", after.LineCount, after.ItemCount)
	}
	if after.Total != 2000 {
		t.Fatalf("剩余小计应为 2000 分，实际 %d", after.Total)
	}
}
