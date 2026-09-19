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

	"gorm.io/gorm"
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

	if err := s.dm.Create(ctx, entity); err != nil {
		return err
	}
	// 部门已落库：同步重载数据权限快照（快照内含部门祖先链与子树，供规则分配匹配与
	// dept.scope:SELF_AND_CHILDREN 展开使用）。
	s.reloadDataRuleSnapshotAfterWrite("部门新建")
	return nil
}

// DeptUpdate 更新部门（含移动节点 ancestors 维护）。
//
// 事务：移动节点要写「自身行 + 全部子孙行」两处持久化数据，必须同事务。旧实现自身 Update 与
// 子孙 UpdateAncestors 分两次提交：子孙祖先链更新失败时自身已经改完，库里留下「父变了、子孙
// 还在旧祖先链」的树 —— pkg/datarule 的部门范围（SELF_AND_CHILDREN）会匹配到错误的部门集合
// （多看到 / 少看到别的部门数据），而且这个过程不报错。现在任一步失败整体回滚。
//
// 顺带修掉同一实体的两次 Update：旧实现在父级变更分支里先 Update 一次（ancestors/parent_id），
// 末尾再 Update 一次（其余字段），两次之间失败会留下半截行。这里合并为一次 UpdateTx。
//
// 读-改-写加行锁（LockByIDTx）：先读旧 ancestors 才能算子孙前缀，不加锁并发移动同一子树会按
// 各自的旧快照改写，把祖先链写错。
func (s *Service) DeptUpdate(ctx context.Context, req *admindto.DeptUpdateReq) error {
	updateErr := s.dm.Transaction(ctx, func(tx *gorm.DB) error {
		current, err := s.dm.LockByIDTx(ctx, tx, req.ID)
		if err != nil {
			return err
		}
		if current == nil {
			return errors.New(adminenums.ErrDeptNotFound)
		}

		// 编码变更检查（同事务内读；并发插入由 sys_dept.dept_code 唯一索引兜底，不自动加后缀）
		if req.DeptCode != current.DeptCode {
			existing, err := s.dm.GetByCodeTx(ctx, tx, req.DeptCode)
			if err != nil {
				return err
			}
			if existing != nil && existing.ID != req.ID {
				return errors.New(adminenums.ErrDeptCodeExists)
			}
		}

		// 父级变更：防止环 + 维护 ancestors（自身与子孙一起改）
		moved := req.ParentID != current.ParentID
		oldAncestors := current.Ancestors
		if moved {
			if req.ParentID == req.ID {
				return errors.New(adminenums.ErrDeptCircle)
			}
			if req.ParentID != 0 {
				if err = s.deptCheckCircleTx(ctx, tx, req.ID, req.ParentID); err != nil {
					return err
				}
			}

			// 计算新 ancestors
			if req.ParentID == 0 {
				current.Ancestors = "0"
			} else {
				parent, err := s.dm.GetByIDTx(ctx, tx, req.ParentID)
				if err != nil {
					return err
				}
				if parent == nil {
					return errors.New(adminenums.ErrDeptNotFound)
				}
				current.Ancestors = parent.Ancestors + "," + fmt.Sprintf("%d", parent.ID)
			}
			current.ParentID = req.ParentID
		}

		// 基本字段（与 ancestors/parent_id 一起，只写一次）
		current.DeptName = req.DeptName
		current.DeptCode = req.DeptCode
		current.LeaderID = req.LeaderID
		current.SortOrder = req.SortOrder
		current.Status = req.Status
		if req.Remark != "" {
			current.Remark = &req.Remark
		} else {
			current.Remark = nil
		}

		if err = s.dm.UpdateTx(ctx, tx, current); err != nil {
			return err
		}
		if !moved {
			return nil
		}

		// 批量更新子孙 ancestors（旧前缀 → 新前缀），与自身行同事务
		oldPrefix := oldAncestors + "," + fmt.Sprintf("%d", current.ID)
		newPrefix := current.Ancestors + "," + fmt.Sprintf("%d", current.ID)
		if err = s.dm.UpdateAncestorsTx(ctx, tx, oldPrefix, newPrefix); err != nil {
			return fmt.Errorf("部门已移动但子孙祖先链更新失败，本次移动已整体回滚: %w", err)
		}
		return nil
	})
	if err := updateErr; err != nil {
		return err
	}
	// 部门已更新（可能移动了子树）：同步重载数据权限快照，祖先链变化立刻生效。
	s.reloadDataRuleSnapshotAfterWrite("部门更新")
	return nil
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

	if err := s.dm.Delete(ctx, req.ID); err != nil {
		return err
	}
	// 部门已删除：同步重载数据权限快照，避免已删部门继续出现在祖先链 / 子树里。
	s.reloadDataRuleSnapshotAfterWrite("部门删除")
	return nil
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

// deptCheckCircleTx 检查将 targetID 的 parent 设为 newParentID 是否形成环，
// 沿父链的读取与 DeptUpdate 的写在同一个事务里（看到一致快照）。
func (s *Service) deptCheckCircleTx(ctx context.Context, tx *gorm.DB, targetID, newParentID uint64) error {
	cursor := newParentID
	for cursor != 0 {
		if cursor == targetID {
			return errors.New(adminenums.ErrDeptCircle)
		}
		parent, err := s.dm.GetByIDTx(ctx, tx, cursor)
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
