package adminservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	adminmodel "go_wp/internal/module/admin/model"
	"go_wp/pkg/datarule"
)

// GetRules 实现 datarule.RuleProvider 接口。
// 查询角色、用户和部门关联的所有已启用限制规则，并按规则 ID 去重。
func (s *Service) GetRules(ctx context.Context, user *datarule.UserContext, domain string) ([]datarule.RuleConfig, error) {
	if user == nil || user.UserID == 0 {
		return nil, nil
	}

	var rules []struct {
		ID     uint64 `gorm:"column:id"`
		Config string `gorm:"column:config"`
	}
	if err := s.drm.DB(ctx).
		Select("id, config").
		Where("domain = ? AND status = ?", domain, adminmodel.RuleStatusEnabled).
		Find(&rules).Error; err != nil {
		return nil, err
	}
	if len(rules) == 0 {
		return nil, nil
	}

	ruleIDs := make([]uint64, 0, len(rules))
	ruleMap := make(map[uint64]string, len(rules))
	for _, rule := range rules {
		ruleIDs = append(ruleIDs, rule.ID)
		ruleMap[rule.ID] = rule.Config
	}

	// 同包直调：角色编码 → 启用角色 ID
	roleIDs, err := s.GetEnabledRoleIDsByCodes(ctx, user.Roles)
	if err != nil {
		return nil, err
	}

	var ancestorDeptIDs []uint64
	if user.DeptID != 0 {
		ancestorDeptIDs, err = s.AncestorIDs(ctx, user.DeptID)
		if err != nil {
			return nil, err
		}
	}

	predicates := []string{"target_type = ? AND target_id = ?"}
	args := []any{adminmodel.AssignmentTargetTypeUser, user.UserID}
	if len(roleIDs) > 0 {
		predicates = append(predicates, "target_type = ? AND target_id IN ?")
		args = append(args, adminmodel.AssignmentTargetTypeRole, roleIDs)
	}
	if user.DeptID != 0 {
		predicates = append(predicates, "target_type = ? AND target_id = ?")
		args = append(args, adminmodel.AssignmentTargetTypeDept, user.DeptID)
	}
	if len(ancestorDeptIDs) > 0 {
		predicates = append(predicates, "target_type = ? AND target_id IN ? AND target_scope = ?")
		args = append(
			args,
			adminmodel.AssignmentTargetTypeDept,
			ancestorDeptIDs,
			adminmodel.AssignmentTargetScopeSelfAndChildren,
		)
	}

	type assignmentRow struct {
		RuleID uint64 `gorm:"column:rule_id"`
	}
	var assignments []assignmentRow
	if err := s.dram.DB(ctx).
		Select("rule_id").
		Where("rule_id IN ?", ruleIDs).
		Where("("+strings.Join(predicates, ") OR (")+")", args...).
		Find(&assignments).Error; err != nil {
		return nil, err
	}

	seen := make(map[uint64]struct{}, len(assignments))
	result := make([]datarule.RuleConfig, 0, len(assignments))
	for _, assignment := range assignments {
		if _, ok := seen[assignment.RuleID]; ok {
			continue
		}
		seen[assignment.RuleID] = struct{}{}

		configStr, ok := ruleMap[assignment.RuleID]
		if !ok {
			continue
		}

		var config datarule.RuleConfig
		if err := json.Unmarshal([]byte(configStr), &config); err != nil {
			return nil, fmt.Errorf("解析数据规则 %d 配置失败: %w", assignment.RuleID, err)
		}
		result = append(result, config)
	}

	return result, nil
}

// RuleAssignmentList 查询规则分配列表。
func (s *Service) RuleAssignmentList(ctx context.Context, req *admindto.RuleAssignmentListReq) (res *admindto.RuleAssignmentListResp, err error) {
	// 按规则 ID 查询所有分配记录
	entities, err := s.dram.ListByRuleID(ctx, req.RuleID)
	if err != nil {
		return nil, err
	}

	// 组装响应，格式化时间字段
	list := make([]admindto.RuleAssignmentResp, 0, len(entities))
	for _, e := range entities {
		createTime := ""
		if e.CreateTime != nil {
			createTime = e.CreateTime.Format("2006-01-02 15:04:05")
		}
		list = append(list, admindto.RuleAssignmentResp{
			ID:          e.ID,
			RuleID:      e.RuleID,
			TargetType:  e.TargetType,
			TargetID:    e.TargetID,
			TargetScope: e.TargetScope,
			CreateTime:  createTime,
		})
	}

	return &admindto.RuleAssignmentListResp{List: list}, nil
}

// RuleAssignmentSave 批量保存规则分配（全量替换）。
func (s *Service) RuleAssignmentSave(ctx context.Context, req *admindto.RuleAssignmentSaveReq) error {
	seen := make(map[[2]uint64]struct{}, len(req.Assignments))
	now := time.Now()
	entities := make([]adminmodel.SysRuleAssignmentEntity, 0, len(req.Assignments))
	for _, a := range req.Assignments {
		if a.TargetID == 0 {
			return errors.New(adminenums.ErrInvalidAssignment)
		}

		targetScope := adminmodel.AssignmentTargetScopeNone
		switch a.TargetType {
		case adminmodel.AssignmentTargetTypeRole, adminmodel.AssignmentTargetTypeUser:
		case adminmodel.AssignmentTargetTypeDept:
			if a.TargetScope != adminmodel.AssignmentTargetScopeSelf &&
				a.TargetScope != adminmodel.AssignmentTargetScopeSelfAndChildren {
				return errors.New(adminenums.ErrInvalidAssignment)
			}
			targetScope = a.TargetScope
		default:
			return errors.New(adminenums.ErrInvalidAssignment)
		}

		key := [2]uint64{uint64(a.TargetType), a.TargetID}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		entities = append(entities, adminmodel.SysRuleAssignmentEntity{
			RuleID:      req.RuleID,
			TargetType:  a.TargetType,
			TargetID:    a.TargetID,
			TargetScope: targetScope,
			CreateTime:  &now,
		})
	}

	return s.dram.ReplaceByRuleID(ctx, req.RuleID, entities)
}
