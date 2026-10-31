package cartservice

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	membershipdto "go_wp/internal/module/membership/dto"
	productcontract "go_wp/internal/module/product/contract"
	projectcontract "go_wp/internal/module/project/contract"
)

type stubShippingMembership struct {
	res *membershipdto.MembershipResp
	err error

	calls int
}

func (s *stubShippingMembership) Resolve(_ context.Context, _ *membershipdto.ResolveReq) (*membershipdto.MembershipResp, error) {
	s.calls++
	return s.res, s.err
}

type stubShippingPolicy struct {
	policy projectcontract.ShippingPolicy
	err    error
	calls  int
}

func (s *stubShippingPolicy) ShippingPolicyOf(_ context.Context, _ string) (projectcontract.ShippingPolicy, error) {
	s.calls++
	return s.policy, s.err
}

type stubShippingProduct struct {
	snaps []*productcontract.VariantSnapshot
	err   error
	calls int
}

func (s *stubShippingProduct) VariantSnapshots(_ context.Context, _ []string, _ string) (list []*productcontract.VariantSnapshot, err error) {
	s.calls++
	return s.snaps, s.err
}

func shippingPolicyOf(base, threshold int64) projectcontract.ShippingPolicy {
	return projectcontract.ShippingPolicy{BaseFeeCents: base, FreeThresholdCents: threshold}
}

func TestShippingTotalOfThreeTiers(t *testing.T) {
	ctx := context.Background()
	uid := uint64(42)
	const base = int64(800)

	nonMember := func() *stubShippingMembership {
		return &stubShippingMembership{res: &membershipdto.MembershipResp{
			UserID: uid, TierID: 1, TierName: "普通", FreeShipping: false,
		}}
	}

	member := nonMember()
	svc := &Service{membership: member}
	if got := svc.shippingTotalOf(ctx, "p-1", &uid, 3000, shippingPolicyOf(base, 10000)); got != base {
		t.Fatalf("① 未达门槛的非会员应收 %d，实际 %d", base, got)
	}
	if member.calls != 1 {
		t.Fatalf("① 未达门槛时应解析一次会员身份，实际 %d 次", member.calls)
	}

	member = nonMember()
	svc = &Service{membership: member}
	for _, subtotal := range []int64{10000, 12000} {
		if got := svc.shippingTotalOf(ctx, "p-1", &uid, subtotal, shippingPolicyOf(base, 10000)); got != 0 {
			t.Fatalf("② 小计 %d 已达门槛应免运费，实际 %d", subtotal, got)
		}
	}
	if member.calls != 0 {
		t.Fatalf("② 已达门槛时不该再解析会员身份，实际 %d 次", member.calls)
	}

	freeMember := &stubShippingMembership{res: &membershipdto.MembershipResp{
		UserID: uid, TierID: 2, TierName: "黄金", FreeShipping: true,
	}}
	svc = &Service{membership: freeMember}
	if got := svc.shippingTotalOf(ctx, "p-1", &uid, 3000, shippingPolicyOf(base, 10000)); got != 0 {
		t.Fatalf("② 免运费会员应归 0，实际 %d", got)
	}

	member = nonMember()
	svc = &Service{membership: member}
	for _, policy := range []projectcontract.ShippingPolicy{
		shippingPolicyOf(0, 0),
		shippingPolicyOf(0, 10000),
		shippingPolicyOf(-1, 10000),
	} {
		if got := svc.shippingTotalOf(ctx, "p-1", &uid, 3000, policy); got != 0 {
			t.Fatalf("③ 基础运费 %d 应为 0，实际 %d", policy.BaseFeeCents, got)
		}
	}
	if member.calls != 0 {
		t.Fatalf("③ 无运费可免时不应解析会员身份，实际 %d 次", member.calls)
	}
}

func TestShippingTotalOfThresholdZeroNeverFrees(t *testing.T) {
	uid := uint64(42)
	reader := &stubShippingMembership{res: &membershipdto.MembershipResp{FreeShipping: false}}
	svc := &Service{membership: reader}

	if got := svc.shippingTotalOf(context.Background(), "p-1", &uid, 999999, shippingPolicyOf(800, 0)); got != 800 {
		t.Fatalf("门槛 0（不启用）时应照收 800，实际 %d", got)
	}
}

func TestShippingTotalOfDegradesToBaseOnError(t *testing.T) {
	uid := uint64(42)
	reader := &stubShippingMembership{err: errors.New("db down")}
	svc := &Service{membership: reader}

	const base = int64(800)
	if got := svc.shippingTotalOf(context.Background(), "p-1", &uid, 3000, shippingPolicyOf(base, 10000)); got != base {
		t.Fatalf("会员端口报错时应照收 %d，实际 %d", base, got)
	}
}

