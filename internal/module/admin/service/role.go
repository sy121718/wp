package adminservice

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"

	"gorm.io/gorm"

	"go_wp/internal/module/admin/dto"
	"go_wp/internal/module/admin/enums"
	"go_wp/internal/module/admin/model"
	"go_wp/pkg/casbin"
	"go_wp/pkg/database"
)

var pureNumericPattern = regexp.MustCompile(`^[0-9]+$`)

func (s *Service) RoleList(ctx context.Context, req *admindto.RoleListReq) (res *admindto.RoleListResp, err error) {
	total, entities, err := s.rm.ListAll(ctx, req.GetPage(), req.GetLimit(), req.Keyword)
	if err != nil {
		return nil, err
	}

	list := make([]admindto.RoleItem, 0, len(entities))
	for _, e := range entities {
		list = append(list, roleEntityToItem(e))
	}
	return &admindto.RoleListResp{Total: total, List: list}, nil
}

func (s *Service) RoleDetail(ctx context.Context, req *admindto.RoleDetailReq) (res *admindto.RoleDetailResp, err error) {
	entity, err := s.rm.GetByID(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if entity == nil {
		return nil, adminenums.Err(adminenums.ErrRoleNotFound)
	}
	return roleEntityToDetailResp(entity), nil
}

func (s *Service) RoleCreate(ctx context.Context, req *admindto.RoleCreateReq) error {
	if pureNumericPattern.MatchString(req.RoleCode) {
		return adminenums.Err(adminenums.ErrRoleCodeNumeric)
	}

	existing, err := s.rm.GetByCode(ctx, req.RoleCode)
	if err != nil {
		return err
	}
	if existing != nil {
		return adminenums.Err(adminenums.ErrRoleCodeExists)
	}

	entity := &adminmodel.RoleEntity{
		RoleCode:  req.RoleCode,
		RoleName:  req.RoleName,
		SortOrder: req.SortOrder,
	}

	if req.Status != nil {
		entity.Status = *req.Status
	} else {
		entity.Status = adminmodel.RoleStatusEnabled
	}
	if req.Remark != "" {
		entity.Remark = &req.Remark
	}

	reloadPolicy := false
	err = s.rm.Transaction(ctx, func(tx *gorm.DB) error {
		if err := s.rm.CreateTx(ctx, tx, entity); err != nil {
			return err
		}

		if entity.Status != adminmodel.RoleStatusEnabled {
			return nil
		}
		changed, err := casbin.ActivateRoleTx(ctx, tx, entity.RoleCode)
		if err != nil {
			return fmt.Errorf("角色已建但启用标记写入失败，本次创建已整体回滚: %w", err)
		}
		reloadPolicy = changed
		return nil
	})
	if err != nil {
		return err
	}
	if reloadPolicy {
		if err = casbin.ReloadPolicy(); err != nil {
			return fmt.Errorf("角色已创建，但权限策略重载失败（内存副本仍是旧策略，请重试或重启服务）: %w", err)
		}
	}

	s.reloadDataRuleSnapshotAfterWrite("角色新建")
	return nil
}

func (s *Service) RoleUpdate(ctx context.Context, req *admindto.RoleUpdateReq) error {
	reloadPolicy := false
	err := s.rm.Transaction(ctx, func(tx *gorm.DB) error {
		entity, err := s.rm.LockByIDTx(ctx, tx, req.ID)
		if err != nil {
			return err
		}
		if entity == nil {
			return adminenums.Err(adminenums.ErrRoleNotFound)
		}

		oldStatus := entity.Status
		entity.RoleName = req.RoleName
		entity.Status = req.Status
		entity.SortOrder = req.SortOrder
		if req.Remark != "" {
			entity.Remark = &req.Remark
		} else {
			entity.Remark = nil
		}

		if err = s.rm.UpdateTx(ctx, tx, entity); err != nil {
			return err
		}

		if oldStatus == entity.Status {
			return nil
		}
		changed := false
		if entity.Status == adminmodel.RoleStatusEnabled {
			changed, err = casbin.ActivateRoleTx(ctx, tx, entity.RoleCode)
		} else {
			changed, err = casbin.DeactivateRoleTx(ctx, tx, entity.RoleCode)
		}
		if err != nil {
			return fmt.Errorf("角色状态已改但启用标记同步失败，本次修改已整体回滚: %w", err)
		}
		reloadPolicy = changed
		return nil
	})
	if err != nil {
		return err
	}
	if reloadPolicy {
		if err = casbin.ReloadPolicy(); err != nil {
			return fmt.Errorf("角色已保存，但权限策略重载失败（内存副本仍是旧策略，请重试或重启服务）: %w", err)
		}
	}

	s.reloadDataRuleSnapshotAfterWrite("角色更新")
	return nil
}

func (s *Service) RoleDelete(ctx context.Context, req *admindto.RoleDeleteReq) error {
	reloadPolicy := false
	err := s.rm.Transaction(ctx, func(tx *gorm.DB) error {
		entity, err := s.rm.LockByIDTx(ctx, tx, req.ID)
		if err != nil {
			return err
		}
		if entity == nil {
			return adminenums.Err(adminenums.ErrRoleNotFound)
		}
		if entity.IsSystem == 1 {
			return adminenums.Err(adminenums.ErrRoleIsSystem)
		}

		changed, err := casbin.DeleteRoleAllPoliciesTx(ctx, tx, entity.RoleCode)
		if err != nil {
			return fmt.Errorf("角色策略清理失败，本次删除已整体回滚: %w", err)
		}
		if err = s.rm.DeleteTx(ctx, tx, req.ID); err != nil {
			return err
		}
		reloadPolicy = changed
		return nil
	})
	if err != nil {
		return err
	}
	if reloadPolicy {
		if err = casbin.ReloadPolicy(); err != nil {
			return fmt.Errorf("角色已删除，但权限策略重载失败（内存副本仍是旧策略，请重试或重启服务）: %w", err)
		}
	}

	s.reloadDataRuleSnapshotAfterWrite("角色删除")
	return nil
}

func (s *Service) GetRoleCodesByUserID(ctx context.Context, userID uint64) ([]string, error) {
	codes, err := casbin.GetRoleCodesByUserID(strconv.FormatUint(userID, 10))
	if err != nil || len(codes) == 0 {
		return codes, err
	}

	roles, err := s.rm.ListByCodes(ctx, codes)
	if err != nil {
		return nil, err
	}
	enabled := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		if role.Status == adminmodel.RoleStatusEnabled {
			enabled[role.RoleCode] = struct{}{}
		}
	}

	activeCodes := make([]string, 0, len(enabled))
	for _, code := range codes {
		if _, ok := enabled[code]; ok {
			activeCodes = append(activeCodes, code)
		}
	}
	return activeCodes, nil
}

