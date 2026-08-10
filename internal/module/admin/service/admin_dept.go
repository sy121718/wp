package adminservice

import (
	"context"

	admindto "go_wp/internal/module/admin/dto"
	adminmodel "go_wp/internal/module/admin/model"
)

// ListByDeptID 按部门 ID 分页查询管理员列表。
func (s *Service) ListByDeptID(ctx context.Context, req *admindto.ListByDeptIDReq) (res *admindto.ListByDeptIDResp, err error) {
	page := req.GetPage()
	limit := req.GetLimit()

	query := s.am.DB(ctx).Where("dept_id = ?", req.DeptID)

	var total int64
	if err = query.Count(&total).Error; err != nil {
		return nil, err
	}

	var entities []adminmodel.AdminEntity
	offset := (page - 1) * limit
	if err = query.Select("id, username, name, email, status").
		Offset(offset).Limit(limit).
		Order("id ASC").
		Find(&entities).Error; err != nil {
		return nil, err
	}

	list := make([]admindto.DeptAdminItem, 0, len(entities))
	for _, e := range entities {
		name := ""
		if e.Name != nil {
			name = *e.Name
		}
		list = append(list, admindto.DeptAdminItem{
			ID:       e.ID,
			Username: e.Username,
			Name:     name,
			Email:    func() string { if e.Email != nil { return *e.Email }; return "" }(),
			Status:   e.Status,
		})
	}
	return &admindto.ListByDeptIDResp{Total: total, List: list}, nil
}

// BatchSetDeptID 批量设置用户的部门 ID。
func (s *Service) BatchSetDeptID(ctx context.Context, req *admindto.BatchSetDeptIDReq) error {
	if len(req.UserIDs) == 0 {
		return nil
	}
	return s.am.DB(ctx).Model(&adminmodel.AdminEntity{}).
		Where("id IN ?", req.UserIDs).
		Update("dept_id", req.DeptID).Error
}

// CountByDeptID 统计部门下管理员数量。
func (s *Service) CountByDeptID(ctx context.Context, deptID uint64) (int64, error) {
	var count int64
	err := s.am.DB(ctx).Where("dept_id = ?", deptID).Count(&count).Error
	return count, err
}