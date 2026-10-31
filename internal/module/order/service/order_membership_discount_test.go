package orderservice

import (
	"context"
	"errors"
	"testing"

	membershipcontract "go_wp/internal/module/membership/contract"
	membershipdto "go_wp/internal/module/membership/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
)

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

func TestOrderTotalRecalcWithCouponAndMembership(t *testing.T) {
	const (
		subtotal      = int64(10000)
		couponPercent = int64(20)
		memberPercent = int64(10)
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

func TestOrderTotalNeverNegativeWithBothDiscounts(t *testing.T) {
	const subtotal = int64(10000)
	items := []*ordermodel.OrderItemEntity{{LineSubtotal: subtotal, Quantity: 1}}

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

type stubMembershipReader struct {
	res *membershipdto.MembershipResp
	err error

	calls int
}

func (s *stubMembershipReader) Resolve(_ context.Context, _ *membershipdto.ResolveReq) (*membershipdto.MembershipResp, error) {
	s.calls++
	return s.res, s.err
}

var _ membershipcontract.Reader = (*stubMembershipReader)(nil)

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

func TestResolveMembershipDiscountWithoutPortIsZero(t *testing.T) {
	svc := &Service{}
	uid := uint64(7)
	if got := svc.resolveMembershipDiscount(context.Background(), "p-1", &uid, 10000, 0); got != 0 {
		t.Fatalf("未注入端口应为 0 折扣，实际 %d", got)
	}
}

func TestResolveMembershipDiscountAppliesBenefit(t *testing.T) {
	uid := uint64(7)
	svc := &Service{membership: &stubMembershipReader{res: &membershipdto.MembershipResp{
		UserID: uid, TierID: 3, TierName: "黄金", DiscountPercent: 20,
	}}}
	if got := svc.resolveMembershipDiscount(context.Background(), "p-1", &uid, 10000, 0); got != 2000 {
		t.Fatalf("20%% 折扣应为 2000，实际 %d", got)
	}

	if got := svc.resolveMembershipDiscount(context.Background(), "p-1", &uid, 10000, 9000); got != 1000 {
		t.Fatalf("受券后余额约束应为 1000，实际 %d", got)
	}
}

func TestSpentTotalsByUserRejectsEmptyProject(t *testing.T) {
	svc := &Service{}
	if _, err := svc.SpentTotalsByUser(context.Background(), "   "); err == nil || err.Error() != orderenums.ErrProjectRequired {
		t.Fatalf("空工程应返回 %s，实际 %v", orderenums.ErrProjectRequired, err)
	}
}
