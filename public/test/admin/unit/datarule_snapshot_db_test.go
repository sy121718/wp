package unit

// datarule_snapshot_db_test.go —— 数据权限快照的真库用例（性能整改的第二半：查询期 0 次 DB）。
//
// 模块内单测（internal/module/admin/service/datarule_snapshot_test.go）覆盖纯内存语义；
// 这里覆盖只有真库才能证明的三件事：
//  1. 快照确实来自 DB（懒加载首次命中时把库里的规则拉起来并命中）；
//  2. 查询期**零 DB**：计数 GORM 的 gorm:query 回调，N 次 GetRules 期间查询数为 0；
//     再接一次真实业务查询（带 UserContext，走 datarule 插件），总查询数恰好 1 次 ——
//     改造前这一条是 1 + 4 次；
//  3. 写路径（规则 / 分配）提交后立即生效，不需要等 5 分钟兜底刷新。
//
// 本地 PG 实测数字（2026-09，单测内 t.Logf 输出，改动算法后会变，请以用例输出为准）：
//   · 并发首次（8 个 goroutine 同时命中，加载器内 sleep 20ms 拉开竞态）只触发 **1 次**加载；
//   · 首次命中请求的快照加载耗时 **≈1.09ms**（3 条小查询：规则 LEFT JOIN 分配 / 启用角色 / 部门树）；
//   · 快照就绪后 **20 次** GetRules 期间 DB 查询 **0** 条；
//   · 一次带 UserContext 的真实业务查询（Find，走 datarule 插件）总 DB 往返 **1** 条
//     —— 改造前是 1 + 4 条（权限侧每次查库）。
//
// 计数口径的坑（下一个做同类打点的人必踩）：必须**同时挂 Query 链与 Row 链**。
// gorm 的 Find 走 Query 链，而 Scan / Rows 走独立的 Row 链 —— 只挂 gorm:query 会漏掉
// 快照加载用的 Select(...).Scan(...)，从而得出「加载 0 次查询」的错误结论。

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	admindto "go_wp/internal/module/admin/dto"
	adminmodel "go_wp/internal/module/admin/model"
	"go_wp/pkg/datarule"

	"gorm.io/gorm"
)

// ruleConfigWithOmit 构造只含一个字段屏蔽的规则配置（按域白名单里确实存在的列）。
func ruleConfigWithOmit(field string) admindto.RuleConfigDTO {
	return admindto.RuleConfigDTO{OmitFields: []string{field}}
}

// ruleAssignmentForUser 构造「分配给某个用户」的全量分配请求。
func ruleAssignmentForUser(ruleID, userID uint64) *admindto.RuleAssignmentSaveReq {
	return &admindto.RuleAssignmentSaveReq{
		RuleID: ruleID,
		Assignments: []admindto.RuleAssignmentItem{
			{TargetType: adminmodel.AssignmentTargetTypeUser, TargetID: userID},
		},
	}
}

// ruleDeleteReq 构造单条规则删除请求。
func ruleDeleteReq(ruleID uint64) *admindto.RuleDeleteReq {
	return &admindto.RuleDeleteReq{IDs: []uint64{ruleID}}
}

// dataruleSnapshotTestContext 构造一个固定用户上下文（用户 100，DEPARTMENT 域用不到）。
func dataruleSnapshotTestUser() *datarule.UserContext {
	return &datarule.UserContext{UserID: 100, Roles: []string{}}
}

