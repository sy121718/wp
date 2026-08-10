package permissionservice

import (
	"context"

	permissiondto "go_wp/internal/module/permission/dto"
)

// List 权限点分页查询。
func (s *Service) List(ctx context.Context, req *permissiondto.ListReq) (res *permissiondto.ListResp, err error) {
	query := s.pm.DB(ctx)

	if req.Module != "" {
		query = query.Where("module = ?", req.Module)
	}
	if req.Code != "" {
		query = query.Where("permission_code LIKE ?", "%"+req.Code+"%")
	}
	if req.APIPath != "" {
		query = query.Where("api_path LIKE ?", "%"+req.APIPath+"%")
	}
	if req.Status != nil {
		query = query.Where("status = ?", *req.Status)
	}

	var total int64
	if err = query.Count(&total).Error; err != nil {
		return nil, err
	}

	var items []permissiondto.PermissionItem
	offset := (req.GetPage() - 1) * req.GetLimit()
	err = query.Order("id DESC").
		Offset(offset).Limit(req.GetLimit()).
		Scan(&items).Error
	if err != nil {
		return nil, err
	}

	// 补齐 remark 的空值处理
	list := make([]permissiondto.PermissionItem, 0, len(items))
	for _, item := range items {
		list = append(list, item)
	}

	return &permissiondto.ListResp{Total: total, List: list}, nil
}