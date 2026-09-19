// tx_rollback_datarule_test.go — RuleDelete 的事务回滚证明
// （2026-09-19 admin 收口批第二部分，接在 tx_rollback_test.go 与 tx_rollback_delete_test.go 之后）。
//
// 复现的形态：RuleDelete 里「sys_rule_assignment 的分配行」与「sys_rule 的规则行」是两处持久化写。
// 修复前两步各自提交 —— 分配行删成功、规则行删除失败，就留下「规则还在、分配一条不剩」：
// 该规则对任何角色/用户/部门都不再生效（数据权限静默放开），而报错只回给这一次请求，
// 列表页上完全看不出异常（下一次打开编辑页才会发现授权没了）。
//
// 故障注入走数据库触发器（同 tx_rollback_test.go 的说明：走完全真实的
// service → model → gorm → PG 路径，不在生产代码上留测试开关）。
//
// 两处写的先后顺序是「先删分配、后删规则」，所以强断言在「第二步失败」这一侧
// （分配行已经在本事务里删掉了，必须随事务一起回来）；反向那一侧也钉一条，
// 免得将来谁调换顺序后，唯一在验证回滚的用例悄悄失效。
package unit

import (
	"context"
	"fmt"
	"testing"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	adminmodel "go_wp/internal/module/admin/model"
)

// countRuleAssignments 统计某条规则名下的分配行数。
func countRuleAssignments(t *testing.T, e *env, ruleID uint64) int64 {
	t.Helper()
	var n int64
	if err := e.db.Model(&adminmodel.SysRuleAssignmentEntity{}).
		Where("rule_id = ?", ruleID).Count(&n).Error; err != nil {
		t.Fatalf("统计规则分配行失败: %v", err)
	}
	return n
}

// countRules 统计规则行数。
func countRules(t *testing.T, e *env, ruleID uint64) int64 {
	t.Helper()
	var n int64
	if err := e.db.Model(&adminmodel.SysRuleEntity{}).Where("id = ?", ruleID).Count(&n).Error; err != nil {
		t.Fatalf("统计规则行失败: %v", err)
	}
	return n
}

// seedRuleAssignments 给规则挂两条真实分配（用户 + 角色），返回规则 ID。
func seedRuleAssignments(t *testing.T, e *env, name string) uint64 {
	t.Helper()
	ctx := context.Background()
	ruleID := ruleCreate(t, e, name, "ORDER", 1, admindto.RuleConfigDTO{})
	err := e.svc.RuleAssignmentSave(ctx, &admindto.RuleAssignmentSaveReq{
		RuleID: ruleID,
		Assignments: []admindto.RuleAssignmentItem{
			{TargetType: adminmodel.AssignmentTargetTypeUser, TargetID: 42},
			{TargetType: adminmodel.AssignmentTargetTypeRole, TargetID: 7},
		},
	})
	wantErr(t, err, "")
	if got := countRuleAssignments(t, e, ruleID); got != 2 {
		t.Fatalf("前置条件失败：应有 2 条分配，实际 %d", got)
	}
	return ruleID
}

// TestRuleDeleteRuleFailureRollsBackAssignments 规则行删除失败 → 已删的分配行必须回来。
//
// 这是本批的核心断言：修复前分配行是**独立提交**的，第二步（删 sys_rule）失败后库里就只剩
// 「规则在、授权没了」—— 规则列表页照常显示这条规则，但它对谁都不生效。
func TestRuleDeleteRuleFailureRollsBackAssignments(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()
	ruleID := seedRuleAssignments(t, e, "回滚用例规则_"+uniq(""))

	// 故障注入：删 sys_rule 该行时抛异常（分配行的删除已经发生在这个事务里）
	installFailTrigger(t, e, "sys_rule", "DELETE", fmt.Sprintf("OLD.id = %d", ruleID))

	err := e.svc.RuleDelete(ctx, &admindto.RuleDeleteReq{IDs: []uint64{ruleID}})
	if err == nil {
		t.Fatalf("规则行删除失败必须把错误回给调用方（不允许静默吞掉）")
	}

	if n := countRules(t, e, ruleID); n != 1 {
		t.Fatalf("规则行删除失败时规则应保持原样: count=%d", n)
	}
	if n := countRuleAssignments(t, e, ruleID); n != 2 {
		t.Fatalf("规则行删除失败时分配行应随事务回滚（旧实现的半截状态就在这）: got=%d want=2", n)
	}
}

// TestRuleDeleteAssignmentFailureRollsBackRule 分配行删除失败 → 规则行不删。
//
// 顺序一旦被调换，这条就是真正在验证回滚的那一条；两条都留。
func TestRuleDeleteAssignmentFailureRollsBackRule(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()
	ruleID := seedRuleAssignments(t, e, "回滚用例规则2_"+uniq(""))

	// 故障注入：删该规则的分配行时抛异常
	installFailTrigger(t, e, "sys_rule_assignment", "DELETE", fmt.Sprintf("OLD.rule_id = %d", ruleID))

	err := e.svc.RuleDelete(ctx, &admindto.RuleDeleteReq{IDs: []uint64{ruleID}})
	if err == nil {
		t.Fatalf("分配行删除失败必须把错误回给调用方")
	}

	if n := countRules(t, e, ruleID); n != 1 {
		t.Fatalf("分配行删除失败时规则应保持原样: count=%d", n)
	}
	if n := countRuleAssignments(t, e, ruleID); n != 2 {
		t.Fatalf("分配行删除失败时分配行应一行不少: got=%d want=2", n)
	}
}

// TestRuleDeleteMissingRuleReportsNotFound 删不存在的规则必须报「规则不存在」，
// 而不是静默成功 —— 行锁复核（LockByIDsTx 锁到的行数 vs 请求的 id 数）就是为了这条。
// 旧实现对此一律返回成功，调用方无法区分「删掉了」与「本来就没有」。
func TestRuleDeleteMissingRuleReportsNotFound(t *testing.T) {
	e := setupEnv(t)
	err := e.svc.RuleDelete(context.Background(), &admindto.RuleDeleteReq{IDs: []uint64{987654321}})
	wantErr(t, err, adminenums.ErrRuleNotFound)
}

// TestRuleDeleteDedupesIDs 重复 id 不应被行锁复核误判成「有行不存在」。
// 缺了 uniqueRuleIDs 时，LockByIDsTx 锁到的是**去重后**的行数，复核会拿它和请求长度比，
// 于是一次重复提交就被整批拒绝并报 ErrRuleNotFound。
func TestRuleDeleteDedupesIDs(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()
	ruleID := seedRuleAssignments(t, e, "去重用例规则_"+uniq(""))

	wantErr(t, e.svc.RuleDelete(ctx, &admindto.RuleDeleteReq{IDs: []uint64{ruleID, ruleID}}), "")

	if n := countRules(t, e, ruleID); n != 0 {
		t.Fatalf("规则行应已删除: count=%d", n)
	}
	if n := countRuleAssignments(t, e, ruleID); n != 0 {
		t.Fatalf("分配行应随规则一并删除: got=%d", n)
	}
}
