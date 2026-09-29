package membershipmodel

// membership_assignment_model_test.go — 归属来源常量与 enums 的对账（BIZ-3）。
//
// model 层不 import enums（依赖方向是 model → 无、enums → 对外文案），
// 两处各写一份「auto / manual」的字符串。这条测试把两份钉在一起：
// 值漂移时的表现是**静默**的 —— 页面显示「自动重算」而库里存的是别的值，
// 或者 model 的 SQL 守卫（WHERE source <> 'manual'）再也匹配不上：
// 后者会直接破坏「手工指定不被自动重算覆盖」这条不变量，且没有任何报错。
//
// 纯字符串对账，不需要数据库；迁移 462 的 ck_membership_assignments_source 是第三处，
// 它由迁移测试（psql 事务回滚校验）覆盖。

import (
	"testing"

	membershipenums "go_wp/internal/module/membership/enums"
)

// TestSourceConstantsMatchEnums 两个来源常量在两处必须逐字相同。
func TestSourceConstantsMatchEnums(t *testing.T) {
	if SourceAuto != membershipenums.SourceAuto {
		t.Errorf("auto 常量漂移：model=%q enums=%q", SourceAuto, membershipenums.SourceAuto)
	}
	if SourceManual != membershipenums.SourceManual {
		t.Errorf("manual 常量漂移：model=%q enums=%q", SourceManual, membershipenums.SourceManual)
	}
	if SourceManual != "manual" {
		t.Errorf("SQL 守卫里的字面量 'manual' 与常量必须一致，实际 %q", SourceManual)
	}
}

// TestAssignmentTableName 表名必须与迁移 462 逐字一致（写错会得到「表不存在」的 42P01，
// 而它出现在页面上就是一句通用提示）。
func TestAssignmentTableName(t *testing.T) {
	if got := (AssignmentEntity{}).TableName(); got != "membership_assignments" {
		t.Errorf("TableName = %q", got)
	}
	if got := (TierEntity{}).TableName(); got != "membership_tiers" {
		t.Errorf("TableName = %q", got)
	}
	if got := (EntitlementEntity{}).TableName(); got != "membership_entitlements" {
		t.Errorf("TableName = %q", got)
	}
}
