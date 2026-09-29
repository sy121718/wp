package cartservice

// cart_shipping_test.go — 结算运费的**取值链**（站点基础运费 → 满额免运费门槛 → 会员免运费）。
//
// 免运费是「少收一笔钱」的能力，所以判据全在两个地方：
//
//	① **失效方向**：端口报错若被当成「免运费」，一次数据库抖动就是全站订单免运费
//	   （少收钱、不可追溯）。所以下面把每种失效形态逐条钉死，而不是只测「有权益时归零」。
//	② **环节顺序与短路**：达门槛命中之后就不该再问会员身份，基础运费为 0 时两个端口
//	   都不该被碰 —— 结算路径上每一次多余往返都是访客在等，而「多读一次也不报错」
//	   的退化没有任何测试会红，只能靠这里钉。
//
// 另有一条源码级断言（TestShippingIsPricedInOnePlace）：运费**只在一个函数里定价**、
// 订单域的运费入参只出现在一处。把「该收多少运费」的判断散到第二处的那天，两处就会
// 开始分叉，而分叉表现为「购物车说免运费、订单里收了运费」这种只有客户能发现的错。

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

// stubShippingMembership 会员身份读取端口的替身（只实现契约里的一条只读方法）。
type stubShippingMembership struct {
	res *membershipdto.MembershipResp
	err error
	// calls 记录调用次数：用于断言「没有运费可免时不查库」「已达门槛不重复问」。
	calls int
}

func (s *stubShippingMembership) Resolve(_ context.Context, _ *membershipdto.ResolveReq) (*membershipdto.MembershipResp, error) {
	s.calls++
	return s.res, s.err
}

// stubShippingPolicy 站点运费规则端口的替身。
type stubShippingPolicy struct {
	policy projectcontract.ShippingPolicy
	err    error
	calls  int
}

func (s *stubShippingPolicy) ShippingPolicyOf(_ context.Context, _ string) (projectcontract.ShippingPolicy, error) {
	s.calls++
	return s.policy, s.err
}

// stubShippingProduct 商品快照端口的替身（只服务 cartSubtotalOf）。
type stubShippingProduct struct {
	snaps []*productcontract.VariantSnapshot
	err   error
	calls int
}

func (s *stubShippingProduct) VariantSnapshots(_ context.Context, _ []string, _ string) (list []*productcontract.VariantSnapshot, err error) {
	s.calls++
	return s.snaps, s.err
}

// shippingPolicyOf 便利构造：分 → 规则。
func shippingPolicyOf(base, threshold int64) projectcontract.ShippingPolicy {
	return projectcontract.ShippingPolicy{BaseFeeCents: base, FreeThresholdCents: threshold}
}

// TestShippingTotalOfThreeTiers 三档交互（本批的核心金额复算）。
//
//	① 有基础运费、未达门槛、非会员 → 收基础运费；
//	② 达门槛（或有免运费权益）→ 0；
//	③ 基础运费本身就是 0 → 0（不是「免费优惠」，而是这个站点不运收费）。
func TestShippingTotalOfThreeTiers(t *testing.T) {
	ctx := context.Background()
	uid := uint64(42)
	const base = int64(800)

	nonMember := func() *stubShippingMembership {
		return &stubShippingMembership{res: &membershipdto.MembershipResp{
			UserID: uid, TierID: 1, TierName: "普通", FreeShipping: false,
		}}
	}

	// ① 有运费、没达门槛、也没有权益：照收 800。
	member := nonMember()
	svc := &Service{membership: member}
	if got := svc.shippingTotalOf(ctx, "p-1", &uid, 3000, shippingPolicyOf(base, 10000)); got != base {
		t.Fatalf("① 未达门槛的非会员应收 %d，实际 %d", base, got)
	}
	if member.calls != 1 {
		t.Fatalf("① 未达门槛时应解析一次会员身份，实际 %d 次", member.calls)
	}

	// ②a 达门槛：0，且**不再问会员身份**（已经免了，再解析一次只是多一次往返）。
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

	// ②b 未达门槛，但会员有 free_shipping：0。
	freeMember := &stubShippingMembership{res: &membershipdto.MembershipResp{
		UserID: uid, TierID: 2, TierName: "黄金", FreeShipping: true,
	}}
	svc = &Service{membership: freeMember}
	if got := svc.shippingTotalOf(ctx, "p-1", &uid, 3000, shippingPolicyOf(base, 10000)); got != 0 {
		t.Fatalf("② 免运费会员应归 0，实际 %d", got)
	}

	// ③ 基础运费为 0：两个端口都不该被碰（0 就是最终答案）。
	member = nonMember()
	svc = &Service{membership: member}
	for _, policy := range []projectcontract.ShippingPolicy{
		shippingPolicyOf(0, 0),
		shippingPolicyOf(0, 10000),
		shippingPolicyOf(-1, 10000), // 存储被写坏时的防御：负值同样按「不收运费」
	} {
		if got := svc.shippingTotalOf(ctx, "p-1", &uid, 3000, policy); got != 0 {
			t.Fatalf("③ 基础运费 %d 应为 0，实际 %d", policy.BaseFeeCents, got)
		}
	}
	if member.calls != 0 {
		t.Fatalf("③ 无运费可免时不应解析会员身份，实际 %d 次", member.calls)
	}
}

