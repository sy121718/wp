package adminservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	adminmodel "go_wp/internal/module/admin/model"

	"go_wp/pkg/datarule"
	"gorm.io/gorm"
)

// GetRules 实现 datarule.RuleProvider 接口。
//
// **读路径零查询**：只读一份进程内快照（datarule_snapshot.go），在内存里按
// 「用户本人 / 用户角色 / 用户部门 / 上级部门 + SELF_AND_CHILDREN」过滤规则分配，
// 命中的规则按 id 去重后解析配置。快照由装配期加载一次、写路径同步重载、定时兜底刷新共同维护。
//
// 快照是**懒加载**的：快照为空（从未加载）时在这里加锁重建一次，之后一直复用，
// 直到写路径主动重载或 5 分钟兜底刷新把它换掉。
//
// fail-closed：加载失败时返回 error（让本次查询失败），绝不返回空规则集合 —— 空集合在引擎里
// 意味着「没有任何限制」，那是把数据权限静默关掉，比查询失败危险得多（改造前 provider
// 出错走 db.AddError(err)，同样是 fail-closed，这里保持同一语义）。
func (s *Service) GetRules(ctx context.Context, user *datarule.UserContext, domain string) ([]datarule.RuleConfig, error) {
	if user == nil || user.UserID == 0 {
		return nil, nil
	}

	snap := s.currentDataRuleSnapshot()
	if snap == nil {
		loaded, err := s.ensureDataRuleSnapshot(ctx)
		if err != nil {
			return nil, fmt.Errorf("加载数据权限规则快照失败: %w", err)
		}
		snap = loaded
	}

	domainRules, ok := snap.domains[domain]
	if !ok || domainRules == nil {
		return nil, nil // 该域没有启用规则：与快照语义一致，不是错误
	}

	// 用户角色 code → 启用角色 id（快照里只保留 status=启用 的角色）。
	roleIDs := make([]uint64, 0, len(user.Roles))
	for _, code := range user.Roles {
		if id, ok := snap.roleIDs[code]; ok {
			roleIDs = append(roleIDs, id)
		}
	}
	// 用户部门的祖先链（快照解析，替代改造前的 AncestorIDs 查库）。
	ancestorDeptIDs := snap.deptAncestors[user.DeptID]

	matchedRuleIDs := matchAssignmentRuleIDs(
		domainRules.assignments, user.UserID, user.DeptID, roleIDs, ancestorDeptIDs,
	)
	if len(matchedRuleIDs) == 0 {
		return nil, nil
	}

	configByID := make(map[uint64]string, len(domainRules.rules))
	for _, rule := range domainRules.rules {
		configByID[rule.id] = rule.config
	}

	result := make([]datarule.RuleConfig, 0, len(matchedRuleIDs))
	for _, ruleID := range matchedRuleIDs {
		configStr, ok := configByID[ruleID]
		if !ok {
			continue
		}
		var config datarule.RuleConfig
		if err := json.Unmarshal([]byte(configStr), &config); err != nil {
			return nil, fmt.Errorf("解析数据规则 %d 配置失败: %w", ruleID, err)
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
	// 校验规则存在：避免产生孤儿分配记录（GetRules 对未命中 ruleMap 静默 continue）。
	if _, err := s.drm.GetByID(ctx, req.RuleID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New(adminenums.ErrRuleNotFound)
		}
		return err
	}
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

	if err := s.dram.ReplaceByRuleID(ctx, req.RuleID, entities); err != nil {
		return err
	}
	// 分配已提交：同步重载快照，让新分配对下一次查询立即生效（不等 5 分钟兜底刷新）。
	s.reloadDataRuleSnapshotAfterWrite("规则分配保存")
	return nil
}
