package orderservice

// order_membership_discount_test.go — 会员折扣的金额口径（BIZ-3 消费侧接入）。
//
// 为什么这组用例必须存在：会员折扣改变了「顾客付多少钱」这条链路，而它接在一处
// 资损缺陷的历史现场旁边（SEC-001：无券时折扣恒为 0）。口径写错的两种形态都不会报错：
//   · 会员折扣并进 discount_total → SEC-001 那条判据出现常规例外；
//   · 折扣合计超过小计 → total 变负数，一路传到支付金额上。
//
// 用例里的算式是**真实函数**的组合（membershipDiscountAmount / allocateLineDiscounts），
// 只有「先券、后会员折扣、再分摊」这个顺序在测试里显式写出 —— 它同时是 buildOrderDraft
// 的顺序（见该函数的文件头注释）。

import (
	"context"
	"errors"
	"testing"

	membershipcontract "go_wp/internal/module/membership/contract"
	membershipdto "go_wp/internal/module/membership/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
)

// TestMembershipDiscountAmount 折扣额的取值域与上界。
func TestMembershipDiscountAmount(t *testing.T) {
	cases := []struct {
		name      string
		subtotal  int64
		remaining int64
		percent   int64
		want      int64
	}{
		{name: "打八折即扣 20%", subtotal: 10000, remaining: 10000, percent: 20, want: 2000},
		{name: "除不尽部分归商家（向下取整）", subtotal: 1001, remaining: 1001, percent: 33, want: 330},
		{name: "无折扣权益", subtotal: 10000, remaining: 10000, percent: 0, want: 0},
		{name: "负百分比不产生加价", subtotal: 10000, remaining: 10000, percent: -20, want: 0},
		{name: "百分比越上界按 100 夹住", subtotal: 10000, remaining: 10000, percent: 180, want: 10000},
		{name: "上限受券扣完的余额约束", subtotal: 10000, remaining: 3000, percent: 50, want: 3000},
		{name: "券已吃满小计则会员折扣为 0", subtotal: 10000, remaining: 0, percent: 50, want: 0},
		{name: "小计为 0", subtotal: 0, remaining: 0, percent: 50, want: 0},
		{name: "负小计不产生折扣", subtotal: -100, remaining: -100, percent: 50, want: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := membershipDiscountAmount(tc.subtotal, tc.remaining, tc.percent); got != tc.want {
				t.Fatalf("会员折扣应为 %d 分，实际 %d", tc.want, got)
			}
		})
	}
}

// TestOrderTotalRecalcWithCouponAndMembership 复算证据：券与会员折扣各自入账，总额三方平衡。
//
// 算式：total = 小计 − 券 − 会员折扣 + 运费；
// 同时把分摊后的行实付也复算一遍 —— 退款按行实付算，分摊少一份就是退多一份。
func TestOrderTotalRecalcWithCouponAndMembership(t *testing.T) {
	const (
		subtotal      = int64(10000)
		couponPercent = int64(20) // 券：减 2000
		memberPercent = int64(10) // 会员：减 1000
		shipping      = int64(800)
	)
	couponDiscount := subtotal * couponPercent / 100
	membershipDiscount := membershipDiscountAmount(subtotal, subtotal-couponDiscount, memberPercent)

	items := []*ordermodel.OrderItemEntity{
		{LineSubtotal: 6000, Quantity: 1},
		{LineSubtotal: 4000, Quantity: 1},
	}
	allocateLineDiscounts(items, subtotal, couponDiscount+membershipDiscount)
	total := subtotal - couponDiscount - membershipDiscount + shipping

	if couponDiscount != 2000 {
		t.Fatalf("券折扣应为 2000，实际 %d", couponDiscount)
	}
	if membershipDiscount != 1000 {
		t.Fatalf("会员折扣应为 1000，实际 %d", membershipDiscount)
	}
	if total != 7800 {
		t.Fatalf("总额应为 10000−2000−1000+800=7800，实际 %d", total)
	}
	// 各自入账：两列分别存券与会员折扣，相加才是总扣减。
	if got := couponDiscount + membershipDiscount; got != subtotal-total+shipping {
		t.Fatalf("总扣减应为 %d，实际 %d", subtotal-total+shipping, got)
	}
	var lineDiscountSum, lineTotalSum int64
	for _, it := range items {
		lineDiscountSum += it.LineDiscount
		lineTotalSum += it.LineTotal
	}
	if lineDiscountSum != couponDiscount+membershipDiscount {
		t.Fatalf("行折扣之和应等于两项折扣合计 %d，实际 %d", couponDiscount+membershipDiscount, lineDiscountSum)
	}
	if lineTotalSum != subtotal-couponDiscount-membershipDiscount {
		t.Fatalf("行实付之和应为 %d，实际 %d", subtotal-couponDiscount-membershipDiscount, lineTotalSum)
	}
}

