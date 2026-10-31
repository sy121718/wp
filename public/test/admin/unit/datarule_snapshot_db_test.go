package unit

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

func ruleConfigWithOmit(field string) admindto.RuleConfigDTO {
	return admindto.RuleConfigDTO{OmitFields: []string{field}}
}

func ruleAssignmentForUser(ruleID, userID uint64) *admindto.RuleAssignmentSaveReq {
	return &admindto.RuleAssignmentSaveReq{
		RuleID: ruleID,
		Assignments: []admindto.RuleAssignmentItem{
			{TargetType: adminmodel.AssignmentTargetTypeUser, TargetID: userID},
		},
	}
}

func ruleDeleteReq(ruleID uint64) *admindto.RuleDeleteReq {
	return &admindto.RuleDeleteReq{IDs: []uint64{ruleID}}
}

func dataruleSnapshotTestUser() *datarule.UserContext {
	return &datarule.UserContext{UserID: 100, Roles: []string{}}
}

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

func TestSnapshotLazyLoadOnFirstUse(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

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

	if got := counter.Load(); got != 3 {
		t.Fatalf("首次懒加载应当只跑 3 条加载查询，实际 %d 条", got)
	}
	t.Logf("首次命中请求的快照加载耗时 %v（3 条加载查询；此后每次读取 0 次）", elapsed)

	counter.Store(0)
	if _, err = e.svc.GetRules(ctx, dataruleSnapshotTestUser(), "ORDER"); err != nil {
		t.Fatalf("第二次读取失败: %v", err)
	}
	if got := counter.Load(); got != 0 {
		t.Fatalf("快照就绪后读取不应查库，实际 %d 次", got)
	}
}

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

func TestBusinessQueryTriggersNoExtraDataruleQuery(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

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

func TestRuleWritesTakeEffectImmediately(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

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

	rules, err = e.svc.GetRules(ctx, dataruleSnapshotTestUser(), "ORDER")
	if err != nil {
		t.Fatalf("写后读取失败: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("写后应立即命中 1 条规则，实际 %d: %+v", len(rules), rules)
	}

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
