// Package dataruleservice datarule 模块业务逻辑实现。
package dataruleservice

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	datamodel "go_wp/internal/module/datarule/model"
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
	if err := s.rm.DB(ctx).
		Select("id, config").
		Where("domain = ? AND status = ?", domain, 1).
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

	roleIDs, err := s.roleSvc.GetEnabledRoleIDsByCodes(ctx, user.Roles)
	if err != nil {
		return nil, err
	}

	var ancestorDeptIDs []uint64
	if user.DeptID != 0 {
		if s.deptSvc == nil {
			return nil, fmt.Errorf("部门服务未配置")
		}
		ancestorDeptIDs, err = s.deptSvc.AncestorIDs(ctx, user.DeptID)
		if err != nil {
			return nil, err
		}
	}

	predicates := []string{"target_type = ? AND target_id = ?"}
	args := []any{datamodel.AssignmentTargetTypeUser, user.UserID}
	if len(roleIDs) > 0 {
		predicates = append(predicates, "target_type = ? AND target_id IN ?")
		args = append(args, datamodel.AssignmentTargetTypeRole, roleIDs)
	}
	if user.DeptID != 0 {
		predicates = append(predicates, "target_type = ? AND target_id = ?")
		args = append(args, datamodel.AssignmentTargetTypeDept, user.DeptID)
	}
	if len(ancestorDeptIDs) > 0 {
		predicates = append(predicates, "target_type = ? AND target_id IN ? AND target_scope = ?")
		args = append(
			args,
			datamodel.AssignmentTargetTypeDept,
			ancestorDeptIDs,
			datamodel.AssignmentTargetScopeSelfAndChildren,
		)
	}

	type assignmentRow struct {
		RuleID uint64 `gorm:"column:rule_id"`
	}
	var assignments []assignmentRow
	if err := s.ram.DB(ctx).
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
