package adminservice

import (
	"context"
	"strings"

	admindto "go_wp/internal/module/admin/dto"
	adminmodel "go_wp/internal/module/admin/model"
)

func (s *Service) List(c context.Context, req *admindto.ListReq) (res *admindto.ListResp, err error) {
	//返回的总条数，go需要提前准备容器
	var total int64
	page := req.GetPage()
	limit := req.GetLimit()

	//默认排除超管
	query := s.am.DB(c).Where("is_admin != ?", 1)

	if email := strings.TrimSpace(req.Email); email != "" {
		query = query.Where("email LIKE ?", "%"+email+"%")
	}
	if name := strings.TrimSpace(req.Name); name != "" {
		query = query.Where("name LIKE ?", "%"+name+"%")
	}
	if req.Status != nil {
		query = query.Where("status = ?", *req.Status)
	}

	if err = query.Count(&total).Error; err != nil {
		return nil, err
	}

	orderClause := "id DESC"
	if req.SortField != "" && req.SortOrder != "" {
		orderClause = string(req.SortField) + " " + strings.ToUpper(string(req.SortOrder))
	}

	list := make([]adminmodel.AdminEntity, 0, limit)
	if err = query.
		Order(orderClause).
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&list).Error; err != nil {
		return nil, err
	}

	items := make([]admindto.AdminItem, len(list))
	for i, entity := range list {
		items[i] = admindto.AdminItem{
			ID:         entity.ID,
			Username:   entity.Username,
			Name:       entity.Name,
			Avatar:     entity.Avatar,
			Email:      entity.Email,
			Phone:      entity.Phone,
			Status:     entity.Status,
			CreateTime: entity.CreateTime,
		}
	}

	res = &admindto.ListResp{
		Total: total,
		List:  items,
	}

	return
}
