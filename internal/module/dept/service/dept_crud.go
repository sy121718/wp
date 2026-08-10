// Package deptservice 部门业务逻辑层。
// 提供部门的增删改查、树形结构管理，以及部门与用户的关联管理。
package deptservice

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	admindto "go_wp/internal/module/admin/dto"
	deptdto "go_wp/internal/module/dept/dto"
	deptenums "go_wp/internal/module/dept/enums"
	deptmodel "go_wp/internal/module/dept/model"
)

// Tree 查询完整部门树。
func (s *Service) Tree(ctx context.Context) ([]deptdto.TreeNode, error) {
	all, err := s.dm.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	return buildDeptTree(all), nil
}

// Detail 部门详情。
func (s *Service) Detail(ctx context.Context, req *deptdto.DetailReq) (*deptdto.TreeNode, error) {
	entity, err := s.dm.GetByID(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if entity == nil {
		return nil, errors.New(deptenums.ErrDeptNotFound)
	}
	node := entityToNode(*entity)
	return &node, nil
}

// AncestorIDs 返回指定部门的全部上级部门 ID，不包含 0 和当前部门。
func (s *Service) AncestorIDs(ctx context.Context, deptID uint64) (ids []uint64, err error) {
	entity, err := s.dm.GetByID(ctx, deptID)
	if err != nil {
		return nil, err
	}
	if entity == nil {
		return nil, errors.New(deptenums.ErrDeptNotFound)
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

// Create 新建部门。
func (s *Service) Create(ctx context.Context, req *deptdto.CreateReq) error {
	// 检查编码唯一
	existing, err := s.dm.GetByCode(ctx, req.DeptCode)
	if err != nil {
		return err
	}
	if existing != nil {
		return errors.New(deptenums.ErrDeptCodeExists)
	}

	// 计算 ancestors
	ancestors := "0"
	if req.ParentID != 0 {
		parent, err := s.dm.GetByID(ctx, req.ParentID)
		if err != nil {
			return err
		}
		if parent == nil {
			return errors.New(deptenums.ErrDeptNotFound)
		}
		ancestors = parent.Ancestors + "," + fmt.Sprintf("%d", parent.ID)
	}

	entity := &deptmodel.DeptEntity{
		ParentID:  req.ParentID,
		Ancestors: ancestors,
		DeptName:  req.DeptName,
		DeptCode:  req.DeptCode,
		LeaderID:  req.LeaderID,
		SortOrder: req.SortOrder,
		Status:    req.Status,
	}
	if entity.Status == 0 {
		entity.Status = deptmodel.DeptStatusEnabled
	}
	if req.Remark != "" {
		entity.Remark = &req.Remark
	}

	return s.dm.Create(ctx, entity)
}

// Update 更新部门（含移动节点 ancestors 维护）。
func (s *Service) Update(ctx context.Context, req *deptdto.UpdateReq) error {
	entity, err := s.dm.GetByID(ctx, req.ID)
	if err != nil {
		return err
	}
	if entity == nil {
		return errors.New(deptenums.ErrDeptNotFound)
	}

	// 编码变更检查
	if req.DeptCode != entity.DeptCode {
		existing, err := s.dm.GetByCode(ctx, req.DeptCode)
		if err != nil {
			return err
		}
		if existing != nil && existing.ID != req.ID {
			return errors.New(deptenums.ErrDeptCodeExists)
		}
	}

	// 父级变更：防止环 + 维护 ancestors
	if req.ParentID != entity.ParentID {
		if req.ParentID == req.ID {
			return errors.New(deptenums.ErrDeptCircle)
		}
		if req.ParentID != 0 {
			if err := s.checkCircle(ctx, req.ID, req.ParentID); err != nil {
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
				return errors.New(deptenums.ErrDeptNotFound)
			}
			newAncestors = parent.Ancestors + "," + fmt.Sprintf("%d", parent.ID)
		}

		// 旧 ancestors 前缀 → 新前缀，批量改子孙
		oldAncestors := entity.Ancestors
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

// Delete 删除部门。
// 流程：1) 检查部门是否存在；2) 检查是否存在子部门，有则拒绝删除；
// TODO: 检查部门下是否有用户（需要 admin contract），当前阶段暂不检查，后续注入后补充。
func (s *Service) Delete(ctx context.Context, req *deptdto.DeleteReq) error {
	entity, err := s.dm.GetByID(ctx, req.ID)
	if err != nil {
		return err
	}
	if entity == nil {
		return errors.New(deptenums.ErrDeptNotFound)
	}

	// 检查子部门
	childCount, err := s.dm.CountByParentID(ctx, req.ID)
	if err != nil {
		return err
	}
	if childCount > 0 {
		return errors.New(deptenums.ErrDeptHasChildren)
	}

	// 检查部门下是否有用户
	userCount, err := s.adminSvc.CountByDeptID(ctx, req.ID)
	if err != nil {
		return err
	}
	if userCount > 0 {
		return errors.New(deptenums.ErrDeptHasUsers)
	}

	return s.dm.Delete(ctx, req.ID)
}

// UserList 查询部门下用户列表。
func (s *Service) UserList(ctx context.Context, req *deptdto.UserListReq) (interface{}, error) {
	listReq := &admindto.ListByDeptIDReq{
		DeptID: req.DeptID,
	}
	listReq.Page = req.GetPage()
	listReq.Limit = req.GetLimit()

	resp, err := s.adminSvc.ListByDeptID(ctx, listReq)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// UserSave 批量分配用户到部门。
func (s *Service) UserSave(ctx context.Context, req *deptdto.UserSaveReq) error {
	return s.adminSvc.BatchSetDeptID(ctx, &admindto.BatchSetDeptIDReq{
		DeptID:  req.DeptID,
		UserIDs: req.UserIDs,
	})
}

// checkCircle 检查将 targetID 的 parent 设为 newParentID 是否形成环。
func (s *Service) checkCircle(ctx context.Context, targetID, newParentID uint64) error {
	cursor := newParentID
	for cursor != 0 {
		if cursor == targetID {
			return errors.New(deptenums.ErrDeptCircle)
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
func buildDeptTree(list []deptmodel.DeptEntity) []deptdto.TreeNode {
	nodeMap := make(map[uint64]*deptdto.TreeNode, len(list))
	var roots []deptdto.TreeNode

	for _, d := range list {
		node := entityToNode(d)
		nodeMap[d.ID] = &node
	}

	for _, d := range list {
		node := nodeMap[d.ID]
		if d.ParentID == 0 {
			roots = append(roots, *node)
		} else {
			if parent, ok := nodeMap[d.ParentID]; ok {
				parent.Children = append(parent.Children, *node)
			} else {
				roots = append(roots, *node)
			}
		}
	}

	return roots
}

// entityToNode 将数据库实体转换为树节点 DTO。
// 处理 nil 指针字段（Remark）的默认值。
func entityToNode(d deptmodel.DeptEntity) deptdto.TreeNode {
	remark := ""
	if d.Remark != nil {
		remark = *d.Remark
	}
	return deptdto.TreeNode{
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
