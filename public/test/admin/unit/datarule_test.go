package unit

import (
	"context"
	"testing"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	adminmodel "go_wp/internal/module/admin/model"
	"go_wp/pkg/datarule"
)

func TestRuleCreateAndDetailRoundTrip(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	ruleID := ruleCreate(t, e, "只看本部门", "ORDER", 1, admindto.RuleConfigDTO{
		OmitFields: []string{"price"},
		ConditionGroups: []admindto.RuleConditionGroupDTO{{
			Logic: "AND",
			Conditions: []admindto.RuleConditionDTO{
				{Field: "dept_id", Op: "EQ", Value: "dept.scope:SELF"},
			},
		}},
	})

	detail, err := e.svc.RuleDetail(ctx, &admindto.RuleDetailReq{ID: ruleID})
	wantErr(t, err, "")
	if detail.RuleName != "只看本部门" || detail.Domain != "ORDER" {
		t.Fatalf("规则详情不符: %+v", detail)
	}
	if len(detail.Config.OmitFields) != 1 || detail.Config.OmitFields[0] != "price" {
		t.Fatalf("Config OmitFields 往返不符: %+v", detail.Config)
	}
	if len(detail.Config.ConditionGroups) != 1 || detail.Config.ConditionGroups[0].Conditions[0].Op != "EQ" {
		t.Fatalf("Config ConditionGroups 往返不符: %+v", detail.Config)
	}
}

func TestRuleDetailNotFound(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	_, err := e.svc.RuleDetail(ctx, &admindto.RuleDetailReq{ID: 888888})
	wantErr(t, err, adminenums.ErrRuleNotFound)
}

func TestRuleUpdateSuccess(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	ruleID := ruleCreate(t, e, "旧规则", "ORDER", 1, admindto.RuleConfigDTO{})
	err := e.svc.RuleUpdate(ctx, &admindto.RuleUpdateReq{
		ID: ruleID, RuleName: "新规则", Domain: "ADMIN",
		Config: admindto.RuleConfigDTO{OmitFields: []string{"phone"}}, Status: 1,
	})
	wantErr(t, err, "")

	detail, err := e.svc.RuleDetail(ctx, &admindto.RuleDetailReq{ID: ruleID})
	wantErr(t, err, "")
	if detail.RuleName != "新规则" || detail.Domain != "ADMIN" || len(detail.Config.OmitFields) != 1 {
		t.Fatalf("更新后不符: %+v", detail)
	}
}

func TestRuleUpdateNotFound(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	err := e.svc.RuleUpdate(ctx, &admindto.RuleUpdateReq{
		ID: 777777, RuleName: "x", Domain: "ORDER", Config: admindto.RuleConfigDTO{}, Status: 1,
	})
	wantErr(t, err, adminenums.ErrRuleNotFound)
}

func TestRuleDeleteCleansAssignments(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	ruleID := ruleCreate(t, e, "待删规则", "ORDER", 1, admindto.RuleConfigDTO{})
	err := e.svc.RuleAssignmentSave(ctx, &admindto.RuleAssignmentSaveReq{
		RuleID: ruleID,
		Assignments: []admindto.RuleAssignmentItem{
			{TargetType: adminmodel.AssignmentTargetTypeUser, TargetID: 42},
		},
	})
	wantErr(t, err, "")

	err = e.svc.RuleDelete(ctx, &admindto.RuleDeleteReq{IDs: []uint64{ruleID}})
	wantErr(t, err, "")

	var ruleCount, assignCount int64
	if err := e.db.Model(&adminmodel.SysRuleEntity{}).Where("id = ?", ruleID).Count(&ruleCount).Error; err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if err := e.db.Model(&adminmodel.SysRuleAssignmentEntity{}).Where("rule_id = ?", ruleID).Count(&assignCount).Error; err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if ruleCount != 0 || assignCount != 0 {
		t.Fatalf("规则与分配记录应一并删除: rule=%d assign=%d", ruleCount, assignCount)
	}
}

func TestRuleListFilters(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	ruleCreate(t, e, "订单规则", "ORDER", 1, admindto.RuleConfigDTO{})
	ruleCreate(t, e, "管理员规则", "ADMIN", 1, admindto.RuleConfigDTO{})

	res, err := e.svc.RuleList(ctx, &admindto.RuleListReq{Domain: "ORDER"})
	wantErr(t, err, "")
	if res.Total != 1 {
		t.Fatalf("domain 筛选不符: total=%d", res.Total)
	}
	res, err = e.svc.RuleList(ctx, &admindto.RuleListReq{})
	wantErr(t, err, "")
	if res.Total != 2 {
		t.Fatalf("全量不符: total=%d", res.Total)
	}
}

