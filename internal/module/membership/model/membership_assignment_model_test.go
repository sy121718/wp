package membershipmodel

import (
	"testing"

	membershipenums "go_wp/internal/module/membership/enums"
)

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