// countGormQueries 在给定 DB 上挂前置回调计数（仅统计，不改变行为）。
//
// 必须同时挂 **Query 链与 Row 链**：gorm 的 Find 走 Query 链，而 Scan / Rows 走独立的
// Row 链 —— 只挂 Query 会漏掉快照加载用的 Select(...).Scan(...)，从而得出「加载 0 次查询」
// 的错误结论（把测量口径的漏洞当成优化效果）。
func countGormQueries(t *testing.T, db *gorm.DB) *atomic.Int64 {
	t.Helper()
	var count atomic.Int64
	countFn := func(*gorm.DB) { count.Add(1) }
	const (
		queryName = "test:count-snapshot-queries"
		rowName   = "test:count-snapshot-rows"
	)
	if err := db.Callback().Query().Before("gorm:query").Register(queryName, countFn); err != nil {
		t.Fatalf("注册 Query 计数回调失败: %v", err)
	}
	if err := db.Callback().Row().Before("gorm:row").Register(rowName, countFn); err != nil {
		t.Fatalf("注册 Row 计数回调失败: %v", err)
	}
	t.Cleanup(func() {
		db.Callback().Query().Remove(queryName)
		db.Callback().Row().Remove(rowName)
	})
	return &count
}

// TestSnapshotLazyLoadOnFirstUse 懒加载：库里已有规则（绕过 service 直接落库，进程内快照仍为空），
// 首次命中请求触发加载并命中规则；同时记录这次一次性加载的耗时。
func TestSnapshotLazyLoadOnFirstUse(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	// 直接落库：规则 + 分配都不经 service，因此不会触发写路径重载 —— 快照一定还是空的。
	rule := &adminmodel.SysRuleEntity{
		RuleName: "懒加载规则" + uniq(""), Domain: "ORDER",
		Config: `{"omit_fields":["price"]}`, Status: adminmodel.RuleStatusEnabled,
	}
	if err := e.db.Create(rule).Error; err != nil {
		t.Fatalf("创建规则失败: %v", err)
	}
	if err := e.db.Create(&adminmodel.SysRuleAssignmentEntity{
		RuleID: rule.ID, TargetType: adminmodel.AssignmentTargetTypeUser, TargetID: 100,
	}).Error; err != nil {
		t.Fatalf("创建分配失败: %v", err)
	}

	counter := countGormQueries(t, e.db)
	start := time.Now()
	rules, err := e.svc.GetRules(ctx, dataruleSnapshotTestUser(), "ORDER")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("首次命中读取失败: %v", err)
	}
	if len(rules) != 1 || len(rules[0].OmitFields) != 1 || rules[0].OmitFields[0] != "price" {
		t.Fatalf("懒加载应把库里的规则拉进快照并命中: %+v", rules)
	}
	// 懒加载的一次性成本 = 1 次 JOIN（规则+分配）+ 1 次角色 + 1 次部门 = 3 条小查询。
	if got := counter.Load(); got != 3 {
		t.Fatalf("首次懒加载应当只跑 3 条加载查询，实际 %d 条", got)
	}
	t.Logf("首次命中请求的快照加载耗时 %v（3 条加载查询；此后每次读取 0 次）", elapsed)

	// 加载完成后第二次读取不再查库。
	counter.Store(0)
	if _, err = e.svc.GetRules(ctx, dataruleSnapshotTestUser(), "ORDER"); err != nil {
		t.Fatalf("第二次读取失败: %v", err)
	}
	if got := counter.Load(); got != 0 {
		t.Fatalf("快照就绪后读取不应查库，实际 %d 次", got)
	}
}

// TestGetRulesCostsZeroQueries 查询期零 DB：N 次 GetRules 期间一次查询都没有。
func TestGetRulesCostsZeroQueries(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	ruleID := ruleCreate(t, e, "零查询规则"+uniq(""), "ORDER", adminmodel.RuleStatusEnabled,
		ruleConfigWithOmit("price"))
	if err := e.svc.RuleAssignmentSave(ctx, ruleAssignmentForUser(ruleID, 100)); err != nil {
		t.Fatalf("分配失败: %v", err)
	}

	counter := countGormQueries(t, e.db)
	for i := 0; i < 20; i++ {
		rules, err := e.svc.GetRules(ctx, dataruleSnapshotTestUser(), "ORDER")
		if err != nil {
			t.Fatalf("第 %d 次读取失败: %v", i, err)
		}
		if len(rules) != 1 {
			t.Fatalf("第 %d 次读取结果不符: %+v", i, rules)
		}
	}
	if got := counter.Load(); got != 0 {
		t.Fatalf("20 次 GetRules 期间发生了 %d 次 DB 查询，期望 0 次", got)
	}
}