func TestRuleAssignmentSaveInvalidTarget(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	ruleID := ruleCreate(t, e, "规则", "ORDER", 1, admindto.RuleConfigDTO{})

	err := e.svc.RuleAssignmentSave(ctx, &admindto.RuleAssignmentSaveReq{
		RuleID:      ruleID,
		Assignments: []admindto.RuleAssignmentItem{{TargetType: adminmodel.AssignmentTargetTypeUser, TargetID: 0}},
	})
	wantErr(t, err, adminenums.ErrInvalidAssignment)

	err = e.svc.RuleAssignmentSave(ctx, &admindto.RuleAssignmentSaveReq{
		RuleID:      ruleID,
		Assignments: []admindto.RuleAssignmentItem{{TargetType: 99, TargetID: 1}},
	})
	wantErr(t, err, adminenums.ErrInvalidAssignment)

	err = e.svc.RuleAssignmentSave(ctx, &admindto.RuleAssignmentSaveReq{
		RuleID:      ruleID,
		Assignments: []admindto.RuleAssignmentItem{{TargetType: adminmodel.AssignmentTargetTypeDept, TargetID: 1}},
	})
	wantErr(t, err, adminenums.ErrInvalidAssignment)
}

func TestRuleAssignmentSaveDedupAndReplace(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	ruleID := ruleCreate(t, e, "分配规则", "ORDER", 1, admindto.RuleConfigDTO{})

	err := e.svc.RuleAssignmentSave(ctx, &admindto.RuleAssignmentSaveReq{
		RuleID: ruleID,
		Assignments: []admindto.RuleAssignmentItem{
			{TargetType: adminmodel.AssignmentTargetTypeUser, TargetID: 1},
			{TargetType: adminmodel.AssignmentTargetTypeUser, TargetID: 1},
		},
	})
	wantErr(t, err, "")
	list, err := e.svc.RuleAssignmentList(ctx, &admindto.RuleAssignmentListReq{RuleID: ruleID})
	wantErr(t, err, "")
	if len(list.List) != 1 {
		t.Fatalf("重复目标应去重: %d", len(list.List))
	}

	err = e.svc.RuleAssignmentSave(ctx, &admindto.RuleAssignmentSaveReq{
		RuleID: ruleID,
		Assignments: []admindto.RuleAssignmentItem{
			{TargetType: adminmodel.AssignmentTargetTypeUser, TargetID: 1},
			{TargetType: adminmodel.AssignmentTargetTypeRole, TargetID: 7},
		},
	})
	wantErr(t, err, "")
	list, err = e.svc.RuleAssignmentList(ctx, &admindto.RuleAssignmentListReq{RuleID: ruleID})
	wantErr(t, err, "")
	if len(list.List) != 2 {
		t.Fatalf("全量替换后应有 2 条: %d", len(list.List))
	}
}

func TestGetRulesByUser(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	ruleID := ruleCreate(t, e, "用户规则", "ORDER", 1, admindto.RuleConfigDTO{
		OmitFields: []string{"price"},
	})
	if err := e.svc.RuleAssignmentSave(ctx, &admindto.RuleAssignmentSaveReq{
		RuleID: ruleID,
		Assignments: []admindto.RuleAssignmentItem{
			{TargetType: adminmodel.AssignmentTargetTypeUser, TargetID: 100},
		},
	}); err != nil {
		t.Fatalf("分配失败: %v", err)
	}

	rules, err := e.svc.GetRules(ctx, &datarule.UserContext{UserID: 100, Roles: []string{}}, "ORDER")
	wantErr(t, err, "")
	if len(rules) != 1 || len(rules[0].OmitFields) != 1 {
		t.Fatalf("用户规则命中不符: %+v", rules)
	}

	rules, err = e.svc.GetRules(ctx, &datarule.UserContext{UserID: 101, Roles: []string{}}, "ORDER")
	wantErr(t, err, "")
	if len(rules) != 0 {
		t.Fatalf("无关用户不应命中: %+v", rules)
	}
}

