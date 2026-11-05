package membershipservice

import (
	"errors"
	"strings"
	"testing"

	membershipdto "go_wp/internal/module/membership/dto"
	membershipenums "go_wp/internal/module/membership/enums"
	membershipmodel "go_wp/internal/module/membership/model"
	"go_wp/pkg/facing"
)

func tiersFixture() []*membershipmodel.TierEntity {
	return []*membershipmodel.TierEntity{
		{ID: 3, Name: "钻石", SortOrder: 3, ThresholdAmount: 500000},
		{ID: 2, Name: "黄金", SortOrder: 2, ThresholdAmount: 100000},
		{ID: 1, Name: "普通", SortOrder: 0, ThresholdAmount: 0, IsDefault: true},
	}
}

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

func TestSelectTierBySpendSortOrderBeatsThreshold(t *testing.T) {
	tiers := []*membershipmodel.TierEntity{
		{ID: 9, Name: "钻石", SortOrder: 9, ThresholdAmount: 1000},
		{ID: 2, Name: "黄金", SortOrder: 2, ThresholdAmount: 100000},
	}
	if got := selectTierBySpend(tiers, 5000); got == nil || got.Name != "钻石" {
		t.Errorf("排序在前的档应优先命中，实际 %+v", got)
	}
}

func TestSelectTierBySpendReturnsNilWhenNothingMatches(t *testing.T) {
	tiers := []*membershipmodel.TierEntity{
		{ID: 3, Name: "钻石", SortOrder: 3, ThresholdAmount: 500000},
		{ID: 2, Name: "黄金", SortOrder: 2, ThresholdAmount: 100000},
	}
	if got := selectTierBySpend(tiers, 10); got != nil {
		t.Errorf("没有满足门槛的档时应返回 nil，实际 %+v", got)
	}
}

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

func TestPickDefaultTier(t *testing.T) {
	if got := pickDefaultTier(tiersFixture()); got == nil || got.Name != "普通" {
		t.Errorf("pickDefaultTier = %+v", got)
	}
	if got := pickDefaultTier([]*membershipmodel.TierEntity{{ID: 2, Name: "黄金"}}); got != nil {
		t.Errorf("没有默认等级时应返回 nil，实际 %+v", got)
	}
}

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

	if uniqueViolationOn(errors.New("pq: relation does not exist"), indexTierName) {
		t.Errorf("非唯一冲突不该命中")
	}
}

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

	raw := errors.New("pq: connection refused")
	if got := svc.mapTierConflict(raw); got != raw {
		t.Errorf("不可识别的错误应原样返回，实际 %v", got)
	}
	if got := svc.mapTierConflict(nil); got != nil {
		t.Errorf("nil 应返回 nil")
	}
}

func TestErrorFactsAreDeclaredAtSource(t *testing.T) {
	fact, ok := facing.FactOf(membershipenums.Err(membershipenums.ErrTierNameTaken))
	// 声明处只给 key；中文归词库，取词发生在出口。
	if !ok || !fact.Business || fact.Key != membershipenums.ErrTierNameTaken || fact.Fallback != "" {
		t.Fatalf("业务错误应给出 key、不带成品兜底，实际 %+v ok=%v", fact, ok)
	}
	detailed, ok := facing.FactOf(membershipenums.ErrDetail(membershipenums.ErrDefaultTierMissing, "p-1"))
	if !ok || detailed.Tail != "p-1" {
		t.Fatalf("明细应作为 Tail 带出，实际 %+v", detailed)
	}
	if _, ok := facing.FactOf(errors.New(`pq: relation "membership_tiers" does not exist`)); ok {
		t.Fatal("内部错误不该给出业务事实")
	}
}

func TestTierNotFoundCarriesTierID(t *testing.T) {
	err := tierNotFound(42)
	if err == nil || !strings.Contains(err.Error(), "42") {
		t.Fatalf("不存在错误应带 tier_id，实际 %v", err)
	}
}

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

func TestRecalcProjectWithoutPortFailsExplicitly(t *testing.T) {
	svc := &Service{}
	_, err := svc.RecalcProject(nil, &membershipdto.RecalcProjectReq{ProjectID: "p-1"})
	if err == nil || err.Error() != membershipenums.ErrRecalcUnavailable {
		t.Fatalf("未注入端口应报 ErrRecalcUnavailable，实际 %v", err)
	}
}
