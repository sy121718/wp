package adminservice

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	adminmodel "go_wp/internal/module/admin/model"
	"go_wp/pkg/datarule"
)

func dataruleTestSnapshot() *dataruleSnapshot {
	return &dataruleSnapshot{
		domains: map[string]*dataruleDomain{
			"ORDER": {
				rules: []dataruleRule{
					{id: 1, config: `{"omit_fields":["f1"]}`},
					{id: 2, config: `{"omit_fields":["f2"]}`},
					{id: 3, config: `{"omit_fields":["f3"]}`},
					{id: 4, config: `{"omit_fields":["f4"]}`},
					{id: 5, config: `{"omit_fields":["f5"]}`},
				},
				assignments: []dataruleAssignment{
					{ruleID: 1, targetType: adminmodel.AssignmentTargetTypeUser, targetID: 100},
					{ruleID: 2, targetType: adminmodel.AssignmentTargetTypeRole, targetID: 7},
					{ruleID: 3, targetType: adminmodel.AssignmentTargetTypeDept, targetID: 10, targetScope: adminmodel.AssignmentTargetScopeSelf},
					{ruleID: 4, targetType: adminmodel.AssignmentTargetTypeDept, targetID: 5, targetScope: adminmodel.AssignmentTargetScopeSelfAndChildren},
					{ruleID: 5, targetType: adminmodel.AssignmentTargetTypeDept, targetID: 10, targetScope: adminmodel.AssignmentTargetScopeSelfAndChildren},
				},
			},
			"ADMIN": {
				rules: []dataruleRule{{id: 6, config: `{"omit_fields":["f6"]}`}},
				assignments: []dataruleAssignment{
					{ruleID: 6, targetType: adminmodel.AssignmentTargetTypeUser, targetID: 100},
				},
			},
		},
		roleIDs:       map[string]uint64{"editor": 7, "viewer": 8},
		deptAncestors: map[uint64][]uint64{10: {5}, 20: {5, 10}},
		deptSubtrees:  map[uint64][]uint64{5: {5, 10, 20}, 10: {10, 20}, 20: {20}},
	}
}

func newDataRuleTestService(snap *dataruleSnapshot) *Service {
	svc := &Service{ruleSnapshot: &dataruleSnapshotStore{}}
	if snap != nil {
		svc.ruleSnapshot.current.Store(snap)
	}
	return svc
}

func TestMatchAssignmentRuleIDsEquivalentToLegacySQL(t *testing.T) {
	t.Parallel()

	snap := dataruleTestSnapshot()

	cases := []struct {
		name   string
		user   *datarule.UserContext
		domain string
		want   []uint64
	}{
		{
			name: "仅用户直接分配", domain: "ORDER",
			user: &datarule.UserContext{UserID: 100},
			want: []uint64{1},
		},
		{
			name: "用户+角色+本部门+上级部门四条谓词同时命中", domain: "ORDER",
			user: &datarule.UserContext{UserID: 100, DeptID: 10, Roles: []string{"editor"}},
			want: []uint64{1, 2, 3, 4, 5},
		},
		{
			name: "本部门 scope=SELF 与 SELF_AND_CHILDREN 都命中，上级部门仅 scope=SELF_AND_CHILDREN 命中", domain: "ORDER",
			user: &datarule.UserContext{UserID: 1, DeptID: 10},
			want: []uint64{3, 4, 5},
		},
		{
			name: "子部门命中上级部门的 SELF_AND_CHILDREN，不命中其 scope=SELF", domain: "ORDER",
			user: &datarule.UserContext{UserID: 1, DeptID: 20},
			want: []uint64{4, 5},
		},
		{
			name: "角色不在快照里（未启用或不存在）不命中", domain: "ORDER",
			user: &datarule.UserContext{UserID: 1, Roles: []string{"viewer"}},
			want: nil,
		},
		{
			name: "部门不在快照里（无祖先链）只有本部门谓词参与", domain: "ORDER",
			user: &datarule.UserContext{UserID: 1, DeptID: 999},
			want: nil,
		},
		{
			name: "域隔离：ADMIN 域只命中 ADMIN 的规则", domain: "ADMIN",
			user: &datarule.UserContext{UserID: 100},
			want: []uint64{6},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			domainRules := snap.domains[c.domain]
			roleIDs := make([]uint64, 0, len(c.user.Roles))
			for _, code := range c.user.Roles {
				if id, ok := snap.roleIDs[code]; ok {
					roleIDs = append(roleIDs, id)
				}
			}
			got := matchAssignmentRuleIDs(
				domainRules.assignments, c.user.UserID, c.user.DeptID, roleIDs, snap.deptAncestors[c.user.DeptID],
			)
			if !sameIDSet(got, c.want) {
				t.Fatalf("命中规则集合不符：got %v, want %v", got, c.want)
			}
		})
	}
}