func TestGetRulesByRole(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	roleID := createRole(t, e, "editor_"+uniq(""), "编辑")
	ruleID := ruleCreate(t, e, "角色规则", "ORDER", 1, admindto.RuleConfigDTO{})
	if err := e.svc.RuleAssignmentSave(ctx, &admindto.RuleAssignmentSaveReq{
		RuleID: ruleID,
		Assignments: []admindto.RuleAssignmentItem{
			{TargetType: adminmodel.AssignmentTargetTypeRole, TargetID: roleID},
		},
	}); err != nil {
		t.Fatalf("分配失败: %v", err)
	}

	rules, err := e.svc.GetRules(ctx, &datarule.UserContext{UserID: 100, Roles: []string{codeOfRole(t, e, roleID)}}, "ORDER")
	wantErr(t, err, "")
	if len(rules) != 1 {
		t.Fatalf("角色规则命中不符: %+v", rules)
	}
}

func TestGetRulesByDeptScope(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	root := deptCreate(t, e, "总部", "DR_"+uniq(""))
	child := deptCreate(t, e, "分部", "DC2_"+uniq(""), root)

	ruleSelf := ruleCreate(t, e, "本部门规则", "ORDER", 1, admindto.RuleConfigDTO{})
	if err := e.svc.RuleAssignmentSave(ctx, &admindto.RuleAssignmentSaveReq{
		RuleID: ruleSelf,
		Assignments: []admindto.RuleAssignmentItem{
			{TargetType: adminmodel.AssignmentTargetTypeDept, TargetID: root, TargetScope: adminmodel.AssignmentTargetScopeSelf},
		},
	}); err != nil {
		t.Fatalf("分配失败: %v", err)
	}

	ruleTree := ruleCreate(t, e, "树形规则", "ORDER", 1, admindto.RuleConfigDTO{})
	if err := e.svc.RuleAssignmentSave(ctx, &admindto.RuleAssignmentSaveReq{
		RuleID: ruleTree,
		Assignments: []admindto.RuleAssignmentItem{
			{TargetType: adminmodel.AssignmentTargetTypeDept, TargetID: root, TargetScope: adminmodel.AssignmentTargetScopeSelfAndChildren},
		},
	}); err != nil {
		t.Fatalf("分配失败: %v", err)
	}

	rules, err := e.svc.GetRules(ctx, &datarule.UserContext{UserID: 1, Roles: []string{}, DeptID: root}, "ORDER")
	wantErr(t, err, "")
	if len(rules) != 2 {
		t.Fatalf("总部应命中 2 条规则: %+v", rules)
	}

	rules, err = e.svc.GetRules(ctx, &datarule.UserContext{UserID: 1, Roles: []string{}, DeptID: child}, "ORDER")
	wantErr(t, err, "")
	if len(rules) != 1 {
		t.Fatalf("分部应命中 1 条规则: %+v", rules)
	}
}

func TestGetRulesNilUser(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	rules, err := e.svc.GetRules(ctx, nil, "ORDER")
	wantErr(t, err, "")
	if len(rules) != 0 {
		t.Fatalf("nil 用户不应命中: %+v", rules)
	}
}

func TestGetRulesInvalidConfig(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	rule := &adminmodel.SysRuleEntity{
		RuleName: "坏配置", Domain: "ORDER", Config: "[]", Status: adminmodel.RuleStatusEnabled,
	}
	if err := e.db.Create(rule).Error; err != nil {
		t.Fatalf("创建规则失败: %v", err)
	}
	if err := e.db.Create(&adminmodel.SysRuleAssignmentEntity{
		RuleID: rule.ID, TargetType: adminmodel.AssignmentTargetTypeUser, TargetID: 100,
	}).Error; err != nil {
		t.Fatalf("创建分配失败: %v", err)
	}

	_, err := e.svc.GetRules(ctx, &datarule.UserContext{UserID: 100, Roles: []string{}}, "ORDER")
	if err == nil {
		t.Fatalf("非法 config 应返回解析错误")
	}
}

func TestRuleSchemaListDetail(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	domains, err := e.svc.RuleSchemaList(ctx)
	wantErr(t, err, "")
	if len(domains) == 0 {
		t.Fatalf("注册中心应有 domain")
	}

	detail, err := e.svc.RuleSchemaDetail(ctx, &admindto.RuleSchemaDetailReq{Domain: domains[0].Domain})
	wantErr(t, err, "")
	if detail == nil || len(detail.Fields) == 0 {
		t.Fatalf("domain 详情应含字段白名单")
	}

	missing, err := e.svc.RuleSchemaDetail(ctx, &admindto.RuleSchemaDetailReq{Domain: "NOT_REGISTERED"})
	wantErr(t, err, "")
	if missing != nil {
		t.Fatalf("未注册 domain 应返回 nil")
	}
}