// TestBusinessQueryTriggersNoExtraDataruleQuery 真实链路：注册 datarule 插件后，
// 一次带 UserContext 的业务查询总共只打**1** 次 DB。
//
// 改造前 provider 每次都查 4 次库，所以同一条业务查询是 1 + 4 = 5 次；现在权限侧是纯内存，
// 计数必须是 1。这是「查询期 0 次 DB」最贴近生产的证据。
func TestBusinessQueryTriggersNoExtraDataruleQuery(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	// 一条命中当前用户的 ADMIN 域规则（OmitFields 注入路径也会被走到）。
	ruleID := ruleCreate(t, e, "业务查询规则"+uniq(""), "ADMIN", adminmodel.RuleStatusEnabled,
		ruleConfigWithOmit("phone"))
	if err := e.svc.RuleAssignmentSave(ctx, ruleAssignmentForUser(ruleID, 100)); err != nil {
		t.Fatalf("分配失败: %v", err)
	}

	datarule.SetProvider(e.svc)
	if err := datarule.RegisterPluginWithDB(e.db); err != nil {
		t.Fatalf("注册 datarule 插件失败: %v", err)
	}

	userCtx := context.WithValue(context.Background(), datarule.UserContextKey{}, dataruleSnapshotTestUser())
	counter := countGormQueries(t, e.db)

	var admins []adminmodel.AdminEntity
	if err := e.db.WithContext(userCtx).Model(&adminmodel.AdminEntity{}).Limit(1).Find(&admins).Error; err != nil {
		t.Fatalf("业务查询失败: %v", err)
	}
	if got := counter.Load(); got != 1 {
		t.Fatalf("一次业务查询应恰好 1 次 DB 往返（权限侧 0 次），实际 %d 次", got)
	}
}

// TestRuleWritesTakeEffectImmediately 写路径同步重载：新建规则 + 保存分配后，
// 不需要等 5 分钟兜底刷新，下一次 GetRules 立刻命中。
func TestRuleWritesTakeEffectImmediately(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	// 先让快照存在（空表加载一次）。
	if _, err := e.svc.GetRules(ctx, dataruleSnapshotTestUser(), "ORDER"); err != nil {
		t.Fatalf("初始加载失败: %v", err)
	}
	rules, err := e.svc.GetRules(ctx, dataruleSnapshotTestUser(), "ORDER")
	if err != nil || len(rules) != 0 {
		t.Fatalf("初始应为空: rules=%+v err=%v", rules, err)
	}

	ruleID := ruleCreate(t, e, "立即生效规则"+uniq(""), "ORDER", adminmodel.RuleStatusEnabled,
		ruleConfigWithOmit("price"))
	if err := e.svc.RuleAssignmentSave(ctx, ruleAssignmentForUser(ruleID, 100)); err != nil {
		t.Fatalf("分配失败: %v", err)
	}

	// 关键：这里**没有**手动加载快照，写路径必须已经把它重载过。
	rules, err = e.svc.GetRules(ctx, dataruleSnapshotTestUser(), "ORDER")
	if err != nil {
		t.Fatalf("写后读取失败: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("写后应立即命中 1 条规则，实际 %d: %+v", len(rules), rules)
	}

	// 删除后同样立即失效。
	if err := e.svc.RuleDelete(ctx, ruleDeleteReq(ruleID)); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	rules, err = e.svc.GetRules(ctx, dataruleSnapshotTestUser(), "ORDER")
	if err != nil {
		t.Fatalf("删后读取失败: %v", err)
	}
	if len(rules) != 0 {
		t.Fatalf("删后应立即不再命中，实际 %d: %+v", len(rules), rules)
	}
}