// TestShippingTotalOfThresholdZeroNeverFrees 门槛 0 = 不启用，绝不能被解释成「0 元就免」。
//
// 门禁 > 0 才算启用：漏掉这个判断时，一个「没配门槛」的站点会变成全场免运费 ——
// 而它的表现与「配了门槛」一模一样，只有对账时才能发现。
func TestShippingTotalOfThresholdZeroNeverFrees(t *testing.T) {
	uid := uint64(42)
	reader := &stubShippingMembership{res: &membershipdto.MembershipResp{FreeShipping: false}}
	svc := &Service{membership: reader}

	if got := svc.shippingTotalOf(context.Background(), "p-1", &uid, 999999, shippingPolicyOf(800, 0)); got != 800 {
		t.Fatalf("门槛 0（不启用）时应照收 800，实际 %d", got)
	}
}

// TestShippingTotalOfDegradesToBaseOnError 会员解析失败照收运费（不解释成免运费）。
func TestShippingTotalOfDegradesToBaseOnError(t *testing.T) {
	uid := uint64(42)
	reader := &stubShippingMembership{err: errors.New("db down")}
	svc := &Service{membership: reader}

	const base = int64(800)
	if got := svc.shippingTotalOf(context.Background(), "p-1", &uid, 3000, shippingPolicyOf(base, 10000)); got != base {
		t.Fatalf("会员端口报错时应照收 %d，实际 %d", base, got)
	}
}

// TestShippingTotalOfWithoutPortOrUserIsBase 会员端口未注入 / 未登录一律照收。
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
	// 工程为空：问不出会员身份（端口要求工程作用域），照收。
	if got := svc.shippingTotalOf(ctx, "   ", &uid, 3000, policy); got != base {
		t.Fatalf("空工程应照收 %d，实际 %d", base, got)
	}
	if reader := svc.membership.(*stubShippingMembership); reader.calls != 0 {
		t.Fatalf("空工程 / 未登录都不该调会员端口，实际 %d 次", reader.calls)
	}
}

// TestShippingPolicyOfSkipsPortWhenNotWired 端口未注入 / 工程为空 → 零值（不收运费），不报错。
//
// 这是**合法的部署形态**（站点没配运费功能），不是故障 —— 所以它不该打日志、
// 也不该让结算失败，只是运费恒 0（与接入站点策略之前逐字一致）。
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
	// 正常路径：一次读取，原样返回（**唯一**读站点设置的入口，见 TestShippingIsPricedInOnePlace）。
	if got := svc.shippingPolicyOf(ctx, "p-1"); got.BaseFeeCents != 800 {
		t.Fatalf("应读到基础运费 800，实际 %+v", got)
	}
	if port.calls != 1 {
		t.Fatalf("正常路径应读一次端口，实际 %d 次", port.calls)
	}
}

// TestShippingPolicyOfDegradesToZeroOnError 读规则失败 → 不收运费（**不拒单**）。
//
// 失效方向与「会员解析失败照收」相反，但判据是同一条：这一侧 base 根本不知道 ——
// 编造一个金额没有依据，而 0 是系统在「没有运费策略」时的既有语义。
// 拒单则会把一次读库抖动升级成「全店无法结算」，代价不可比。
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

// TestCartSubtotalOf 小计口径：下架 / 跨工程 / 数量非正的行不计入，读失败归 0。
//
// 小计是门槛判定的输入，口径必须与订单域建单时一致 —— 否则「购物车说已满 100 免运费、
// 订单里仍收运费」这种偏差会出现在边界金额上，而那正是客户最在意的一档。
func TestCartSubtotalOf(t *testing.T) {
	ctx := context.Background()
	lines := []cartPayloadLine{
		{VariantID: "v1", Quantity: 2}, // 100 × 2 = 200
		{VariantID: "v2", Quantity: 1}, // 下架 → 不计入
		{VariantID: "v3", Quantity: 1}, // 跨工程 → 不计入
		{VariantID: "v4", Quantity: 0}, // 数量非正 → 不计入
		{VariantID: "v5", Quantity: 1}, // 快照里没有 → 不计入
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
	// 读不到商品事实 → 0：门槛恒 > 0，于是「不达门槛」= 照收运费，落在安全的一侧。
	failing := &Service{product: &stubShippingProduct{err: errors.New("db down")}}
	if got := failing.cartSubtotalOf(ctx, "p-1", lines); got != 0 {
		t.Fatalf("商品端口报错时应归 0（不达门槛），实际 %d", got)
	}
	if got := (&Service{}).cartSubtotalOf(ctx, "p-1", lines); got != 0 {
		t.Fatalf("商品端口未注入时应归 0，实际 %d", got)
	}
}

// TestShippingIsPricedInOnePlace 运费定价只有一个出口（源码级断言）。
//
// 三条判据，任一条失守都意味着「运费在别处也能被算/被改」：
//
//	① shippingTotalOf 只有一处定义；
//	② 交给订单域的 ShippingTotal 入参只出现一次（cart_checkout.go）；
//	③ 站点运费规则端口只被一个地方读（shippingPolicyOf）。
//
// 判据不是字面量匹配而是**形状**匹配（函数名 + 端口方法名），所以重命名会当场打回，
// 而不是静默退化成永远通过的空检查 —— 所以每条都断言「恰好 1 处」而不是「≥1 处」。
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
