package adminservice

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	adminmodel "go_wp/internal/module/admin/model"
)

// DeptTree 查询完整部门树。
func (s *Service) DeptTree(ctx context.Context) ([]admindto.DeptTreeNode, error) {
	all, err := s.dm.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	return buildDeptTree(all), nil
}

// DeptDetail 部门详情。
func (s *Service) DeptDetail(ctx context.Context, req *admindto.DeptDetailReq) (*admindto.DeptTreeNode, error) {
	entity, err := s.dm.GetByID(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if entity == nil {
		return nil, errors.New(adminenums.ErrDeptNotFound)
	}
	node := deptEntityToNode(*entity)
	return &node, nil
}

// AncestorIDs 返回指定部门的全部上级部门 ID，不包含 0 和当前部门。
func (s *Service) AncestorIDs(ctx context.Context, deptID uint64) (ids []uint64, err error) {
	entity, err := s.dm.GetByID(ctx, deptID)
	if err != nil {
		return nil, err
	}
	if entity == nil {
		return nil, errors.New(adminenums.ErrDeptNotFound)
	}

	for _, item := range strings.Split(entity.Ancestors, ",") {
		id, parseErr := strconv.ParseUint(strings.TrimSpace(item), 10, 64)
		if parseErr != nil || id == 0 {
			continue
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// DeptCreate 新建部门。
func (s *Service) DeptCreate(ctx context.Context, req *admindto.DeptCreateReq) error {
	// 检查编码唯一
	existing, err := s.dm.GetByCode(ctx, req.DeptCode)
	if err != nil {
		return err
	}
	if existing != nil {
		return errors.New(adminenums.ErrDeptCodeExists)
	}

	// 计算 ancestors
	ancestors := "0"
	if req.ParentID != 0 {
		parent, err := s.dm.GetByID(ctx, req.ParentID)
		if err != nil {
			return err
		}
		if parent == nil {
			return errors.New(adminenums.ErrDeptNotFound)
		}
		ancestors = parent.Ancestors + "," + fmt.Sprintf("%d", parent.ID)
	}

	entity := &adminmodel.DeptEntity{
		ParentID:  req.ParentID,
		Ancestors: ancestors,
		DeptName:  req.DeptName,
		DeptCode:  req.DeptCode,
		LeaderID:  req.LeaderID,
		SortOrder: req.SortOrder,
		Status:    req.Status,
	}
	if entity.Status == 0 {
		entity.Status = adminmodel.DeptStatusEnabled
	}
	if req.Remark != "" {
		entity.Remark = &req.Remark
	}

	return s.dm.Create(ctx, entity)
}

// DeptUpdate 更新部门（含移动节点 ancestors 维护）。
func (s *Service) DeptUpdate(ctx context.Context, req *admindto.DeptUpdateReq) error {
	entity, err := s.dm.GetByID(ctx, req.ID)
	if err != nil {
		return err
	}
	if entity == nil {
		return errors.New(adminenums.ErrDeptNotFound)
	}

	// 编码变更检查
	if req.DeptCode != entity.DeptCode {
		existing, err := s.dm.GetByCode(ctx, req.DeptCode)
		if err != nil {
			return err
		}
		if existing != nil && existing.ID != req.ID {
			return errors.New(adminenums.ErrDeptCodeExists)
		}
	}

	// 父级变更：防止环 + 维护 ancestors
	if req.ParentID != entity.ParentID {
		if req.ParentID == req.ID {
			return errors.New(adminenums.ErrDeptCircle)
		}
		if req.ParentID != 0 {
			if err := s.deptCheckCircle(ctx, req.ID, req.ParentID); err != nil {
				return err
			}
		}

		// 计算新 ancestors
		var newAncestors string
		if req.ParentID == 0 {
			newAncestors = "0"
		} else {
			parent, err := s.dm.GetByID(ctx, req.ParentID)
			if err != nil {
				return err
			}
			if parent == nil {
				return errors.New(adminenums.ErrDeptNotFound)
			}
			newAncestors = parent.Ancestors + "," + fmt.Sprintf("%d", parent.ID)
		}

		// 旧 ancestors 前缀 → 新前缀，批量改子孙
		oldAncestors := entity.Ancestors
		entity.ParentID = req.ParentID
		entity.Ancestors = newAncestors
		// 更新自身
		if err := s.dm.Update(ctx, entity); err != nil {
			return err
		}
		// 批量更新子孙 ancestors
		oldPrefix := oldAncestors + "," + fmt.Sprintf("%d", entity.ID)
		newPrefix := newAncestors + "," + fmt.Sprintf("%d", entity.ID)
		if err := s.dm.UpdateAncestors(ctx, oldPrefix, newPrefix); err != nil {
			return err
		}
	}

	// 更新基本字段
	entity.DeptName = req.DeptName
	entity.DeptCode = req.DeptCode
	entity.LeaderID = req.LeaderID
	entity.SortOrder = req.SortOrder
	entity.Status = req.Status
	if req.Remark != "" {
		entity.Remark = &req.Remark
	} else {
		entity.Remark = nil
	}

	return s.dm.Update(ctx, entity)
}

// DeptDelete 删除部门。
// 流程：1) 检查部门是否存在；2) 检查是否存在子部门，有则拒绝删除；3) 检查部门下是否有用户。
func (s *Service) DeptDelete(ctx context.Context, req *admindto.DeptDeleteReq) error {
	entity, err := s.dm.GetByID(ctx, req.ID)
	if err != nil {
		return err
	}
	if entity == nil {
		return errors.New(adminenums.ErrDeptNotFound)
	}

	// 检查子部门
	childCount, err := s.dm.CountByParentID(ctx, req.ID)
	if err != nil {
		return err
	}
	if childCount > 0 {
		return errors.New(adminenums.ErrDeptHasChildren)
	}

	// 检查部门下是否有用户（同包直调）
	userCount, err := s.AdminCountByDeptID(ctx, req.ID)
	if err != nil {
		return err
	}
	if userCount > 0 {
		return errors.New(adminenums.ErrDeptHasUsers)
	}

	return s.dm.Delete(ctx, req.ID)
}

// DeptUserList 查询部门下用户列表。
func (s *Service) DeptUserList(ctx context.Context, req *admindto.DeptUserListReq) (resp *admindto.AdminListByDeptIDResp, err error) {
	listReq := &admindto.AdminListByDeptIDReq{
		DeptID: req.DeptID,
	}
	listReq.Page = req.GetPage()
	listReq.Limit = req.GetLimit()

	return s.AdminListByDeptID(ctx, listReq)
}

// DeptUserSave 批量分配用户到部门。
func (s *Service) DeptUserSave(ctx context.Context, req *admindto.DeptUserSaveReq) error {
	return s.AdminBatchSetDeptID(ctx, &admindto.AdminBatchSetDeptIDReq{
		DeptID:  req.DeptID,
		UserIDs: req.UserIDs,
	})
}

// deptCheckCircle 检查将 targetID 的 parent 设为 newParentID 是否形成环。
func (s *Service) deptCheckCircle(ctx context.Context, targetID, newParentID uint64) error {
	cursor := newParentID
	for cursor != 0 {
		if cursor == targetID {
			return errors.New(adminenums.ErrDeptCircle)
		}
		parent, err := s.dm.GetByID(ctx, cursor)
		if err != nil {
			return err
		}
		if parent == nil {
			break
		}
		cursor = parent.ParentID
	}
	return nil
}

// buildDeptTree 将扁平部门列表组装为树形结构（多根节点）。
// 通过 parent_id 建立父子关系，返回根节点列表。
// Children 为指针存储，节点只挂载一次，避免值拷贝导致子孙丢失。
func buildDeptTree(list []adminmodel.DeptEntity) []admindto.DeptTreeNode {
	nodeMap := make(map[uint64]*admindto.DeptTreeNode, len(list))
	var roots []*admindto.DeptTreeNode

	for _, d := range list {
		node := deptEntityToNode(d)
		nodeMap[d.ID] = &node
	}

	for _, d := range list {
		node := nodeMap[d.ID]
		if d.ParentID == 0 {
			roots = append(roots, node)
		} else {
			if parent, ok := nodeMap[d.ParentID]; ok {
				parent.Children = append(parent.Children, node)
			} else {
				roots = append(roots, node)
			}
		}
	}

	result := make([]admindto.DeptTreeNode, 0, len(roots))
	for _, root := range roots {
		result = append(result, *root)
	}
	return result
}

// deptEntityToNode 将数据库实体转换为树节点 DTO。
// 处理 nil 指针字段（Remark）的默认值。
func deptEntityToNode(d adminmodel.DeptEntity) admindto.DeptTreeNode {
	remark := ""
	if d.Remark != nil {
		remark = *d.Remark
	}
	return admindto.DeptTreeNode{
		ID:        d.ID,
		ParentID:  d.ParentID,
		DeptName:  d.DeptName,
		DeptCode:  d.DeptCode,
		LeaderID:  d.LeaderID,
		SortOrder: d.SortOrder,
		Status:    d.Status,
		Remark:    remark,
	}
}
