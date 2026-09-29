package membershipservice

// membership_logic_test.go — 分档、权益归一与文案出口的纯逻辑单测（BIZ-3）。
//
// 这些判断「错了会不会静默出错」的答案都是**会**：
//   - 分档错 → 会员等级算错，页面上一切正常，只有钱不对；
//   - 权益归一错 → 负折扣变加价、100 以上的折扣让应付为负；
//   - 文案出口错 → 可行动的业务提示被压成通用提示（或反过来把库原文透出去）。
//
// 不碰数据库、不走装配：这些逻辑几乎每次改动都会碰到，就近跑一次不到一秒。

import (
	"errors"
	"strings"
	"testing"

	membershipdto "go_wp/internal/module/membership/dto"
	membershipenums "go_wp/internal/module/membership/enums"
	membershipmodel "go_wp/internal/module/membership/model"
)

// tiersFixture 一份典型清单（model.ListTiers 的排序：sort_order 降序）。
func tiersFixture() []*membershipmodel.TierEntity {
	return []*membershipmodel.TierEntity{
		{ID: 3, Name: "钻石", SortOrder: 3, ThresholdAmount: 500000},
		{ID: 2, Name: "黄金", SortOrder: 2, ThresholdAmount: 100000},
		{ID: 1, Name: "普通", SortOrder: 0, ThresholdAmount: 0, IsDefault: true},
	}
}

// TestSelectTierBySpend 消费额定档：取第一个满足门槛的档（清单已按 sort_order 降序）。
func TestSelectTierBySpend(t *testing.T) {
	tiers := tiersFixture()
	cases := []struct {
		name  string
		spent int64
		want  string
	}{
		{"零消费落在普通档", 0, "普通"},
		{"差一分不满门槛", 99999, "普通"},
		{"正好等于门槛", 100000, "黄金"},
		{"中间值取低档", 250000, "黄金"},
		{"等于高档门槛", 500000, "钻石"},
		{"超过最高门槛仍是钻石", 900000, "钻石"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := selectTierBySpend(tiers, tc.spent)
			if got == nil {
				t.Fatalf("spent=%d 应命中某个档", tc.spent)
			}
			if got.Name != tc.want {
				t.Errorf("spent=%d → %s，期望 %s", tc.spent, got.Name, tc.want)
			}
		})
	}
}

// TestSelectTierBySpendSortOrderBeatsThreshold 档位高低由 sort_order 决定，不是门槛数值。
//
// 运营完全可以把「钻石」排在「黄金」之上而门槛数值不单调（例如活动期间给老客户开一档
// 低门槛的钻石）。此时若按门槛数值选档，会把钻石会员算成黄金 —— 而两行数据看起来都正常。
func TestSelectTierBySpendSortOrderBeatsThreshold(t *testing.T) {
	tiers := []*membershipmodel.TierEntity{
		{ID: 9, Name: "钻石", SortOrder: 9, ThresholdAmount: 1000},
		{ID: 2, Name: "黄金", SortOrder: 2, ThresholdAmount: 100000},
	}
	if got := selectTierBySpend(tiers, 5000); got == nil || got.Name != "钻石" {
		t.Errorf("排序在前的档应优先命中，实际 %+v", got)
	}
}

// TestSelectTierBySpendReturnsNilWhenNothingMatches 一个档都达不到时返回 nil，
// 由调用方回退默认等级 —— **不是**在这里偷偷退回清单最后一项。
func TestSelectTierBySpendReturnsNilWhenNothingMatches(t *testing.T) {
	tiers := []*membershipmodel.TierEntity{
		{ID: 3, Name: "钻石", SortOrder: 3, ThresholdAmount: 500000},
		{ID: 2, Name: "黄金", SortOrder: 2, ThresholdAmount: 100000},
	}
	if got := selectTierBySpend(tiers, 10); got != nil {
		t.Errorf("没有满足门槛的档时应返回 nil，实际 %+v", got)
	}
}

