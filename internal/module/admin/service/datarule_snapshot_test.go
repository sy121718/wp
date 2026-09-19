package adminservice

// datarule_snapshot_test.go —— 数据权限快照的模块内单测（不碰数据库）。
//
// 覆盖四件「错了会静默出错」的事：
//  1. 语义等价：内存匹配与改造前 SQL 的四条目标谓词（datarule_provider.go:61-79）给出相同规则集合；
//  2. fail-closed：快照从未加载成功时 GetRules 必须返回错误，绝不返回空规则集合；
//  3. 加载失败保留旧快照：重建失败不得用空快照覆盖已有的成功快照；
//  4. 并发安全：并发读 + 并发重载（go test -race）；
//  5. 写完立即生效：重载后新规则立刻出现在 GetRules 结果里。
//
// 快照与读路径都不依赖 DB，因此这里用 Service 的最小构造（只填 ruleSnapshot / ruleSnapshotLoader），
// 真库路径（加载 SQL、写路径自动重载、查询期零 DB）由 public/test/admin/unit 的 DB 用例覆盖。

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

// dataruleTestSnapshot 构造与测试 fixture 对应的一份快照。
//
// fixture（与 TestMatchAssignmentRuleIDsEquivalentToLegacySQL 的表逐行对应）：
//
//	ORDER 域规则：1 分配给用户 100；2 分配给角色 7（editor）；3 分配给部门 10 scope=SELF；
//	             4 分配给部门 5 scope=SELF_AND_CHILDREN；5 分配给部门 10 scope=SELF_AND_CHILDREN
//	ADMIN 域规则：6 分配给用户 100（用于验证域隔离）
//	启用角色：editor=7、viewer=8
//	部门：5 是根；10 的祖先链是 [5]；20 的祖先链是 [5,10]（10 的子部门）
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

// newDataRuleTestService 构造只带快照的最小 Service（读路径不需要 model）。
func newDataRuleTestService(snap *dataruleSnapshot) *Service {
	svc := &Service{ruleSnapshot: &dataruleSnapshotStore{}}
	if snap != nil {
		svc.ruleSnapshot.current.Store(snap)
	}
	return svc
}

// TestMatchAssignmentRuleIDsEquivalentToLegacySQL 语义等价：内存匹配算法与改造前的 SQL 谓词
// 给出相同规则集合。
//
// 期望值逐条由改造前 datarule_provider.go:61-79 的四条谓词手推（谓词之间是 OR）：
//
//	① target_type=USER AND target_id=user.UserID                      （:61-62，无条件）
//	② target_type=ROLE AND target_id IN roleIDs（roleIDs 非空时才加）   （:63-66）
//	③ target_type=DEPT AND target_id=user.DeptID（DeptID 非 0 时才加）  （:67-70）
//	④ target_type=DEPT AND target_id IN 祖先 AND target_scope=SELF_AND_CHILDREN（祖先非空时才加）（:71-79）
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

// TestGetRulesReadsSnapshotOnly 读路径端到端：GetRules 从快照组装 RuleConfig（按规则配置区分），
// 且声明「无该域规则」「无命中」时不返回错误。
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

	// 未注册域：与快照语义一致地返回空，而不是错误。
	rules, err = svc.GetRules(ctx, &datarule.UserContext{UserID: 100}, "NOT_REGISTERED")
	if err != nil || len(rules) != 0 {
		t.Fatalf("未注册域应返回空且无错误：rules=%v err=%v", rules, err)
	}

	// nil 用户不参与匹配（与改造前一致）。
	rules, err = svc.GetRules(ctx, nil, "ORDER")
	if err != nil || len(rules) != 0 {
		t.Fatalf("nil 用户应返回空且无错误：rules=%v err=%v", rules, err)
	}
}

// TestGetRulesFailClosedWithoutSnapshot 快照加载失败时必须返回错误，绝不返回空规则集合。
//
// 这是本改造的安全红线：返回空规则集合等价于「没有任何限制」，会把数据权限静默关掉；
// 改造前 provider 出错走 db.AddError(err) 让查询失败，同样是 fail-closed，这里保持同一语义。
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
	// 失败后快照仍然为空：下一次命中请求会重试，而不是把「空」记下来当结果。
	if snap := svc.currentDataRuleSnapshot(); snap != nil {
		t.Fatalf("加载失败不应写入任何快照: %+v", snap)
	}
}

// TestConcurrentFirstUseLoadsOnce 并发首次命中只触发一次加载（double-check 的实际效果）。
//
// 没有这层二次检查时，N 个并发首次请求会各自加载一遍（3N 次查询）。加载器里睡一小会儿，
// 把竞态窗口拉开，断言计数恰好为 1。
func TestConcurrentFirstUseLoadsOnce(t *testing.T) {
	t.Parallel()

	svc := newDataRuleTestService(nil)
	var loads atomic.Int64
	svc.ruleSnapshotLoader = func(context.Context) (*dataruleSnapshot, error) {
		loads.Add(1)
		time.Sleep(20 * time.Millisecond) // 拉开竞态窗口：没有 double-check 时这里必然 > 1
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

// TestLoadSnapshotFailureKeepsPreviousSnapshot 加载失败不得清空已有快照。
//
// 场景：先成功加载一份快照（规则命中），随后重建失败（库抖动 / SQL 出错），
// 读路径必须继续按**旧快照**拦截，而不是退化成「没有规则」。
func TestLoadSnapshotFailureKeepsPreviousSnapshot(t *testing.T) {
	t.Parallel()

	svc := newDataRuleTestService(nil)
	svc.ruleSnapshotLoader = func(context.Context) (*dataruleSnapshot, error) {
		return dataruleTestSnapshot(), nil
	}
	if err := svc.LoadDataRuleSnapshot(context.Background()); err != nil {
		t.Fatalf("首次加载应成功: %v", err)
	}

	// 换成必然失败的加载器：重建失败，但旧快照必须保留。
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

// TestReloadTakesEffectImmediately 重载后新规则立刻出现在 GetRules 结果里（不等定时刷新）。
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

	// 库侧变更完成（新规则 + 分配），写路径同步重载后读端立刻可见。
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

// TestSnapshotConcurrentReadAndReload 并发读 + 并发重载必须无数据竞争（go test -race）。
//
// 读端只做 atomic.Pointer.Load（无锁），重建端由 store.mu 串行化并整体 Store 新快照；
// 这个用例的作用是让 -race 真正跑到那条路径上。
func TestSnapshotConcurrentReadAndReload(t *testing.T) {
	t.Parallel()

	svc := newDataRuleTestService(dataruleTestSnapshot())
	var loads atomic.Int64
	svc.ruleSnapshotLoader = func(context.Context) (*dataruleSnapshot, error) {
		loads.Add(1)
		// 每次重建都返回一份内容相同但**新分配**的快照：强制走整体替换，而不是复用同一指针。
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

// sameIDSet 比较两个 id 列表是否为同一集合（顺序无关，忽略空值差异）。
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