func roleEntityToItem(e adminmodel.RoleEntity) admindto.RoleItem {
	return admindto.RoleItem{
		ID:        e.ID,
		RoleCode:  e.RoleCode,
		RoleName:  e.RoleName,
		Status:    e.Status,
		IsSystem:  e.IsSystem,
		SortOrder: e.SortOrder,
		Remark:    roleRemark(e.Remark),
	}
}

func roleEntityToDetailResp(e *adminmodel.RoleEntity) *admindto.RoleDetailResp {
	return &admindto.RoleDetailResp{
		ID:        e.ID,
		RoleCode:  e.RoleCode,
		RoleName:  e.RoleName,
		Status:    e.Status,
		IsSystem:  e.IsSystem,
		SortOrder: e.SortOrder,
		Remark:    roleRemark(e.Remark),
	}
}

func roleRemark(remark *string) string {
	if remark != nil {
		return *remark
	}
	return ""
}

func (s *Service) RoleMenuList(ctx context.Context, req *admindto.RoleMenuListReq) (res *admindto.RoleMenuListResp, err error) {
	role, err := s.rm.GetByID(ctx, req.RoleID)
	if err != nil {
		return nil, err
	}
	if role == nil {
		return nil, adminenums.Err(adminenums.ErrRoleNotFound)
	}

	permissions, err := casbin.GetRolePermissions(role.RoleCode)
	if err != nil {
		return nil, err
	}

	codes := make([]string, 0, len(permissions))
	for _, p := range permissions {
		codes = append(codes, p[2])
	}

	menuIDs, err := s.GetIDsByPermissionCodes(ctx, codes)
	if err != nil {
		return nil, err
	}

	all, err := s.mm.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	menuIDs = withAncestorMenuIDs(all, menuIDs)

	return &admindto.RoleMenuListResp{MenuIDs: menuIDs}, nil
}