// TestOrderTotalNeverNegativeWithBothDiscounts 券 100% + 会员折扣并存时总额不为负。
//
// 这是「相加扣减」最容易踩的一格：两道折扣各自对小计算，合计会超过小计。
// 负数总额不报错，它会顺着 total 传到支付金额上 —— 让通道按负数扣款（或直接拒单）。
func TestOrderTotalNeverNegativeWithBothDiscounts(t *testing.T) {
	const subtotal = int64(10000)
	items := []*ordermodel.OrderItemEntity{{LineSubtotal: subtotal, Quantity: 1}}

	// 券 100%（满额券）：折扣夹到小计。
	couponDiscount := subtotal
	membershipDiscount := membershipDiscountAmount(subtotal, subtotal-couponDiscount, 50)
	allocateLineDiscounts(items, subtotal, couponDiscount+membershipDiscount)
	total := subtotal - couponDiscount - membershipDiscount

	if membershipDiscount != 0 {
		t.Fatalf("券已吃满小计，会员折扣应为 0，实际 %d", membershipDiscount)
	}
	if total != 0 {
		t.Fatalf("总额应为 0（不为负），实际 %d", total)
	}
	if items[0].LineTotal != 0 {
		t.Fatalf("行实付应为 0，实际 %d", items[0].LineTotal)
	}
}

// stubMembershipReader 会员身份读取端口的替身（只实现契约里的一条只读方法）。
type stubMembershipReader struct {
	res *membershipdto.MembershipResp
	err error
	// calls 记录被调用次数：用于断言「未登录时不查库」这条取舍。
	calls int
}

func (s *stubMembershipReader) Resolve(_ context.Context, _ *membershipdto.ResolveReq) (*membershipdto.MembershipResp, error) {
	s.calls++
	return s.res, s.err
}

// 编译期断言：替身确实实现了消费侧收窄契约（少一个方法要在编译期就炸）。
var _ membershipcontract.Reader = (*stubMembershipReader)(nil)

// TestResolveMembershipDiscountSkipsQueryWithoutUser 没有账号时不查库。
//
// 访客还没开号就没有会员身份（AGENTS.md 不变量 1 的取舍：不为一个必然没有结论的问题
// 花一次数据库往返）。
func TestResolveMembershipDiscountSkipsQueryWithoutUser(t *testing.T) {
	reader := &stubMembershipReader{res: &membershipdto.MembershipResp{DiscountPercent: 50}}
	svc := &Service{membership: reader}

	if got := svc.resolveMembershipDiscount(context.Background(), "p-1", nil, 10000, 0); got != 0 {
		t.Fatalf("无账号应为 0 折扣，实际 %d", got)
	}
	zero := uint64(0)
	if got := svc.resolveMembershipDiscount(context.Background(), "p-1", &zero, 10000, 0); got != 0 {
		t.Fatalf("userID=0 应为 0 折扣，实际 %d", got)
	}
	if reader.calls != 0 {
		t.Fatalf("无账号时不应调会员端口，实际调了 %d 次", reader.calls)
	}
}

// TestResolveMembershipDiscountDegradesOnError 解析失败降级为「不打折」而不是免单。
//
// 判据是失效方向：一次数据库抖动若被解释成「免运费/免折扣」，受影响的是全站订单金额。
func TestResolveMembershipDiscountDegradesOnError(t *testing.T) {
	reader := &stubMembershipReader{err: errors.New("db down")}
	svc := &Service{membership: reader}
	uid := uint64(7)

	if got := svc.resolveMembershipDiscount(context.Background(), "p-1", &uid, 10000, 0); got != 0 {
		t.Fatalf("端口报错时应降级为 0 折扣，实际 %d", got)
	}
	if reader.calls != 1 {
		t.Fatalf("应尝试解析一次，实际 %d 次", reader.calls)
	}
}

// TestResolveMembershipDiscountWithoutPortIsZero 端口未注入 = 折扣功能未开启。
func TestResolveMembershipDiscountWithoutPortIsZero(t *testing.T) {
	svc := &Service{}
	uid := uint64(7)
	if got := svc.resolveMembershipDiscount(context.Background(), "p-1", &uid, 10000, 0); got != 0 {
		t.Fatalf("未注入端口应为 0 折扣，实际 %d", got)
	}
}

// TestResolveMembershipDiscountAppliesBenefit 有折扣权益时按小计计算并夹在余额内。
func TestResolveMembershipDiscountAppliesBenefit(t *testing.T) {
	uid := uint64(7)
	svc := &Service{membership: &stubMembershipReader{res: &membershipdto.MembershipResp{
		UserID: uid, TierID: 3, TierName: "黄金", DiscountPercent: 20,
	}}}
	if got := svc.resolveMembershipDiscount(context.Background(), "p-1", &uid, 10000, 0); got != 2000 {
		t.Fatalf("20%% 折扣应为 2000，实际 %d", got)
	}
	// 券已扣 9000 时余额只剩 1000，会员折扣不得把它放大成 2000。
	if got := svc.resolveMembershipDiscount(context.Background(), "p-1", &uid, 10000, 9000); got != 1000 {
		t.Fatalf("受券后余额约束应为 1000，实际 %d", got)
	}
}

// TestSpentTotalsByUserRejectsEmptyProject 批量端口缺工程即参数错误。
//
// 不返回空 map：「没给工程」与「这个工程一分钱消费都没有」在排障时方向完全相反，
// 而 membership 侧正是靠 Scanned 计数分辨这一类静默故障的。
func TestSpentTotalsByUserRejectsEmptyProject(t *testing.T) {
	svc := &Service{}
	if _, err := svc.SpentTotalsByUser(context.Background(), "   "); err == nil || err.Error() != orderenums.ErrProjectRequired {
		t.Fatalf("空工程应返回 %s，实际 %v", orderenums.ErrProjectRequired, err)
	}
}