func TestRuleCreateInvalidDomain(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	err := e.svc.RuleCreate(ctx, &admindto.RuleCreateReq{
		RuleName: "非法域规则", Domain: "NOT_REGISTERED",
		Config: admindto.RuleConfigDTO{}, Status: 1,
	})
	wantErr(t, err, adminenums.ErrInvalidDomain)

	var count int64
	if err := e.db.Model(&adminmodel.SysRuleEntity{}).Where("domain = ?", "NOT_REGISTERED").Count(&count).Error; err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if count != 0 {
		t.Fatalf("非法 domain 规则不应落库: %d", count)
	}
}

func TestRuleUpdateInvalidDomain(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	ruleID := ruleCreate(t, e, "合法规则", "ORDER", 1, admindto.RuleConfigDTO{})
	err := e.svc.RuleUpdate(ctx, &admindto.RuleUpdateReq{
		ID: ruleID, RuleName: "改名", Domain: "NOT_REGISTERED",
		Config: admindto.RuleConfigDTO{}, Status: 1,
	})
	wantErr(t, err, adminenums.ErrInvalidDomain)

	detail, err := e.svc.RuleDetail(ctx, &admindto.RuleDetailReq{ID: ruleID})
	wantErr(t, err, "")
	if detail.RuleName != "合法规则" || detail.Domain != "ORDER" {
		t.Fatalf("非法 domain 更新不应改动原规则: %+v", detail)
	}
}

func TestRuleCreateDisabledExplicit(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	ruleID := ruleCreate(t, e, "禁用规则"+uniq(""), "ORDER", adminmodel.RuleStatusDisabled, admindto.RuleConfigDTO{})

	var rule adminmodel.SysRuleEntity
	if err := e.db.First(&rule, ruleID).Error; err != nil {
		t.Fatalf("查询规则失败: %v", err)
	}
	if rule.Status != adminmodel.RuleStatusDisabled {
		t.Fatalf("显式禁用应落库 status=0: got=%d", rule.Status)
	}

	if err := e.svc.RuleAssignmentSave(ctx, &admindto.RuleAssignmentSaveReq{
		RuleID: ruleID,
		Assignments: []admindto.RuleAssignmentItem{
			{TargetType: adminmodel.AssignmentTargetTypeUser, TargetID: 100},
		},
	}); err != nil {
		t.Fatalf("分配失败: %v", err)
	}
	rules, err := e.svc.GetRules(ctx, &datarule.UserContext{UserID: 100, Roles: []string{}}, "ORDER")
	wantErr(t, err, "")
	if len(rules) != 0 {
		t.Fatalf("禁用规则不应命中: %+v", rules)
	}
}

func TestRuleUpdateDisablePersists(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	ruleID := ruleCreate(t, e, "待禁用规则"+uniq(""), "ORDER", adminmodel.RuleStatusEnabled, admindto.RuleConfigDTO{})
	err := e.svc.RuleUpdate(ctx, &admindto.RuleUpdateReq{
		ID: ruleID, RuleName: "已禁用规则", Domain: "ORDER",
		Config: admindto.RuleConfigDTO{}, Status: adminmodel.RuleStatusDisabled,
	})
	wantErr(t, err, "")

	var rule adminmodel.SysRuleEntity
	if err := e.db.First(&rule, ruleID).Error; err != nil {
		t.Fatalf("查询规则失败: %v", err)
	}
	if rule.Status != adminmodel.RuleStatusDisabled {
		t.Fatalf("更新为禁用应落库 status=0: got=%d", rule.Status)
	}
}