func (s *Service) RoleMenuSave(ctx context.Context, req *admindto.RoleMenuSaveReq) (res *admindto.RoleMenuSaveResp, err error) {
	role, err := s.rm.GetByID(ctx, req.RoleID)
	if err != nil {
		return nil, err
	}
	if role == nil {
		return nil, adminenums.Err(adminenums.ErrRoleNotFound)
	}

	all, err := s.mm.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	menuIDs := withAncestorMenuIDs(all, req.MenuIDs)

	codes, err := s.GetPermissionCodesByIDs(ctx, menuIDs)
	if err != nil {
		return nil, err
	}

	willBeSuper, err := s.codesCoverAllEnabled(ctx, codes)
	if err != nil {
		return nil, err
	}
	roleSuper, err := s.roleHasSuperAdminPermission(ctx, []string{role.RoleCode})
	if err != nil {
		return nil, err
	}
	if err = s.requireSuperAdminForSensitiveTarget(ctx, req.OperatorID, willBeSuper || roleSuper); err != nil {
		return nil, err
	}

	var policies [][3]string
	if len(codes) > 0 {
		briefs, err := s.ListByCodes(ctx, codes)
		if err != nil {
			return nil, err
		}
		for _, b := range briefs {
			policies = append(policies, [3]string{b.APIPath, b.APIMethod, b.PermissionCode})
		}
	}

	if err = casbin.ReplaceRolePermissions(role.RoleCode, policies); err != nil {
		return nil, fmt.Errorf("保存角色权限失败: %w", err)
	}

	return &admindto.RoleMenuSaveResp{RoleID: req.RoleID}, nil
}

func (s *Service) RoleUserList(ctx context.Context, req *admindto.RoleUserListReq) (res *admindto.RoleUserListResp, err error) {
	role, err := s.rm.GetByID(ctx, req.RoleID)
	if err != nil {
		return nil, err
	}
	if role == nil {
		return nil, adminenums.Err(adminenums.ErrRoleNotFound)
	}

	userIDStrs, err := casbin.GetUserIDsByRoleCode(role.RoleCode)
	if err != nil {
		return nil, err
	}

	userIDs := make([]uint64, 0, len(userIDStrs))
	for _, s := range userIDStrs {
		id, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			continue
		}
		userIDs = append(userIDs, id)
	}

	total := int64(len(userIDs))
	start := (req.GetPage() - 1) * req.GetLimit()
	end := start + req.GetLimit()
	if start > int(total) {
		start = int(total)
	}
	if end > int(total) {
		end = int(total)
	}
	pageIDs := userIDs[start:end]

	list := make([]admindto.RoleUserItem, 0, len(pageIDs))
	for _, id := range pageIDs {
		detail, err := s.AdminDetail(ctx, &admindto.AdminDetailReq{Id: id})
		if err != nil || detail == nil {

			list = append(list, admindto.RoleUserItem{ID: id})
			continue
		}
		list = append(list, admindto.RoleUserItem{
			ID:       id,
			Username: detail.Username,
			Name:     detail.Name,
			Email:    detail.Email,
			Status:   detail.Status,
		})
	}

	return &admindto.RoleUserListResp{Total: total, List: list}, nil
}

func (s *Service) RoleUserSave(ctx context.Context, req *admindto.RoleUserSaveReq) (res *admindto.RoleUserSaveResp, err error) {
	role, err := s.rm.GetByID(ctx, req.RoleID)
	if err != nil {
		return nil, err
	}
	if role == nil {
		return nil, adminenums.Err(adminenums.ErrRoleNotFound)
	}

	roleSuper, err := s.roleHasSuperAdminPermission(ctx, []string{role.RoleCode})
	if err != nil {
		return nil, err
	}
	targetSuper := false
	for _, id := range req.UserIDs {
		super, err := s.IsSuperAdmin(ctx, id)
		if err != nil {
			return nil, err
		}
		if super {
			targetSuper = true
			break
		}
	}
	if err = s.requireSuperAdminForSensitiveTarget(ctx, req.OperatorID, roleSuper || targetSuper); err != nil {
		return nil, err
	}

	userIDStrs := make([]string, 0, len(req.UserIDs))
	for _, id := range req.UserIDs {
		userIDStrs = append(userIDStrs, strconv.FormatUint(id, 10))
	}

	if err = casbin.ReplaceRoleUsers(role.RoleCode, userIDStrs); err != nil {
		return nil, fmt.Errorf("保存角色用户失败: %w", err)
	}

	return &admindto.RoleUserSaveResp{RoleID: req.RoleID}, nil
}