func TestShippingTotalOfWithoutPortOrUserIsBase(t *testing.T) {
	const base = int64(800)
	ctx := context.Background()
	uid := uint64(42)
	zero := uint64(0)
	policy := shippingPolicyOf(base, 0)

	if got := (&Service{}).shippingTotalOf(ctx, "p-1", &uid, 3000, policy); got != base {
		t.Fatalf("未注入会员端口应照收 %d，实际 %d", base, got)
	}
	svc := &Service{membership: &stubShippingMembership{res: &membershipdto.MembershipResp{FreeShipping: true}}}
	if got := svc.shippingTotalOf(ctx, "p-1", nil, 3000, policy); got != base {
		t.Fatalf("未登录应照收 %d，实际 %d", base, got)
	}
	if got := svc.shippingTotalOf(ctx, "p-1", &zero, 3000, policy); got != base {
		t.Fatalf("userID=0 应照收 %d，实际 %d", base, got)
	}

	if got := svc.shippingTotalOf(ctx, "   ", &uid, 3000, policy); got != base {
		t.Fatalf("空工程应照收 %d，实际 %d", base, got)
	}
	if reader := svc.membership.(*stubShippingMembership); reader.calls != 0 {
		t.Fatalf("空工程 / 未登录都不该调会员端口，实际 %d 次", reader.calls)
	}
}

func TestShippingPolicyOfSkipsPortWhenNotWired(t *testing.T) {
	port := &stubShippingPolicy{policy: shippingPolicyOf(800, 0)}
	ctx := context.Background()

	if got := (&Service{}).shippingPolicyOf(ctx, "p-1"); got != (projectcontract.ShippingPolicy{}) {
		t.Fatalf("未注入端口应返回零值，实际 %+v", got)
	}
	svc := &Service{shippingPolicy: port}
	if got := svc.shippingPolicyOf(ctx, "  "); got != (projectcontract.ShippingPolicy{}) {
		t.Fatalf("空工程应返回零值，实际 %+v", got)
	}
	if port.calls != 0 {
		t.Fatalf("空工程不该读端口，实际 %d 次", port.calls)
	}

	if got := svc.shippingPolicyOf(ctx, "p-1"); got.BaseFeeCents != 800 {
		t.Fatalf("应读到基础运费 800，实际 %+v", got)
	}
	if port.calls != 1 {
		t.Fatalf("正常路径应读一次端口，实际 %d 次", port.calls)
	}
}

func TestShippingPolicyOfDegradesToZeroOnError(t *testing.T) {
	port := &stubShippingPolicy{err: errors.New("db down")}
	svc := &Service{shippingPolicy: port}

	if got := svc.shippingPolicyOf(context.Background(), "p-1"); got != (projectcontract.ShippingPolicy{}) {
		t.Fatalf("读规则失败应按不收运费（零值），实际 %+v", got)
	}
	if port.calls != 1 {
		t.Fatalf("应尝试读一次，实际 %d 次", port.calls)
	}
}

func TestCartSubtotalOf(t *testing.T) {
	ctx := context.Background()
	lines := []cartPayloadLine{
		{VariantID: "v1", Quantity: 2},
		{VariantID: "v2", Quantity: 1},
		{VariantID: "v3", Quantity: 1},
		{VariantID: "v4", Quantity: 0},
		{VariantID: "v5", Quantity: 1},
	}
	product := &stubShippingProduct{snaps: []*productcontract.VariantSnapshot{
		{VariantID: "v1", ProjectID: "p-1", Price: 100, Enabled: true},
		{VariantID: "v2", ProjectID: "p-1", Price: 50, Enabled: false},
		{VariantID: "v3", ProjectID: "p-2", Price: 70, Enabled: true},
		{VariantID: "v4", ProjectID: "p-1", Price: 30, Enabled: true},
	}}
	svc := &Service{product: product}

	if got := svc.cartSubtotalOf(ctx, "p-1", lines); got != 200 {
		t.Fatalf("小计应为 200（只计在售且属本工程的行），实际 %d", got)
	}

	failing := &Service{product: &stubShippingProduct{err: errors.New("db down")}}
	if got := failing.cartSubtotalOf(ctx, "p-1", lines); got != 0 {
		t.Fatalf("商品端口报错时应归 0（不达门槛），实际 %d", got)
	}
	if got := (&Service{}).cartSubtotalOf(ctx, "p-1", lines); got != 0 {
		t.Fatalf("商品端口未注入时应归 0，实际 %d", got)
	}
}

func TestShippingIsPricedInOnePlace(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("读取本包目录失败: %v", err)
	}
	counts := map[string]int{
		"func (s *Service) shippingTotalOf(": 0,
		"ShippingTotal:":                     0,
		".ShippingPolicyOf(":                 0,
	}
	scanned := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		b, rerr := os.ReadFile(name)
		if rerr != nil {
			t.Fatalf("读 %s 失败: %v", name, rerr)
		}
		scanned++
		src := string(b)
		for needle := range counts {
			counts[needle] += strings.Count(src, needle)
		}
	}
	if scanned == 0 {
		t.Fatal("没有扫到任何非测试源码文件：判据的空转形态，路径假设变了要同步改")
	}
	for needle := range counts {
		if counts[needle] != 1 {
			t.Errorf("%q 应恰好出现 1 次（实际 %d 次）—— 运费定价/传递/规则读取必须只有一处出口",
				needle, counts[needle])
		}
	}
}