func TestRuleCreateRejectsConfigOutsideDomain(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	conditionCase := func(field, op string) admindto.RuleConfigDTO {
		return admindto.RuleConfigDTO{ConditionGroups: []admindto.RuleConditionGroupDTO{{
			Logic:      "AND",
			Conditions: []admindto.RuleConditionDTO{{Field: field, Op: op, Value: "1"}},
		}}}
	}
	logicCase := func(logic string) admindto.RuleConfigDTO {
		return admindto.RuleConfigDTO{ConditionGroups: []admindto.RuleConditionGroupDTO{{
			Logic:      logic,
			Conditions: []admindto.RuleConditionDTO{{Field: "status", Op: "EQ", Value: "1"}},
		}}}
	}

	cases := []struct {
		name   string
		domain string
		config admindto.RuleConfigDTO
		want   string
	}{
		{"条件字段不在白名单", "ORDER", conditionCase("created_by", "EQ"), adminenums.ErrRuleFieldNotAllowed},
		{"操作符不在该字段声明内", "ORDER", conditionCase("order_no", "GT"), adminenums.ErrRuleOpNotAllowed},
		{"操作符小写不接受", "ADMIN", conditionCase("status", "eq"), adminenums.ErrRuleOpNotAllowed},
		{"条件逻辑非法", "ADMIN", logicCase("XOR"), adminenums.ErrRuleLogicNotAllowed},
		{"条件逻辑小写不接受", "ADMIN", logicCase("or"), adminenums.ErrRuleLogicNotAllowed},
		{"屏蔽字段不在白名单", "ADMIN", admindto.RuleConfigDTO{OmitFields: []string{"password"}}, adminenums.ErrRuleFieldNotAllowed},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := e.svc.RuleCreate(ctx, &admindto.RuleCreateReq{
				RuleName: "拒绝_" + uniq(""), Domain: c.domain, Config: c.config,
				Status: adminmodel.RuleStatusEnabled,
			})
			wantErr(t, err, c.want)
		})
	}

	var count int64
	if err := e.db.Model(&adminmodel.SysRuleEntity{}).Count(&count).Error; err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if count != 0 {
		t.Fatalf("被拒绝的规则不应落库: %d", count)
	}
}

func TestRuleUpdateRejectsConfigOutsideDomain(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	ruleName := "合法规则" + uniq("")
	ruleID := ruleCreate(t, e, ruleName, "ORDER", adminmodel.RuleStatusEnabled,
		admindto.RuleConfigDTO{OmitFields: []string{"price"}})

	err := e.svc.RuleUpdate(ctx, &admindto.RuleUpdateReq{
		ID: ruleID, RuleName: "改名", Domain: "ORDER",
		Config: admindto.RuleConfigDTO{
			OmitFields: []string{"price"},
			ConditionGroups: []admindto.RuleConditionGroupDTO{{
				Logic:      "AND",
				Conditions: []admindto.RuleConditionDTO{{Field: "order_no", Op: "BETWEEN", Value: "1,2"}},
			}},
		},
		Status: adminmodel.RuleStatusEnabled,
	})
	wantErr(t, err, adminenums.ErrRuleOpNotAllowed)

	detail, err := e.svc.RuleDetail(ctx, &admindto.RuleDetailReq{ID: ruleID})
	wantErr(t, err, "")
	if detail.RuleName != ruleName || len(detail.Config.ConditionGroups) != 0 {
		t.Fatalf("非法配置更新不应改动原规则: %+v", detail)
	}
}

func TestRuleCreateAcceptsDeclaredOperators(t *testing.T) {
	e := setupEnv(t)

	ruleCreate(t, e, "部门范围"+uniq(""), "ADMIN", adminmodel.RuleStatusEnabled, admindto.RuleConfigDTO{
		OmitFields: []string{"phone"},
		ConditionGroups: []admindto.RuleConditionGroupDTO{
			{Logic: "AND", Conditions: []admindto.RuleConditionDTO{{Field: "dept_id", Op: "EQ", Value: "dept.scope:SELF"}}},
			{Logic: "OR", Conditions: []admindto.RuleConditionDTO{
				{Field: "username", Op: "LIKE", Value: "admin"},
				{Field: "status", Op: "IN", Value: "1,2"},
			}},
		},
	})
}

func ruleCreate(t *testing.T, e *env, name, domain string, status int, config admindto.RuleConfigDTO) uint64 {
	t.Helper()
	err := e.svc.RuleCreate(context.Background(), &admindto.RuleCreateReq{
		RuleName: name, Domain: domain, Config: config, Status: status,
	})
	wantErr(t, err, "")
	var rule adminmodel.SysRuleEntity
	if err := e.db.Where("rule_name = ?", name).Order("id DESC").First(&rule).Error; err != nil {
		t.Fatalf("查询规则失败: %v", err)
	}
	return rule.ID
}

func codeOfRole(t *testing.T, e *env, id uint64) string {
	t.Helper()
	role, err := e.dbGetRole(id)
	if err != nil {
		t.Fatalf("查询角色失败: %v", err)
	}
	return role.RoleCode
}