// TestResolveSpendTierFallsBackToDefault 「一个档都达不到」回退默认等级，而不是留在 nil。
func TestResolveSpendTierFallsBackToDefault(t *testing.T) {
	tiers := []*membershipmodel.TierEntity{
		{ID: 3, Name: "钻石", SortOrder: 3, ThresholdAmount: 500000},
	}
	def := &membershipmodel.TierEntity{ID: 1, Name: "普通", IsDefault: true}
	if got := resolveSpendTier(tiers, def, 10); got == nil || got.ID != def.ID {
		t.Errorf("达不到门槛时应回退默认等级，实际 %+v", got)
	}
	if got := resolveSpendTier(tiers, def, 600000); got == nil || got.ID != 3 {
		t.Errorf("达到门槛时不该回退，实际 %+v", got)
	}
}

// TestPickDefaultTier 默认等级要从清单里按 is_default 挑出来（不是挑第一个）。
func TestPickDefaultTier(t *testing.T) {
	if got := pickDefaultTier(tiersFixture()); got == nil || got.Name != "普通" {
		t.Errorf("pickDefaultTier = %+v", got)
	}
	if got := pickDefaultTier([]*membershipmodel.TierEntity{{ID: 2, Name: "黄金"}}); got != nil {
		t.Errorf("没有默认等级时应返回 nil，实际 %+v", got)
	}
}

// TestNormalizeEntitlements 权益归一的三种结局：合法、越界、未知 kind。
func TestNormalizeEntitlements(t *testing.T) {
	t.Run("合法并按 enums 顺序输出", func(t *testing.T) {
		rows, err := normalizeEntitlements([]membershipdto.EntitlementReq{
			{Kind: membershipenums.KindDiscount, ValueInt: 20},
			{Kind: membershipenums.KindFreeShipping, ValueInt: 1},
		})
		if err != nil {
			t.Fatalf("不应报错: %v", err)
		}
		if len(rows) != 2 || rows[0].Kind != membershipenums.KindFreeShipping ||
			rows[1].Kind != membershipenums.KindDiscount {
			t.Fatalf("落库顺序应固定：%+v", rows)
		}
	})

	t.Run("同 kind 重复时后一条覆盖前一条", func(t *testing.T) {
		rows, err := normalizeEntitlements([]membershipdto.EntitlementReq{
			{Kind: membershipenums.KindDiscount, ValueInt: 10},
			{Kind: membershipenums.KindDiscount, ValueInt: 30},
		})
		if err != nil {
			t.Fatalf("不应报错: %v", err)
		}
		if len(rows) != 1 || rows[0].ValueInt != 30 {
			t.Fatalf("后一条应覆盖前一条：%+v", rows)
		}
	})

	t.Run("越界一律拒绝", func(t *testing.T) {
		cases := []membershipdto.EntitlementReq{
			{Kind: membershipenums.KindDiscount, ValueInt: 0},
			{Kind: membershipenums.KindDiscount, ValueInt: -20},
			{Kind: membershipenums.KindDiscount, ValueInt: 101},
			{Kind: membershipenums.KindFreeShipping, ValueInt: 2},
			{Kind: membershipenums.KindFreeShipping, ValueInt: -1},
		}
		for _, item := range cases {
			if _, err := normalizeEntitlements([]membershipdto.EntitlementReq{item}); err == nil {
				t.Errorf("%+v 应被拒绝（负折扣会算成加价、超过 100 会让应付为负）", item)
			} else if err.Error() != membershipenums.ErrEntitlementValue {
				t.Errorf("越界应返回 ErrEntitlementValue，实际 %v", err)
			}
		}
	})

	t.Run("未知 kind 拒绝", func(t *testing.T) {
		_, err := normalizeEntitlements([]membershipdto.EntitlementReq{{Kind: "cashback", ValueInt: 1}})
		if err == nil || err.Error() != membershipenums.ErrEntitlementKind {
			t.Errorf("未知 kind 应返回 ErrEntitlementKind，实际 %v", err)
		}
	})

	t.Run("空清单合法", func(t *testing.T) {
		rows, err := normalizeEntitlements(nil)
		if err != nil || len(rows) != 0 {
			t.Errorf("空清单应合法且为空：%v %v", rows, err)
		}
	})
}