func TestGetRulesReadsSnapshotOnly(t *testing.T) {
	t.Parallel()

	svc := newDataRuleTestService(dataruleTestSnapshot())
	ctx := context.Background()

	rules, err := svc.GetRules(ctx, &datarule.UserContext{UserID: 100, DeptID: 10, Roles: []string{"editor"}}, "ORDER")
	if err != nil {
		t.Fatalf("期望成功，实际报错: %v", err)
	}
	if len(rules) != 5 {
		t.Fatalf("应命中 5 条规则，实际 %d: %+v", len(rules), rules)
	}
	omits := make(map[string]bool, len(rules))
	for _, rule := range rules {
		if len(rule.OmitFields) == 1 {
			omits[rule.OmitFields[0]] = true
		}
	}
	for _, want := range []string{"f1", "f2", "f3", "f4", "f5"} {
		if !omits[want] {
			t.Fatalf("规则 %s 未命中，实际命中集合 %v", want, omits)
		}
	}

	rules, err = svc.GetRules(ctx, &datarule.UserContext{UserID: 100}, "NOT_REGISTERED")
	if err != nil || len(rules) != 0 {
		t.Fatalf("未注册域应返回空且无错误：rules=%v err=%v", rules, err)
	}

	rules, err = svc.GetRules(ctx, nil, "ORDER")
	if err != nil || len(rules) != 0 {
		t.Fatalf("nil 用户应返回空且无错误：rules=%v err=%v", rules, err)
	}
}

func TestGetRulesFailClosedWithoutSnapshot(t *testing.T) {
	t.Parallel()

	svc := newDataRuleTestService(nil)
	svc.ruleSnapshotLoader = func(context.Context) (*dataruleSnapshot, error) {
		return nil, errors.New("模拟数据库不可用")
	}
	rules, err := svc.GetRules(context.Background(), &datarule.UserContext{UserID: 100}, "ORDER")
	if err == nil {
		t.Fatal("快照加载失败时必须返回错误，实际返回 nil 错误")
	}
	if len(rules) != 0 {
		t.Fatalf("失败时不应返回任何规则: %+v", rules)
	}

	if snap := svc.currentDataRuleSnapshot(); snap != nil {
		t.Fatalf("加载失败不应写入任何快照: %+v", snap)
	}
}

func TestConcurrentFirstUseLoadsOnce(t *testing.T) {
	t.Parallel()

	svc := newDataRuleTestService(nil)
	var loads atomic.Int64
	svc.ruleSnapshotLoader = func(context.Context) (*dataruleSnapshot, error) {
		loads.Add(1)
		time.Sleep(20 * time.Millisecond)
		return dataruleTestSnapshot(), nil
	}

	const goroutines = 8
	var wg sync.WaitGroup
	errCh := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rules, err := svc.GetRules(context.Background(), &datarule.UserContext{UserID: 100, DeptID: 10, Roles: []string{"editor"}}, "ORDER")
			if err != nil {
				errCh <- err
				return
			}
			if len(rules) != 5 {
				errCh <- errors.New("首次懒加载返回的规则集合不完整")
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("并发首次命中失败: %v", err)
	}
	if got := loads.Load(); got != 1 {
		t.Fatalf("并发首次命中应只加载一次，实际 %d 次", got)
	}
}

