// Package dataruleservice datarule 模块业务逻辑实现。
package dataruleservice

import (
	"context"
	"errors"
	"time"

	dataruledto "go_wp/internal/module/datarule/dto"
	dataruleenums "go_wp/internal/module/datarule/enums"
	datamodel "go_wp/internal/module/datarule/model"
)

// AssignmentList 查询规则分配列表。
func (s *Service) AssignmentList(ctx context.Context, req *dataruledto.AssignmentListReq) (res *dataruledto.AssignmentListResp, err error) {
	// 按规则 ID 查询所有分配记录
	entities, err := s.ram.ListByRuleID(ctx, req.RuleID)
	if err != nil {
		return nil, err
	}

	// 组装响应，格式化时间字段
	list := make([]dataruledto.AssignmentResp, 0, len(entities))
	for _, e := range entities {
		createTime := ""
		if e.CreateTime != nil {
			createTime = e.CreateTime.Format("2006-01-02 15:04:05")
		}
		list = append(list, dataruledto.AssignmentResp{
			ID:          e.ID,
			RuleID:      e.RuleID,
			TargetType:  e.TargetType,
			TargetID:    e.TargetID,
			TargetScope: e.TargetScope,
			CreateTime:  createTime,
		})
	}

	return &dataruledto.AssignmentListResp{List: list}, nil
}

// AssignmentSave 批量保存规则分配（全量替换）。
func (s *Service) AssignmentSave(ctx context.Context, req *dataruledto.AssignmentSaveReq) error {
	seen := make(map[[2]uint64]struct{}, len(req.Assignments))
	now := time.Now()
	entities := make([]datamodel.SysRuleAssignmentEntity, 0, len(req.Assignments))
	for _, a := range req.Assignments {
		if a.TargetID == 0 {
			return errors.New(dataruleenums.ErrInvalidAssignment)
		}

		targetScope := datamodel.AssignmentTargetScopeNone
		switch a.TargetType {
		case datamodel.AssignmentTargetTypeRole, datamodel.AssignmentTargetTypeUser:
		case datamodel.AssignmentTargetTypeDept:
			if a.TargetScope != datamodel.AssignmentTargetScopeSelf &&
				a.TargetScope != datamodel.AssignmentTargetScopeSelfAndChildren {
				return errors.New(dataruleenums.ErrInvalidAssignment)
			}
			targetScope = a.TargetScope
		default:
			return errors.New(dataruleenums.ErrInvalidAssignment)
		}

		key := [2]uint64{uint64(a.TargetType), a.TargetID}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		entities = append(entities, datamodel.SysRuleAssignmentEntity{
			RuleID:      req.RuleID,
			TargetType:  a.TargetType,
			TargetID:    a.TargetID,
			TargetScope: targetScope,
			CreateTime:  &now,
		})
	}

	return s.ram.ReplaceByRuleID(ctx, req.RuleID, entities)
}
