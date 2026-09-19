package adminservice

import (
	"context"
	"errors"
	"fmt"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	adminmodel "go_wp/internal/module/admin/model"
	"go_wp/pkg/casbin"
	"go_wp/pkg/database"

	"gorm.io/gorm"
)

// PermList 权限点分页查询。
func (s *Service) PermList(ctx context.Context, req *admindto.PermListReq) (res *admindto.PermListResp, err error) {
	query := s.pm.DB(ctx)

	if req.Module != "" {
		query = query.Where("module = ?", req.Module)
	}
	if req.Code != "" {
		query = query.Where("permission_code LIKE ? ESCAPE '\\'", "%"+database.EscapeLikePattern(req.Code)+"%")
	}
	if req.APIPath != "" {
		query = query.Where("api_path LIKE ? ESCAPE '\\'", "%"+database.EscapeLikePattern(req.APIPath)+"%")
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
//
// 事务：sys_permission 行与 sys_casbin_rule 里该权限点的 p 行是**两处持久化写入**，
// 必须同事务（见 AGENTS.md「写操作的事务与回滚」）。旧实现分两次提交、失败时手工回滚
// 权限点行，手工回滚本身再失败就会留下「旧策略已删、新策略没写完」——该权限点连带
// 超管全员 403（072/077/078/079 踩过的同一形态）。现在两处写在一个事务里，
// 任一步失败由数据库整体回滚，不再需要补偿。
//
// 读-改-写加行锁（LockByIDTx）：先读旧定义、再算新定义、再写回，不加锁会与并发更新互相覆盖。
// 策略行的刷新是派生动作：事务提交成功后重建 Enforcer 内存副本（回滚时不动副本，两者一致）。
func (s *Service) PermUpdate(ctx context.Context, req *admindto.PermUpdateReq) (res *admindto.PermUpdateResp, err error) {
	reloadPolicy := false
	err = s.pm.Transaction(ctx, func(tx *gorm.DB) error {
		entity, err := s.pm.LockByIDTx(ctx, tx, req.ID)
		if err != nil {
			return err
		}
		if entity == nil {
			return errors.New(adminenums.ErrPermissionNotFound)
		}
		if req.PermissionCode != entity.PermissionCode {
			return errors.New(adminenums.ErrCodeImmutable)
		}

		assigned, err := casbin.HasPermissionPolicies(entity.PermissionCode)
		if err != nil {
			return err
		}
		if req.Status == adminmodel.PermissionStatusDisabled && assigned {
			return errors.New(adminenums.ErrPermissionAssigned)
		}

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

		if err = s.pm.UpdateTx(ctx, tx, entity); err != nil {
			return err
		}
		if !definitionChanged || !assigned {
			return nil
		}
		changed, err := casbin.ReplacePermissionDefinitionTx(ctx, tx, entity.PermissionCode, entity.APIPath, entity.APIMethod)
		if err != nil {
			return fmt.Errorf("权限点已改但策略同步失败，本次修改已整体回滚: %w", err)
		}
		reloadPolicy = changed
		return nil
	})
	if err != nil {
		return nil, err
	}
	if reloadPolicy {
		if err = casbin.ReloadPolicy(); err != nil {
			return nil, fmt.Errorf("权限点已保存，但权限策略重载失败（内存副本仍是旧策略，请重试或重启服务）: %w", err)
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
