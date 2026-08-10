package dataruleservice

import (
	"context"
	"testing"

	dataruledto "go_wp/internal/module/datarule/dto"
	datamodel "go_wp/internal/module/datarule/model"
	"go_wp/pkg/datarule"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestRuleConfigContractRoundTripAndDisabledStatus(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败：%v", err)
	}
	if err = db.AutoMigrate(&datamodel.SysRuleEntity{}, &datamodel.SysRuleAssignmentEntity{}); err != nil {
		t.Fatalf("创建测试表失败：%v", err)
	}

	svc := NewService(
		datamodel.NewSysRuleModel(db),
		datamodel.NewSysRuleAssignmentModel(db),
		nil,
		nil,
	)
	ctx := context.Background()
	config := datarule.RuleConfig{
		OmitFields: []string{"email"},
		ConditionGroups: []datarule.ConditionGroup{
			{
				Logic: "AND",
				Conditions: []datarule.Condition{
					{Field: "dept_id", Op: "EQ", Value: "1"},
				},
			},
		},
	}

	if err = svc.Create(ctx, &dataruledto.CreateReq{
		RuleName: "管理员数据规则",
		Domain:   "ADMIN",
		Config:   config,
		Status:   datamodel.RuleStatusEnabled,
	}); err != nil {
		t.Fatalf("创建规则失败：%v", err)
	}

	var created struct {
		ID     uint64
		Config string
		Status int
	}
	if err = db.Model(&datamodel.SysRuleEntity{}).
		Select("id", "config", "status").
		First(&created).Error; err != nil {
		t.Fatalf("查询规则失败：%v", err)
	}
	storedConfig, err := decodeRuleConfig(created.Config)
	if err != nil {
		t.Fatalf("解析持久化规则配置失败：%v", err)
	}
	if len(storedConfig.OmitFields) != 1 || storedConfig.OmitFields[0] != "email" {
		t.Fatalf("规则配置未按对象契约持久化：%+v", storedConfig)
	}

	detailEntity := datamodel.SysRuleEntity{
		RuleName: "待禁用规则",
		Domain:   "ADMIN",
		Config:   created.Config,
		Status:   datamodel.RuleStatusEnabled,
	}
	if err = db.Session(&gorm.Session{SkipHooks: true}).Create(&detailEntity).Error; err != nil {
		t.Fatalf("写入详情测试规则失败：%v", err)
	}

	detail, err := svc.Detail(ctx, &dataruledto.DetailReq{ID: detailEntity.ID})
	if err != nil {
		t.Fatalf("查询规则详情失败：%v", err)
	}
	if len(detail.Config.OmitFields) != 1 || detail.Config.OmitFields[0] != "email" {
		t.Fatalf("规则配置未按对象契约返回：%+v", detail.Config)
	}

	if err = svc.Update(ctx, &dataruledto.UpdateReq{
		ID:       detailEntity.ID,
		RuleName: detail.RuleName,
		Domain:   detail.Domain,
		Config:   detail.Config,
		Status:   datamodel.RuleStatusDisabled,
	}); err != nil {
		t.Fatalf("禁用规则失败：%v", err)
	}

	var updatedStatus int
	if err = db.Model(&datamodel.SysRuleEntity{}).
		Select("status").
		Where("id = ?", detailEntity.ID).
		Scan(&updatedStatus).Error; err != nil {
		t.Fatalf("查询更新后的规则失败：%v", err)
	}
	if updatedStatus != datamodel.RuleStatusDisabled {
		t.Fatalf("禁用状态未落库，实际状态：%d", updatedStatus)
	}
}

func TestAssignmentSaveReplacesAndValidatesTargets(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败：%v", err)
	}
	if err = db.AutoMigrate(&datamodel.SysRuleEntity{}, &datamodel.SysRuleAssignmentEntity{}); err != nil {
		t.Fatalf("创建测试表失败：%v", err)
	}

	svc := NewService(
		datamodel.NewSysRuleModel(db),
		datamodel.NewSysRuleAssignmentModel(db),
		nil,
		nil,
	)
	ctx := context.Background()
	if err = svc.AssignmentSave(ctx, &dataruledto.AssignmentSaveReq{
		RuleID: 1,
		Assignments: []dataruledto.AssignmentItem{
			{TargetType: datamodel.AssignmentTargetTypeRole, TargetID: 10},
			{TargetType: datamodel.AssignmentTargetTypeRole, TargetID: 10},
			{TargetType: datamodel.AssignmentTargetTypeUser, TargetID: 20},
			{
				TargetType:  datamodel.AssignmentTargetTypeDept,
				TargetID:    30,
				TargetScope: datamodel.AssignmentTargetScopeSelfAndChildren,
			},
		},
	}); err != nil {
		t.Fatalf("保存规则分配失败：%v", err)
	}

	var assignments []struct {
		TargetType  int
		TargetID    uint64
		TargetScope int
	}
	if err = db.Model(&datamodel.SysRuleAssignmentEntity{}).
		Select("target_type", "target_id", "target_scope").
		Where("rule_id = ?", 1).
		Order("id ASC").
		Scan(&assignments).Error; err != nil {
		t.Fatalf("查询规则分配失败：%v", err)
	}
	if len(assignments) != 3 {
		t.Fatalf("重复分配未去重，实际数量：%d", len(assignments))
	}
	if assignments[2].TargetScope != datamodel.AssignmentTargetScopeSelfAndChildren {
		t.Fatalf("部门作用范围未保存：%+v", assignments[2])
	}

	if err = svc.AssignmentSave(ctx, &dataruledto.AssignmentSaveReq{
		RuleID: 1,
		Assignments: []dataruledto.AssignmentItem{
			{TargetType: 99, TargetID: 30},
		},
	}); err == nil {
		t.Fatal("无效分配目标应被拒绝")
	}
	assignments = nil
	if err = db.Model(&datamodel.SysRuleAssignmentEntity{}).
		Select("target_type", "target_id").
		Where("rule_id = ?", 1).
		Order("id ASC").
		Scan(&assignments).Error; err != nil {
		t.Fatalf("查询校验失败后的规则分配失败：%v", err)
	}
	if len(assignments) != 3 {
		t.Fatalf("无效请求破坏了原分配，实际数量：%d", len(assignments))
	}

	if err = svc.AssignmentSave(ctx, &dataruledto.AssignmentSaveReq{
		RuleID: 1,
		Assignments: []dataruledto.AssignmentItem{
			{TargetType: datamodel.AssignmentTargetTypeDept, TargetID: 30},
		},
	}); err == nil {
		t.Fatal("未指定作用范围的部门分配应被拒绝")
	}

	if err = svc.AssignmentSave(ctx, &dataruledto.AssignmentSaveReq{
		RuleID: 1,
		Assignments: []dataruledto.AssignmentItem{
			{TargetType: datamodel.AssignmentTargetTypeUser, TargetID: 30},
		},
	}); err != nil {
		t.Fatalf("替换规则分配失败：%v", err)
	}
	assignments = nil
	if err = db.Model(&datamodel.SysRuleAssignmentEntity{}).
		Select("target_type", "target_id").
		Where("rule_id = ?", 1).
		Order("id ASC").
		Scan(&assignments).Error; err != nil {
		t.Fatalf("查询替换后的规则分配失败：%v", err)
	}
	if len(assignments) != 1 || assignments[0].TargetID != 30 {
		t.Fatalf("规则分配未全量替换：%+v", assignments)
	}
}