func TestLoadSnapshotFailureKeepsPreviousSnapshot(t *testing.T) {
	t.Parallel()

	svc := newDataRuleTestService(nil)
	svc.ruleSnapshotLoader = func(context.Context) (*dataruleSnapshot, error) {
		return dataruleTestSnapshot(), nil
	}
	if err := svc.LoadDataRuleSnapshot(context.Background()); err != nil {
		t.Fatalf("首次加载应成功: %v", err)
	}

	svc.ruleSnapshotLoader = func(context.Context) (*dataruleSnapshot, error) {
		return nil, errors.New("模拟数据库抖动")
	}
	if err := svc.LoadDataRuleSnapshot(context.Background()); err == nil {
		t.Fatal("重建失败应返回错误（由调用方决定要不要记日志）")
	}

	rules, err := svc.GetRules(context.Background(), &datarule.UserContext{UserID: 100}, "ORDER")
	if err != nil {
		t.Fatalf("旧快照应仍然可用，实际报错: %v", err)
	}
	if len(rules) != 1 || len(rules[0].OmitFields) != 1 || rules[0].OmitFields[0] != "f1" {
		t.Fatalf("应继续按旧快照命中规则 1，实际: %+v", rules)
	}
}

func TestReloadTakesEffectImmediately(t *testing.T) {
	t.Parallel()

	svc := newDataRuleTestService(nil)
	svc.ruleSnapshotLoader = func(context.Context) (*dataruleSnapshot, error) {
		return dataruleTestSnapshot(), nil
	}
	if err := svc.LoadDataRuleSnapshot(context.Background()); err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	user := &datarule.UserContext{UserID: 100}
	rules, err := svc.GetRules(context.Background(), user, "ORDER")
	if err != nil || len(rules) != 1 {
		t.Fatalf("加载后应命中 1 条: rules=%+v err=%v", rules, err)
	}

	next := dataruleTestSnapshot()
	next.domains["ORDER"].rules = append(next.domains["ORDER"].rules, dataruleRule{id: 9, config: `{"omit_fields":["f9"]}`})
	next.domains["ORDER"].assignments = append(next.domains["ORDER"].assignments, dataruleAssignment{
		ruleID: 9, targetType: adminmodel.AssignmentTargetTypeUser, targetID: 100,
	})
	svc.ruleSnapshotLoader = func(context.Context) (*dataruleSnapshot, error) { return next, nil }
	if err := svc.LoadDataRuleSnapshot(context.Background()); err != nil {
		t.Fatalf("重载失败: %v", err)
	}

	rules, err = svc.GetRules(context.Background(), user, "ORDER")
	if err != nil {
		t.Fatalf("重载后读取报错: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("重载后应立即看到 2 条规则（不等定时刷新），实际 %d: %+v", len(rules), rules)
	}
}

func TestSnapshotConcurrentReadAndReload(t *testing.T) {
	t.Parallel()

	svc := newDataRuleTestService(dataruleTestSnapshot())
	var loads atomic.Int64
	svc.ruleSnapshotLoader = func(context.Context) (*dataruleSnapshot, error) {
		loads.Add(1)

		return dataruleTestSnapshot(), nil
	}

	ctx := context.Background()
	user := &datarule.UserContext{UserID: 100, DeptID: 10, Roles: []string{"editor"}}

	const (
		readers   = 4
		reloaders = 3
		rounds    = 40
	)
	var wg sync.WaitGroup
	errCh := make(chan error, readers*rounds)

	for i := 0; i < reloaders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < rounds; j++ {
				if err := svc.LoadDataRuleSnapshot(ctx); err != nil {
					errCh <- err
					return
				}
			}
		}()
	}
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < rounds; j++ {
				rules, err := svc.GetRules(ctx, user, "ORDER")
				if err != nil {
					errCh <- err
					return
				}
				if len(rules) != 5 {
					errCh <- errors.New("并发读期间返回的规则集合不完整")
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("并发用例失败: %v", err)
	}
	if loads.Load() == 0 {
		t.Fatal("重载未被触发，用例没有覆盖到重建路径")
	}
}

func sameIDSet(got, want []uint64) bool {
	gotSet := make(map[uint64]struct{}, len(got))
	for _, id := range got {
		gotSet[id] = struct{}{}
	}
	wantSet := make(map[uint64]struct{}, len(want))
	for _, id := range want {
		wantSet[id] = struct{}{}
	}
	if len(gotSet) != len(wantSet) {
		return false
	}
	for id := range wantSet {
		if _, ok := gotSet[id]; !ok {
			return false
		}
	}
	return true
}
