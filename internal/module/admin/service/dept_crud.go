package adminservice

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	adminmodel "go_wp/internal/module/admin/model"

	"gorm.io/gorm"

	"go_wp/pkg/tree"
)

func (s *Service) DeptPage(ctx context.Context, page, limit int, keyword string) (*admindto.DeptPageResp, error) {
	parents, err := s.dm.ListParentOptions(ctx)
	if err != nil {
		return nil, err
	}
	total, list, err := s.dm.ListPage(ctx, page, limit, keyword)
	if err != nil {
		return nil, err
	}
	byID := make(map[uint64]adminmodel.DeptParentOption, len(parents))
	for _, p := range parents {
		byID[p.ID] = p
	}
	resp := &admindto.DeptPageResp{Total: total, Rows: make([]admindto.DeptPageRow, 0, len(list)), Parents: make([]admindto.DeptParentChoice, 0, len(parents))}
	for _, p := range parents {
		depth := 0
		seen := map[uint64]bool{p.ID: true}
		for id := p.ParentID; id != 0 && depth < 8; {
			parent, ok := byID[id]
			if !ok || seen[id] {
				break
			}
			seen[id] = true
			depth++
			id = parent.ParentID
		}
		resp.Parents = append(resp.Parents, admindto.DeptParentChoice{ID: p.ID, DeptName: p.DeptName, Indent: strings.Repeat("　", depth)})
	}
	for _, item := range list {
		parentTitle := ""
		if parent, ok := byID[item.ParentID]; ok {
			parentTitle = parent.DeptName
		}
		remark := ""
		if item.Remark != nil {
			remark = *item.Remark
		}
		resp.Rows = append(resp.Rows, admindto.DeptPageRow{
			ID: item.ID, DeptName: item.DeptName, DeptCode: item.DeptCode,
			ParentID: item.ParentID, ParentTitle: parentTitle, Status: item.Status, SortOrder: item.SortOrder, Remark: remark,
		})
	}
	return resp, nil
}

func (s *Service) DeptTree(ctx context.Context) ([]admindto.DeptTreeNode, error) {
	all, err := s.dm.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	return buildDeptTree(all), nil
}

func (s *Service) DeptDetail(ctx context.Context, req *admindto.DeptDetailReq) (*admindto.DeptTreeNode, error) {
	entity, err := s.dm.GetByID(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if entity == nil {
		return nil, adminenums.Err(adminenums.ErrDeptNotFound)
	}
	node := deptEntityToNode(*entity)
	return &node, nil
}

func (s *Service) AncestorIDs(ctx context.Context, deptID uint64) (ids []uint64, err error) {
	entity, err := s.dm.GetByID(ctx, deptID)
	if err != nil {
		return nil, err
	}
	if entity == nil {
		return nil, adminenums.Err(adminenums.ErrDeptNotFound)
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

func (s *Service) DeptCreate(ctx context.Context, req *admindto.DeptCreateReq) error {

	existing, err := s.dm.GetByCode(ctx, req.DeptCode)
	if err != nil {
		return err
	}
	if existing != nil {
		return adminenums.Err(adminenums.ErrDeptCodeExists)
	}

	ancestors := "0"
	if req.ParentID != 0 {
		parent, err := s.dm.GetByID(ctx, req.ParentID)
		if err != nil {
			return err
		}
		if parent == nil {
			return adminenums.Err(adminenums.ErrDeptNotFound)
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

	s.reloadDataRuleSnapshotAfterWrite("部门新建")
	return nil
}

func (s *Service) DeptUpdate(ctx context.Context, req *admindto.DeptUpdateReq) error {
	updateErr := s.dm.Transaction(ctx, func(tx *gorm.DB) error {
		current, err := s.dm.LockByIDTx(ctx, tx, req.ID)
		if err != nil {
			return err
		}
		if current == nil {
			return adminenums.Err(adminenums.ErrDeptNotFound)
		}

		if req.DeptCode != current.DeptCode {
			existing, err := s.dm.GetByCodeTx(ctx, tx, req.DeptCode)
			if err != nil {
				return err
			}
			if existing != nil && existing.ID != req.ID {
				return adminenums.Err(adminenums.ErrDeptCodeExists)
			}
		}

		moved := req.ParentID != current.ParentID
		oldAncestors := current.Ancestors
		if moved {
			if req.ParentID == req.ID {
				return adminenums.Err(adminenums.ErrDeptCircle)
			}
			if req.ParentID != 0 {
				if err = s.deptCheckCircleTx(ctx, tx, req.ID, req.ParentID); err != nil {
					return err
				}
			}

			if req.ParentID == 0 {
				current.Ancestors = "0"
			} else {
				parent, err := s.dm.GetByIDTx(ctx, tx, req.ParentID)
				if err != nil {
					return err
				}
				if parent == nil {
					return adminenums.Err(adminenums.ErrDeptNotFound)
				}
				current.Ancestors = parent.Ancestors + "," + fmt.Sprintf("%d", parent.ID)
			}
			current.ParentID = req.ParentID
		}

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

	s.reloadDataRuleSnapshotAfterWrite("部门更新")
	return nil
}

func (s *Service) DeptDelete(ctx context.Context, req *admindto.DeptDeleteReq) error {
	entity, err := s.dm.GetByID(ctx, req.ID)
	if err != nil {
		return err
	}
	if entity == nil {
		return adminenums.Err(adminenums.ErrDeptNotFound)
	}

	childCount, err := s.dm.CountByParentID(ctx, req.ID)
	if err != nil {
		return err
	}
	if childCount > 0 {
		return adminenums.Err(adminenums.ErrDeptHasChildren)
	}

	userCount, err := s.AdminCountByDeptID(ctx, req.ID)
	if err != nil {
		return err
	}
	if userCount > 0 {
		return adminenums.Err(adminenums.ErrDeptHasUsers)
	}

	if err := s.dm.Delete(ctx, req.ID); err != nil {
		return err
	}

	s.reloadDataRuleSnapshotAfterWrite("部门删除")
	return nil
}

func (s *Service) DeptUserList(ctx context.Context, req *admindto.DeptUserListReq) (resp *admindto.AdminListByDeptIDResp, err error) {
	listReq := &admindto.AdminListByDeptIDReq{
		DeptID: req.DeptID,
	}
	listReq.Page = req.GetPage()
	listReq.Limit = req.GetLimit()

	return s.AdminListByDeptID(ctx, listReq)
}

func (s *Service) DeptUserSave(ctx context.Context, req *admindto.DeptUserSaveReq) error {
	return s.AdminBatchSetDeptID(ctx, &admindto.AdminBatchSetDeptIDReq{
		DeptID:  req.DeptID,
		UserIDs: req.UserIDs,
	})
}

func (s *Service) deptCheckCircleTx(ctx context.Context, tx *gorm.DB, targetID, newParentID uint64) error {
	cursor := newParentID
	for cursor != 0 {
		if cursor == targetID {
			return adminenums.Err(adminenums.ErrDeptCircle)
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

func buildDeptTree(list []adminmodel.DeptEntity) []admindto.DeptTreeNode {
	ptrs := make([]*admindto.DeptTreeNode, 0, len(list))
	for _, d := range list {
		n := deptEntityToNode(d)
		ptrs = append(ptrs, &n)
	}
	roots := tree.Build(ptrs)
	out := make([]admindto.DeptTreeNode, 0, len(roots))
	for _, r := range roots {
		out = append(out, *r)
	}
	return out
}

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
