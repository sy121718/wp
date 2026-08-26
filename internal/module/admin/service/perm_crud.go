package adminservice

import (
	"context"
	"errors"
	"fmt"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	adminmodel "go_wp/internal/module/admin/model"
	"go_wp/pkg/casbin"

	"gorm.io/gorm"
)

// PermList 权限点分页查询。
func (s *Service) PermList(ctx context.Context, req *admindto.PermListReq) (res *admindto.PermListResp, err error) {
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

	var items []admindto.PermItem
	offset := (req.GetPage() - 1) * req.GetLimit()
	err = query.Order("id DESC").
		Offset(offset).Limit(req.GetLimit()).
		Scan(&items).Error
	if err != nil {
		return nil, err
	}

	// 补齐 remark 的空值处理
	list := make([]admindto.PermItem, 0, len(items))
	for _, item := range items {
		list = append(list, item)
	}

	return &admindto.PermListResp{Total: total, List: list}, nil
}

// PermDetail 查询单个权限点详情。
func (s *Service) PermDetail(ctx context.Context, req *admindto.PermDetailReq) (res *admindto.PermDetailResp, err error) {
	res = &admindto.PermDetailResp{}
	err = s.pm.DB(ctx).
		Select("id", "permission_code", "permission_name", "module", "api_path", "api_method",
			"status", "remark", "create_time", "update_time").
		Where("id = ?", req.ID).
		Scan(res).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(adminenums.ErrPermissionNotFound)
		}
		return nil, err
	}
	if res.ID == 0 {
		return nil, errors.New(adminenums.ErrPermissionNotFound)
	}
	return res, nil
}

// PermOptions 返回启用权限选项（供菜单表单选择 permission_code）。
func (s *Service) PermOptions(ctx context.Context, req *admindto.PermOptionsReq) (res *admindto.PermOptionsResp, err error) {
	var entities []adminmodel.PermissionEntity
	query := s.pm.DB(ctx).Where("status = ?", adminmodel.PermissionStatusEnabled)
	if req.Module != "" {
		query = query.Where("module = ?", req.Module)
	}
	err = query.Order("module ASC, id ASC").Find(&entities).Error
	if err != nil {
		return nil, err
	}

	list := make([]admindto.PermOptionItem, 0, len(entities))
	for _, e := range entities {
		list = append(list, admindto.PermOptionItem{
			ID:             e.ID,
			PermissionCode: e.PermissionCode,
			PermissionName: e.PermissionName,
			Module:         e.Module,
			APIPath:        e.APIPath,
			APIMethod:      e.APIMethod,
		})
	}
	return &admindto.PermOptionsResp{List: list}, nil
}

// PermCreate 新建权限点。
func (s *Service) PermCreate(ctx context.Context, req *admindto.PermCreateReq) (res *admindto.PermCreateResp, err error) {
	// 检查 code 唯一性
	existing, err := s.pm.GetByCode(ctx, req.PermissionCode)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, errors.New(adminenums.ErrCodeExists)
	}

	entity := &adminmodel.PermissionEntity{
		PermissionCode: req.PermissionCode,
		PermissionName: req.PermissionName,
		Module:         req.Module,
		APIPath:        req.APIPath,
		APIMethod:      req.APIMethod,
		Status:         req.Status,
	}
	if entity.Status == 0 {
		entity.Status = adminmodel.PermissionStatusEnabled
	}
	if req.Remark != "" {
		entity.Remark = &req.Remark
	}

	if err = s.pm.Create(ctx, entity); err != nil {
		return nil, err
	}

	return &admindto.PermCreateResp{ID: entity.ID}, nil
}

// PermUpdate 更新权限点定义，并同步已分配的 Casbin 策略。
func (s *Service) PermUpdate(ctx context.Context, req *admindto.PermUpdateReq) (res *admindto.PermUpdateResp, err error) {
	entity, err := s.pm.GetByID(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if entity == nil {
		return nil, errors.New(adminenums.ErrPermissionNotFound)
	}

	if req.PermissionCode != entity.PermissionCode {
		return nil, errors.New(adminenums.ErrCodeImmutable)
	}

	assigned, err := casbin.HasPermissionPolicies(entity.PermissionCode)
	if err != nil {
		return nil, err
	}
	if req.Status == adminmodel.PermissionStatusDisabled && assigned {
		return nil, errors.New(adminenums.ErrPermissionAssigned)
	}

	oldEntity := *entity
	definitionChanged := req.APIPath != entity.APIPath || req.APIMethod != entity.APIMethod
	entity.PermissionName = req.PermissionName
	entity.Module = req.Module
	entity.APIPath = req.APIPath
	entity.APIMethod = req.APIMethod
	entity.Status = req.Status
	if req.Remark != "" {
		entity.Remark = &req.Remark
	} else {
		entity.Remark = nil
	}

	if err = s.pm.Update(ctx, entity); err != nil {
		return nil, err
	}
	if definitionChanged && assigned {
		if err = casbin.ReplacePermissionDefinition(entity.PermissionCode, entity.APIPath, entity.APIMethod); err != nil {
			if rollbackErr := s.pm.Update(ctx, &oldEntity); rollbackErr != nil {
				return nil, fmt.Errorf("同步 Casbin 策略失败: %v；恢复权限点失败: %w", err, rollbackErr)
			}
			return nil, err
		}
	}

	return &admindto.PermUpdateResp{ID: req.ID}, nil
}

// PermDelete 批量删除未分配的权限点。
func (s *Service) PermDelete(ctx context.Context, req *admindto.PermDeleteReq) (res *admindto.PermDeleteResp, err error) {
	entities, err := s.pm.ListByIDs(ctx, req.IDs)
	if err != nil {
		return nil, err
	}
	if len(entities) != len(req.IDs) {
		return nil, errors.New(adminenums.ErrPermissionNotFound)
	}
	codes := make([]string, 0, len(entities))
	for _, entity := range entities {
		assigned, err := casbin.HasPermissionPolicies(entity.PermissionCode)
		if err != nil {
			return nil, err
		}
		if assigned {
			return nil, errors.New(adminenums.ErrPermissionAssigned)
		}
		codes = append(codes, entity.PermissionCode)
	}

	// 同包直调菜单引用检查，不再有未初始化问题
	references, err := s.CountByPermissionCodes(ctx, codes)
	if err != nil {
		return nil, err
	}
	if references > 0 {
		return nil, errors.New(adminenums.ErrMenuReferenced)
	}

	deleted, err := s.pm.DeleteByIDs(ctx, req.IDs)
	if err != nil {
		return nil, err
	}
	return &admindto.PermDeleteResp{DeletedCount: deleted}, nil
}

// ListByCodes 按 permission_code 列表批量查权限摘要。
// 供 menu/role/admin 把 code 转换为 path/method。
func (s *Service) ListByCodes(ctx context.Context, codes []string) ([]admindto.PermBrief, error) {
	entities, err := s.pm.ListByCodes(ctx, codes)
	if err != nil {
		return nil, err
	}

	result := make([]admindto.PermBrief, 0, len(entities))
	for _, e := range entities {
		result = append(result, admindto.PermBrief{
			PermissionCode: e.PermissionCode,
			APIPath:        e.APIPath,
			APIMethod:      e.APIMethod,
		})
	}
	return result, nil
}

// ExistsEnabledCode 检查 code 是否存在且启用。
func (s *Service) ExistsEnabledCode(ctx context.Context, code string) (bool, error) {
	return s.pm.ExistsEnabledCode(ctx, code)
}