// TestParameterValidation 必填与长度的校验口径。
func TestParameterValidation(t *testing.T) {
	if err := requireProject("  "); err == nil || err.Error() != membershipenums.ErrProjectRequired {
		t.Errorf("空工程应报 ErrProjectRequired，实际 %v", err)
	}
	if err := requireUser(0); err == nil || err.Error() != membershipenums.ErrUserRequired {
		t.Errorf("user_id=0 应报 ErrUserRequired，实际 %v", err)
	}
	if err := requireTier(0); err == nil {
		t.Errorf("tier_id=0 应报错")
	}
	if err := validateThreshold(-1); err == nil || err.Error() != membershipenums.ErrThresholdInvalid {
		t.Errorf("负门槛应报 ErrThresholdInvalid，实际 %v", err)
	}
	if _, err := normalizeTierName("   "); err == nil {
		t.Errorf("空名称应报错")
	}
	name, err := normalizeTierName("  白银会员  ")
	if err != nil || name != "白银会员" {
		t.Errorf("名称应去首尾空白：%q %v", name, err)
	}
	long := strings.Repeat("会", tierNameMaxLen+1)
	if _, err := normalizeTierName(long); err == nil {
		t.Errorf("超长名称应报错")
	}
}

// TestNormalizePage 分页参数收敛（超过上限即截，而不是让请求方决定查询规模）。
func TestNormalizePage(t *testing.T) {
	cases := []struct {
		page, size, wantPage, wantSize int
	}{
		{0, 0, 1, defaultPageSize},
		{-3, -1, 1, defaultPageSize},
		{2, 50, 2, 50},
		{1, maxPageSize + 500, 1, maxPageSize},
	}
	for _, tc := range cases {
		page, size := normalizePage(tc.page, tc.size)
		if page != tc.wantPage || size != tc.wantSize {
			t.Errorf("normalizePage(%d,%d) = %d,%d，期望 %d,%d",
				tc.page, tc.size, page, size, tc.wantPage, tc.wantSize)
		}
	}
}

// TestUniqueViolationOnDistinguishesIndexes 三条唯一索引都要能被分辨。
//
// 判据只看「是唯一冲突」的实现会让运营对着一个约束名猜自己撞了哪一条 ——
// 而三条索引的处置方式完全不同（改名 / 改门槛 / 换默认等级）。
func TestUniqueViolationOnDistinguishesIndexes(t *testing.T) {
	nameErr := errors.New(`ERROR: duplicate key value violates unique constraint "uq_membership_tiers_name" (SQLSTATE 23505)`)
	thresholdErr := errors.New(`ERROR: duplicate key value violates unique constraint "uq_membership_tiers_threshold" (SQLSTATE 23505)`)
	defaultErr := errors.New(`ERROR: duplicate key value violates unique constraint "uq_membership_tiers_default" (SQLSTATE 23505)`)

	if !uniqueViolationOn(nameErr, indexTierName) {
		t.Errorf("名字索引未命中")
	}
	if uniqueViolationOn(nameErr, indexTierThreshold) {
		t.Errorf("名字冲突不该命中门槛索引")
	}
	if !uniqueViolationOn(thresholdErr, indexTierThreshold) {
		t.Errorf("门槛索引未命中")
	}
	if !uniqueViolationOn(defaultErr, indexTierDefault) {
		t.Errorf("默认索引未命中")
	}
	// 非唯一冲突不命中（否则普通错误会被映射成业务提示）。
	if uniqueViolationOn(errors.New("pq: relation does not exist"), indexTierName) {
		t.Errorf("非唯一冲突不该命中")
	}
}

