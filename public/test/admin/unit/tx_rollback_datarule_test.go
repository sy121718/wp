package unit

import (
	"context"
	"fmt"
	"testing"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	adminmodel "go_wp/internal/module/admin/model"
)

func countRuleAssignments(t *testing.T, e *env, ruleID uint64) int64 {
	t.Helper()
	var n int64
	if err := e.db.Model(&adminmodel.SysRuleAssignmentEntity{}).
		Where("rule_id = ?", ruleID).Count(&n).Error; err != nil {
		t.Fatalf("统计规则分配行失败: %v", err)
	}
	return n
}

func countRules(t *testing.T, e *env, ruleID uint64) int64 {
	t.Helper()
	var n int64
	if err := e.db.Model(&adminmodel.SysRuleEntity{}).Where("id = ?", ruleID).Count(&n).Error; err != nil {
		t.Fatalf("统计规则行失败: %v", err)
	}
	return n
}

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

func TestRuleDeleteRuleFailureRollsBackAssignments(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()
	ruleID := seedRuleAssignments(t, e, "回滚用例规则_"+uniq(""))

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

func TestRuleDeleteAssignmentFailureRollsBackRule(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()
	ruleID := seedRuleAssignments(t, e, "回滚用例规则2_"+uniq(""))

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

func TestRuleDeleteMissingRuleReportsNotFound(t *testing.T) {
	e := setupEnv(t)
	err := e.svc.RuleDelete(context.Background(), &admindto.RuleDeleteReq{IDs: []uint64{987654321}})
	wantErr(t, err, adminenums.ErrRuleNotFound)
}

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
