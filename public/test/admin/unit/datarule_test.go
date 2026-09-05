package unit

import (
	"context"
	"testing"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	adminmodel "go_wp/internal/module/admin/model"
	"go_wp/pkg/datarule"
)

// TestRuleCreateAndDetailRoundTrip 规则创建 → 详情查询，Config JSON 往返一致。
func TestRuleCreateAndDetailRoundTrip(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	ruleID := ruleCreate(t, e, "只看本部门", "ORDER", 1, datarule.RuleConfig{
		OmitFields: []string{"price"},
		ConditionGroups: []datarule.ConditionGroup{{
			Logic: "AND",
			Conditions: []datarule.Condition{
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

// TestRuleDetailNotFound 查询不存在的规则报错。
func TestRuleDetailNotFound(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	_, err := e.svc.RuleDetail(ctx, &admindto.RuleDetailReq{ID: 888888})
	wantErr(t, err, adminenums.ErrRuleNotFound)
}

// TestRuleUpdateSuccess 更新规则名称/域/配置。
func TestRuleUpdateSuccess(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	ruleID := ruleCreate(t, e, "旧规则", "ORDER", 1, datarule.RuleConfig{})
	err := e.svc.RuleUpdate(ctx, &admindto.RuleUpdateReq{
		ID: ruleID, RuleName: "新规则", Domain: "ADMIN",
		Config: datarule.RuleConfig{OmitFields: []string{"phone"}}, Status: 1,
	})
	wantErr(t, err, "")

	detail, err := e.svc.RuleDetail(ctx, &admindto.RuleDetailReq{ID: ruleID})
	wantErr(t, err, "")
	if detail.RuleName != "新规则" || detail.Domain != "ADMIN" || len(detail.Config.OmitFields) != 1 {
		t.Fatalf("更新后不符: %+v", detail)
	}
}

// TestRuleUpdateNotFound 更新不存在的规则报错。
func TestRuleUpdateNotFound(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	err := e.svc.RuleUpdate(ctx, &admindto.RuleUpdateReq{
		ID: 777777, RuleName: "x", Domain: "ORDER", Config: datarule.RuleConfig{}, Status: 1,
	})
	wantErr(t, err, adminenums.ErrRuleNotFound)
}

// TestRuleDeleteCleansAssignments 删除规则连带清理分配记录。
func TestRuleDeleteCleansAssignments(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	ruleID := ruleCreate(t, e, "待删规则", "ORDER", 1, datarule.RuleConfig{})
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

// TestRuleListFilters 规则列表按 domain/status 筛选。
func TestRuleListFilters(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	ruleCreate(t, e, "订单规则", "ORDER", 1, datarule.RuleConfig{})
	ruleCreate(t, e, "管理员规则", "ADMIN", 1, datarule.RuleConfig{})

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

// TestRuleAssignmentSaveInvalidTarget 非法分配目标被拒绝：TargetID=0、非法 TargetType、部门缺 scope。
func TestRuleAssignmentSaveInvalidTarget(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	ruleID := ruleCreate(t, e, "规则", "ORDER", 1, datarule.RuleConfig{})

	// TargetID=0
	err := e.svc.RuleAssignmentSave(ctx, &admindto.RuleAssignmentSaveReq{
		RuleID:      ruleID,
		Assignments: []admindto.RuleAssignmentItem{{TargetType: adminmodel.AssignmentTargetTypeUser, TargetID: 0}},
	})
	wantErr(t, err, adminenums.ErrInvalidAssignment)

	// 非法 TargetType
	err = e.svc.RuleAssignmentSave(ctx, &admindto.RuleAssignmentSaveReq{
		RuleID:      ruleID,
		Assignments: []admindto.RuleAssignmentItem{{TargetType: 99, TargetID: 1}},
	})
	wantErr(t, err, adminenums.ErrInvalidAssignment)

	// 部门缺 scope
	err = e.svc.RuleAssignmentSave(ctx, &admindto.RuleAssignmentSaveReq{
		RuleID:      ruleID,
		Assignments: []admindto.RuleAssignmentItem{{TargetType: adminmodel.AssignmentTargetTypeDept, TargetID: 1}},
	})
	wantErr(t, err, adminenums.ErrInvalidAssignment)
}

// TestRuleAssignmentSaveDedupAndReplace 同目标去重 + 全量替换语义。
func TestRuleAssignmentSaveDedupAndReplace(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	ruleID := ruleCreate(t, e, "分配规则", "ORDER", 1, datarule.RuleConfig{})

	// 重复 target 只落一条
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

	// 全量替换为两个新目标
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

// TestGetRulesByUser 按用户直接分配命中规则。
func TestGetRulesByUser(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	ruleID := ruleCreate(t, e, "用户规则", "ORDER", 1, datarule.RuleConfig{
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

	// 非关联用户不命中
	rules, err = e.svc.GetRules(ctx, &datarule.UserContext{UserID: 101, Roles: []string{}}, "ORDER")
	wantErr(t, err, "")
	if len(rules) != 0 {
		t.Fatalf("无关用户不应命中: %+v", rules)
	}
}

// TestGetRulesByRole 按用户角色分配命中规则。
func TestGetRulesByRole(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	roleID := createRole(t, e, "editor_"+uniq(""), "编辑")
	ruleID := ruleCreate(t, e, "角色规则", "ORDER", 1, datarule.RuleConfig{})
	if err := e.svc.RuleAssignmentSave(ctx, &admindto.RuleAssignmentSaveReq{
		RuleID: ruleID,
		Assignments: []admindto.RuleAssignmentItem{
			{TargetType: adminmodel.AssignmentTargetTypeRole, TargetID: roleID},
		},
	}); err != nil {
		t.Fatalf("分配失败: %v", err)
	}

	// 需要角色编码与 ID 对应（sys_role 有该角色）
	rules, err := e.svc.GetRules(ctx, &datarule.UserContext{UserID: 100, Roles: []string{codeOfRole(t, e, roleID)}}, "ORDER")
	wantErr(t, err, "")
	if len(rules) != 1 {
		t.Fatalf("角色规则命中不符: %+v", rules)
	}
}

// TestGetRulesByDeptScope 部门规则按 scope 生效：SELF 只对本部门，SELF_AND_CHILDREN 覆盖子孙。
func TestGetRulesByDeptScope(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	root := deptCreate(t, e, "总部", "DR_"+uniq(""))
	child := deptCreate(t, e, "分部", "DC2_"+uniq(""), root)

	// 规则A：scope=SELF 给总部 → 只对总部生效
	ruleSelf := ruleCreate(t, e, "本部门规则", "ORDER", 1, datarule.RuleConfig{})
	if err := e.svc.RuleAssignmentSave(ctx, &admindto.RuleAssignmentSaveReq{
		RuleID: ruleSelf,
		Assignments: []admindto.RuleAssignmentItem{
			{TargetType: adminmodel.AssignmentTargetTypeDept, TargetID: root, TargetScope: adminmodel.AssignmentTargetScopeSelf},
		},
	}); err != nil {
		t.Fatalf("分配失败: %v", err)
	}

	// 规则B：scope=SELF_AND_CHILDREN 给总部 → 覆盖子孙
	ruleTree := ruleCreate(t, e, "树形规则", "ORDER", 1, datarule.RuleConfig{})
	if err := e.svc.RuleAssignmentSave(ctx, &admindto.RuleAssignmentSaveReq{
		RuleID: ruleTree,
		Assignments: []admindto.RuleAssignmentItem{
			{TargetType: adminmodel.AssignmentTargetTypeDept, TargetID: root, TargetScope: adminmodel.AssignmentTargetScopeSelfAndChildren},
		},
	}); err != nil {
		t.Fatalf("分配失败: %v", err)
	}

	// 总部自身：两条都命中
	rules, err := e.svc.GetRules(ctx, &datarule.UserContext{UserID: 1, Roles: []string{}, DeptID: root}, "ORDER")
	wantErr(t, err, "")
	if len(rules) != 2 {
		t.Fatalf("总部应命中 2 条规则: %+v", rules)
	}

	// 分部：只命中树形规则
	rules, err = e.svc.GetRules(ctx, &datarule.UserContext{UserID: 1, Roles: []string{}, DeptID: child}, "ORDER")
	wantErr(t, err, "")
	if len(rules) != 1 {
		t.Fatalf("分部应命中 1 条规则: %+v", rules)
	}
}

// TestGetRulesNilUser nil 用户不命中任何规则。
func TestGetRulesNilUser(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	rules, err := e.svc.GetRules(ctx, nil, "ORDER")
	wantErr(t, err, "")
	if len(rules) != 0 {
		t.Fatalf("nil 用户不应命中: %+v", rules)
	}
}

// TestGetRulesInvalidConfig 规则 config 非法 JSON 时返回解析错误（fail 明确）。
func TestGetRulesInvalidConfig(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	// 直接落库一条 config 非法的启用规则并分配给用户
	rule := &adminmodel.SysRuleEntity{
		RuleName: "坏配置", Domain: "ORDER", Config: "{not-json", Status: adminmodel.RuleStatusEnabled,
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

// TestRuleSchemaListDetail 注册中心 domain 列表与字段白名单。
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

	// 未注册 domain 返回 nil 而非错误
	missing, err := e.svc.RuleSchemaDetail(ctx, &admindto.RuleSchemaDetailReq{Domain: "NOT_REGISTERED"})
	wantErr(t, err, "")
	if missing != nil {
		t.Fatalf("未注册 domain 应返回 nil")
	}
}

// TestRuleCreateInvalidDomain 未注册 domain 被拒绝（ErrInvalidDomain）。
// 修复前：RuleCreate 不校验 domain 注册，任意 domain 均可落库。
func TestRuleCreateInvalidDomain(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	err := e.svc.RuleCreate(ctx, &admindto.RuleCreateReq{
		RuleName: "非法域规则", Domain: "NOT_REGISTERED",
		Config: datarule.RuleConfig{}, Status: 1,
	})
	wantErr(t, err, adminenums.ErrInvalidDomain)

	// 不应落库
	var count int64
	if err := e.db.Model(&adminmodel.SysRuleEntity{}).Where("domain = ?", "NOT_REGISTERED").Count(&count).Error; err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if count != 0 {
		t.Fatalf("非法 domain 规则不应落库: %d", count)
	}
}

// TestRuleUpdateInvalidDomain 更新为未注册 domain 被拒绝，且原规则不被改动。
func TestRuleUpdateInvalidDomain(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	ruleID := ruleCreate(t, e, "合法规则", "ORDER", 1, datarule.RuleConfig{})
	err := e.svc.RuleUpdate(ctx, &admindto.RuleUpdateReq{
		ID: ruleID, RuleName: "改名", Domain: "NOT_REGISTERED",
		Config: datarule.RuleConfig{}, Status: 1,
	})
	wantErr(t, err, adminenums.ErrInvalidDomain)

	detail, err := e.svc.RuleDetail(ctx, &admindto.RuleDetailReq{ID: ruleID})
	wantErr(t, err, "")
	if detail.RuleName != "合法规则" || detail.Domain != "ORDER" {
		t.Fatalf("非法 domain 更新不应改动原规则: %+v", detail)
	}
}

// TestRuleCreateDisabledExplicit 显式传 status=0（禁用）创建数据规则，落库为禁用且不被 GetRules 命中。
// 修复前：SysRuleEntity.Status 带 gorm default:1 tag，gorm 对零值字段（status=0）用 DB 默认值
// 替换并回写 struct，显式禁用被改写成启用（1），"创建禁用数据规则"失效。
func TestRuleCreateDisabledExplicit(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	ruleID := ruleCreate(t, e, "禁用规则"+uniq(""), "ORDER", adminmodel.RuleStatusDisabled, datarule.RuleConfig{})

	var rule adminmodel.SysRuleEntity
	if err := e.db.First(&rule, ruleID).Error; err != nil {
		t.Fatalf("查询规则失败: %v", err)
	}
	if rule.Status != adminmodel.RuleStatusDisabled {
		t.Fatalf("显式禁用应落库 status=0: got=%d", rule.Status)
	}

	// 分配给用户后，禁用规则仍不应被 GetRules 命中
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

// TestRuleUpdateDisablePersists 启用规则显式更新为 status=0（禁用）落库为禁用。
// 回归覆盖：RuleUpdate 显式赋值 status=0 + SysRuleModel.Update 显式 Select 含 status 列，
// 禁用状态在更新路径同样不被跳过。
func TestRuleUpdateDisablePersists(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	ruleID := ruleCreate(t, e, "待禁用规则"+uniq(""), "ORDER", adminmodel.RuleStatusEnabled, datarule.RuleConfig{})
	err := e.svc.RuleUpdate(ctx, &admindto.RuleUpdateReq{
		ID: ruleID, RuleName: "已禁用规则", Domain: "ORDER",
		Config: datarule.RuleConfig{}, Status: adminmodel.RuleStatusDisabled,
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

// --- helpers ---

func ruleCreate(t *testing.T, e *env, name, domain string, status int, config datarule.RuleConfig) uint64 {
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