// TestMapTierConflictMapsIndexNames 索引名 → 业务错误的映射（service 的兜底路径）。
func TestMapTierConflictMapsIndexNames(t *testing.T) {
	svc := &Service{}
	cases := []struct {
		raw  string
		want string
	}{
		{`duplicate key value violates unique constraint "uq_membership_tiers_name" (SQLSTATE 23505)`, membershipenums.ErrTierNameTaken},
		{`duplicate key value violates unique constraint "uq_membership_tiers_threshold" (SQLSTATE 23505)`, membershipenums.ErrThresholdTaken},
		{`duplicate key value violates unique constraint "uq_membership_tiers_default" (SQLSTATE 23505)`, membershipenums.ErrDefaultTierExists},
	}
	for _, tc := range cases {
		got := svc.mapTierConflict(errors.New(tc.raw))
		if got == nil || got.Error() != tc.want {
			t.Errorf("%s → %v，期望 %s", tc.raw, got, tc.want)
		}
	}
	// 认不出的错误原样返回（由归口分支处理）。
	raw := errors.New("pq: connection refused")
	if got := svc.mapTierConflict(raw); got != raw {
		t.Errorf("不可识别的错误应原样返回，实际 %v", got)
	}
	if got := svc.mapTierConflict(nil); got != nil {
		t.Errorf("nil 应返回 nil")
	}
}

// TestFacingTextThreeForms 契约出口的三种形态（消费方唯一的文案出口）。
func TestFacingTextThreeForms(t *testing.T) {
	svc := &Service{}
	// 未命中 → 归口文案（原文只进日志）。
	got := svc.FacingText("zh-CN", errors.New(`pq: relation "membership_tiers" does not exist`))
	if got == "" || strings.Contains(got, "membership_tiers") {
		t.Errorf("未命中应给归口文案且不含库原文，实际 %q", got)
	}
	// 命中（裸 key 形态）。
	if got := svc.FacingText("zh-CN", errors.New(membershipenums.ErrTierNameTaken)); got == "" {
		t.Errorf("命中应给文案")
	}
	// 命中（带定位信息形态）：定位信息必须原样保留 —— 它是给操作者的数据。
	detailed := svc.FacingText("en-US", errors.New(
		membershipenums.WithDetail(membershipenums.ErrDefaultTierMissing, "p-1")))
	if !strings.Contains(detailed, "p-1") {
		t.Errorf("定位信息应保留，实际 %q", detailed)
	}
	if !strings.Contains(detailed, ": ") {
		t.Errorf("非中文语言应用半角分隔符，实际 %q", detailed)
	}
	// nil → 空串（调用方不必先判空）。
	if got := svc.FacingText("zh-CN", nil); got != "" {
		t.Errorf("nil 应返回空串，实际 %q", got)
	}
}

// TestTierNotFoundCarriesTierID 不存在错误要带可定位的数据。
func TestTierNotFoundCarriesTierID(t *testing.T) {
	err := tierNotFound(42)
	if err == nil || !strings.Contains(err.Error(), "42") {
		t.Fatalf("不存在错误应带 tier_id，实际 %v", err)
	}
}

// TestRecalcReadyReflectsPort 端口未注入时 RecalcReady 为假（页面据此显示「尚未启用」）。
func TestRecalcReadyReflectsPort(t *testing.T) {
	svc := &Service{}
	if svc.RecalcReady() {
		t.Errorf("未注入端口时不该报告可用")
	}
	var nilSvc *Service
	if nilSvc.RecalcReady() {
		t.Errorf("nil service 不该报告可用")
	}
}

// TestRecalcProjectWithoutPortFailsExplicitly 端口未注入时重算**显式失败**，
// 而不是返回一个 Scanned = 0 的「成功」结果 —— 后者看起来像「大家都没消费」。
func TestRecalcProjectWithoutPortFailsExplicitly(t *testing.T) {
	svc := &Service{}
	_, err := svc.RecalcProject(nil, &membershipdto.RecalcProjectReq{ProjectID: "p-1"})
	if err == nil || err.Error() != membershipenums.ErrRecalcUnavailable {
		t.Fatalf("未注入端口应报 ErrRecalcUnavailable，实际 %v", err)
	}
}