func (s *Service) RolePermissionTree(ctx context.Context, roleID uint64) (res *admindto.RolePermissionTreeResp, err error) {
	role, err := s.rm.GetByID(ctx, roleID)
	if err != nil {
		return nil, err
	}
	if role == nil {
		return nil, adminenums.Err(adminenums.ErrRoleNotFound)
	}

	all, err := s.mm.ListAll(ctx)
	if err != nil {
		return nil, err
	}

	permissions, err := casbin.GetRolePermissions(role.RoleCode)
	if err != nil {
		return nil, err
	}
	codes := make([]string, 0, len(permissions))
	for _, p := range permissions {
		codes = append(codes, p[2])
	}

	ids, err := s.GetIDsByPermissionCodes(ctx, codes)
	if err != nil {
		return nil, err
	}
	checked := withAncestorMenuIDs(all, ids)

	return &admindto.RolePermissionTreeResp{
		RoleID:   role.ID,
		RoleCode: role.RoleCode,
		RoleName: role.RoleName,
		MenuIDs:  checked,
		Tree:     buildPermissionTree(all, checked),
	}, nil
}

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

	list := make([]admindto.PermItem, 0, len(items))
	for _, item := range items {
		list = append(list, item)
	}

	return &admindto.PermListResp{Total: total, List: list}, nil
}

func (s *Service) PermDetail(ctx context.Context, req *admindto.PermDetailReq) (res *admindto.PermDetailResp, err error) {
	res = &admindto.PermDetailResp{}
	err = s.pm.DB(ctx).
		Select("id", "permission_code", "permission_name", "module", "api_path", "api_method",
			"status", "remark", "create_time", "update_time").
		Where("id = ?", req.ID).
		Scan(res).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, adminenums.Err(adminenums.ErrPermissionNotFound)
		}
		return nil, err
	}
	if res.ID == 0 {
		return nil, adminenums.Err(adminenums.ErrPermissionNotFound)
	}
	return res, nil
}

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

func (s *Service) PermCreate(ctx context.Context, req *admindto.PermCreateReq) (res *admindto.PermCreateResp, err error) {

	existing, err := s.pm.GetByCode(ctx, req.PermissionCode)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, adminenums.Err(adminenums.ErrCodeExists)
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

func (s *Service) PermUpdate(ctx context.Context, req *admindto.PermUpdateReq) (res *admindto.PermUpdateResp, err error) {
	reloadPolicy := false
	err = s.pm.Transaction(ctx, func(tx *gorm.DB) error {
		entity, err := s.pm.LockByIDTx(ctx, tx, req.ID)
		if err != nil {
			return err
		}
		if entity == nil {
			return adminenums.Err(adminenums.ErrPermissionNotFound)
		}
		if req.PermissionCode != entity.PermissionCode {
			return adminenums.Err(adminenums.ErrCodeImmutable)
		}

		assigned, err := casbin.HasPermissionPolicies(entity.PermissionCode)
		if err != nil {
			return err
		}
		if req.Status == adminmodel.PermissionStatusDisabled && assigned {
			return adminenums.Err(adminenums.ErrPermissionAssigned)
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

func (s *Service) PermDelete(ctx context.Context, req *admindto.PermDeleteReq) (res *admindto.PermDeleteResp, err error) {
	entities, err := s.pm.ListByIDs(ctx, req.IDs)
	if err != nil {
		return nil, err
	}
	if len(entities) != len(req.IDs) {
		return nil, adminenums.Err(adminenums.ErrPermissionNotFound)
	}
	codes := make([]string, 0, len(entities))
	for _, entity := range entities {
		assigned, err := casbin.HasPermissionPolicies(entity.PermissionCode)
		if err != nil {
			return nil, err
		}
		if assigned {
			return nil, adminenums.Err(adminenums.ErrPermissionAssigned)
		}
		codes = append(codes, entity.PermissionCode)
	}

	references, err := s.CountByPermissionCodes(ctx, codes)
	if err != nil {
		return nil, err
	}
	if references > 0 {
		return nil, adminenums.Err(adminenums.ErrMenuReferenced)
	}

	deleted, err := s.pm.DeleteByIDs(ctx, req.IDs)
	if err != nil {
		return nil, err
	}
	return &admindto.PermDeleteResp{DeletedCount: deleted}, nil
}

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

func (s *Service) ExistsEnabledCode(ctx context.Context, code string) (bool, error) {
	return s.pm.ExistsEnabledCode(ctx, code)
}
