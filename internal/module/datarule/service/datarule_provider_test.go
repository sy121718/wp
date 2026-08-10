package dataruleservice

import (
	"context"
	"testing"

	datamodel "go_wp/internal/module/datarule/model"
	deptmodel "go_wp/internal/module/dept/model"
	deptservice "go_wp/internal/module/dept/service"
	rolemodel "go_wp/internal/module/role/model"
	roleservice "go_wp/internal/module/role/service"
	datarulepkg "go_wp/pkg/datarule"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestGetRulesResolvesEnabledRoleCodesToIDs(t *testing.T) {
	t.Parallel()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败：%v", err)
	}
	if err = db.AutoMigrate(
		&rolemodel.RoleEntity{},
		&deptmodel.DeptEntity{},
		&datamodel.SysRuleEntity{},
		&datamodel.SysRuleAssignmentEntity{},
	); err != nil {
		t.Fatalf("创建测试表失败：%v", err)
	}

	roles := []rolemodel.RoleEntity{
		{ID: 11, RoleCode: "editor", RoleName: "编辑", Status: rolemodel.RoleStatusEnabled},
		{ID: 12, RoleCode: "disabled", RoleName: "禁用角色", Status: rolemodel.RoleStatusDisabled},
	}
	if err = db.Create(&roles).Error; err != nil {
		t.Fatalf("写入测试角色失败：%v", err)
	}
	if err = db.Model(&rolemodel.RoleEntity{}).Where("id = ?", 12).Update("status", rolemodel.RoleStatusDisabled).Error; err != nil {
		t.Fatalf("设置禁用角色失败：%v", err)
	}

	departments := []deptmodel.DeptEntity{
		{ID: 31, ParentID: 0, Ancestors: "0", DeptName: "总部", DeptCode: "root", Status: deptmodel.DeptStatusEnabled},
		{ID: 32, ParentID: 31, Ancestors: "0,31", DeptName: "技术部", DeptCode: "tech", Status: deptmodel.DeptStatusEnabled},
	}
	if err = db.Session(&gorm.Session{SkipHooks: true}).Create(&departments).Error; err != nil {
		t.Fatalf("写入测试部门失败：%v", err)
	}

	rules := []datamodel.SysRuleEntity{
		{ID: 21, RuleName: "角色规则", Domain: "ADMIN", Status: datamodel.RuleStatusEnabled, Config: `{"omit_fields":["email"]}`},
		{ID: 22, RuleName: "禁用角色规则", Domain: "ADMIN", Status: datamodel.RuleStatusEnabled, Config: `{"omit_fields":["phone"]}`},
		{ID: 23, RuleName: "用户规则", Domain: "ADMIN", Status: datamodel.RuleStatusEnabled, Config: `{"omit_fields":["username"]}`},
		{ID: 24, RuleName: "直属部门规则", Domain: "ADMIN", Status: datamodel.RuleStatusEnabled, Config: `{"omit_fields":["status"]}`},
		{ID: 25, RuleName: "上级部门继承规则", Domain: "ADMIN", Status: datamodel.RuleStatusEnabled, Config: `{"omit_fields":["dept_id"]}`},
		{ID: 26, RuleName: "上级部门本级规则", Domain: "ADMIN", Status: datamodel.RuleStatusEnabled, Config: `{"omit_fields":["remark"]}`},
	}
	if err = db.Create(&rules).Error; err != nil {
		t.Fatalf("写入测试规则失败：%v", err)
	}

	assignments := []datamodel.SysRuleAssignmentEntity{
		{RuleID: 21, TargetType: datamodel.AssignmentTargetTypeRole, TargetID: 11},
		{RuleID: 22, TargetType: datamodel.AssignmentTargetTypeRole, TargetID: 12},
		{RuleID: 23, TargetType: datamodel.AssignmentTargetTypeUser, TargetID: 99},
		{RuleID: 23, TargetType: datamodel.AssignmentTargetTypeDept, TargetID: 32, TargetScope: datamodel.AssignmentTargetScopeSelf},
		{RuleID: 24, TargetType: datamodel.AssignmentTargetTypeDept, TargetID: 32, TargetScope: datamodel.AssignmentTargetScopeSelf},
		{RuleID: 25, TargetType: datamodel.AssignmentTargetTypeDept, TargetID: 31, TargetScope: datamodel.AssignmentTargetScopeSelfAndChildren},
		{RuleID: 26, TargetType: datamodel.AssignmentTargetTypeDept, TargetID: 31, TargetScope: datamodel.AssignmentTargetScopeSelf},
	}
	if err = db.Create(&assignments).Error; err != nil {
		t.Fatalf("写入测试分配失败：%v", err)
	}

	roleSvc := roleservice.NewService(rolemodel.NewRoleModel(db), nil, nil)
	deptSvc := deptservice.NewService(deptmodel.NewDeptModel(db), nil)
	service := NewService(
		datamodel.NewSysRuleModel(db),
		datamodel.NewSysRuleAssignmentModel(db),
		roleSvc,
		deptSvc,
	)

	configs, err := service.GetRules(context.Background(), &datarulepkg.UserContext{
		UserID: 99,
		DeptID: 32,
		Roles:  []string{"editor", "disabled"},
	}, "ADMIN")
	if err != nil {
		t.Fatalf("查询规则失败：%v", err)
	}
	if len(configs) != 4 {
		t.Fatalf("应命中角色、用户、直属部门和继承部门规则，实际命中 %d 条", len(configs))
	}

	fields := make(map[string]bool, len(configs))
	for _, config := range configs {
		for _, field := range config.OmitFields {
			fields[field] = true
		}
	}
	if !fields["email"] || !fields["username"] || !fields["status"] || !fields["dept_id"] {
		t.Fatalf("缺少预期交集规则：%v", fields)
	}
	if fields["phone"] {
		t.Fatalf("禁用角色规则不应命中：%v", fields)
	}
	if fields["remark"] {
		t.Fatalf("上级部门仅本级规则不应作用于子部门：%v", fields)
	}
}
