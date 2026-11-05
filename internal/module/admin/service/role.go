package adminservice

import (
	"context"
	"fmt"
	"regexp"
	"strconv"

	"gorm.io/gorm"

	"go_wp/internal/module/admin/dto"
	"go_wp/internal/module/admin/enums"
	"go_wp/internal/module/admin/model"
	"go_wp/pkg/casbin"
	"go_wp/pkg/utils"
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

// RoleDelete 删除角色，单条与批量走同一条全成全败路径。
func (s *Service) RoleDelete(ctx context.Context, req *admindto.RoleDeleteReq) error {
	return s.RoleBulkDelete(ctx, req)
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
	// 授予与事务内的写同事务：先锁角色行与这次要授予的菜单/权限点行，再落 p 策略。
	//
	// 锁序与删除端一致（都按主键 id 升序）：删除端锁同一批行，两个写端才在同一处排队，
	// 「读到没人引用 → 授予提交 → 删掉」这条 READ COMMITTED 反例才挡得住。
	var reloadTaskID int64

	err = s.rm.Transaction(ctx, func(tx *gorm.DB) error {
		role, err := s.rm.LockByIDTx(ctx, tx, req.RoleID)
		if err != nil {
			return err
		}
		if role == nil {
			return adminenums.Err(adminenums.ErrRoleNotFound)
		}

		// 祖先菜单要一并授予，算它需要看全量菜单；锁的是本事务里真正会受影响的那批。
		all, err := s.mm.ListAllTx(ctx, tx)
		if err != nil {
			return err
		}
		menuIDs := withAncestorMenuIDs(all, req.MenuIDs)

		if len(menuIDs) > 0 {
			lockedMenus, err := s.mm.LockByIDsTx(ctx, tx, menuIDs)
			if err != nil {
				return err
			}
			if len(lockedMenus) != len(menuIDs) {
				return adminenums.Err(adminenums.ErrMenuNotFound)
			}
		}

		codes, err := s.menuPermissionCodesTx(ctx, tx, menuIDs)
		if err != nil {
			return err
		}

		willBeSuper, err := s.codesCoverAllEnabled(ctx, codes)
		if err != nil {
			return err
		}
		roleSuper, err := s.roleHasSuperAdminPermission(ctx, []string{role.RoleCode})
		if err != nil {
			return err
		}
		if err = s.requireSuperAdminForSensitiveTarget(ctx, req.OperatorID, willBeSuper || roleSuper); err != nil {
			return err
		}

		var policies [][3]string
		if len(codes) > 0 {
			lockedPerms, err := s.pm.LockByCodesTx(ctx, tx, codes)
			if err != nil {
				return err
			}
			if len(lockedPerms) != len(codes) {
				return adminenums.Err(adminenums.ErrPermissionNotFound)
			}
			for _, b := range lockedPerms {
				policies = append(policies, [3]string{b.APIPath, b.APIMethod, b.PermissionCode})
			}
		}

		changed, err := casbin.ReplaceRolePermissionsTx(ctx, tx, role.RoleCode, policies)
		if err != nil {
			return err
		}
		if changed {
			reloadTaskID, err = s.appendPolicyReloadTx(ctx, tx)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	s.FlushSideEffect(ctx, reloadTaskID)
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

// RoleUserSave 保存角色成员。
//
// 事务内先锁角色行、再按 id 升序锁成员行：成员分配与成员删除/改归属放在同一批行上排队，
// 否则「分配时读到成员还在 → 另一边把人删掉 → 分配把悬挂绑定写进策略」挡不住。
func (s *Service) RoleUserSave(ctx context.Context, req *admindto.RoleUserSaveReq) (res *admindto.RoleUserSaveResp, err error) {
	ids := uniqueIDs(req.UserIDs)
	var reloadTaskID int64

	err = s.rm.Transaction(ctx, func(tx *gorm.DB) error {
		role, err := s.rm.LockByIDTx(ctx, tx, req.RoleID)
		if err != nil {
			return err
		}
		if role == nil {
			return adminenums.Err(adminenums.ErrRoleNotFound)
		}

		var locked []adminmodel.AdminEntity
		if len(ids) > 0 {
			locked, err = s.am.LockByIDsTx(ctx, tx, ids)
			if err != nil {
				return err
			}
			if len(locked) != len(ids) {
				return adminenums.Err(adminenums.ErrAdminNotFound)
			}
		}

		roleSuper, err := s.roleHasSuperAdminPermission(ctx, []string{role.RoleCode})
		if err != nil {
			return err
		}
		targetSuper := false
		for _, entity := range locked {
			if entity.IsSuperAdmin() {
				targetSuper = true
				break
			}
		}
		if err = s.requireSuperAdminForSensitiveTarget(ctx, req.OperatorID, roleSuper || targetSuper); err != nil {
			return err
		}

		userIDStrs := make([]string, 0, len(ids))
		for _, id := range ids {
			userIDStrs = append(userIDStrs, strconv.FormatUint(id, 10))
		}

		// 成员绑定写本事务：非 Tx 版本走 enforcer 自己的连接，
		// 那是另一条连接上的脏写 —— 本事务回滚时它不会跟着回滚。
		changed, err := casbin.ReplaceRoleUsersTx(ctx, tx, role.RoleCode, userIDStrs)
		if err != nil {
			return fmt.Errorf("保存角色用户失败: %w", err)
		}
		if changed {
			// 内存 enforcer 提交后才会与实际一致：落成任务，提交后立刻试一次。
			reloadTaskID, err = s.appendPolicyReloadTx(ctx, tx)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	s.FlushSideEffect(ctx, reloadTaskID)
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
	total, entities, err := s.pm.ListPage(ctx, adminmodel.PermListFilter{
		Module:  req.Module,
		Code:    req.Code,
		APIPath: req.APIPath,
		Status:  req.Status,
		Page:    req.GetPage(),
		Limit:   req.GetLimit(),
	})
	if err != nil {
		return nil, err
	}

	list := make([]admindto.PermItem, 0, len(entities))
	for _, e := range entities {
		list = append(list, admindto.PermItem{
			ID:             e.ID,
			PermissionCode: e.PermissionCode,
			PermissionName: e.PermissionName,
			Module:         e.Module,
			APIPath:        e.APIPath,
			APIMethod:      e.APIMethod,
			Status:         e.Status,
			Remark:         stringOrEmpty(e.Remark),
			CreateTime:     utils.NewJSONTimePtr(e.CreateTime),
		})
	}

	return &admindto.PermListResp{Total: total, List: list}, nil
}

func (s *Service) PermDetail(ctx context.Context, req *admindto.PermDetailReq) (res *admindto.PermDetailResp, err error) {
	entity, err := s.pm.DetailByID(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if entity == nil {
		return nil, adminenums.Err(adminenums.ErrPermissionNotFound)
	}
	return &admindto.PermDetailResp{
		ID:             entity.ID,
		PermissionCode: entity.PermissionCode,
		PermissionName: entity.PermissionName,
		Module:         entity.Module,
		APIPath:        entity.APIPath,
		APIMethod:      entity.APIMethod,
		Status:         entity.Status,
		Remark:         stringOrEmpty(entity.Remark),
		CreateTime:     utils.NewJSONTimePtr(entity.CreateTime),
		UpdateTime:     utils.NewJSONTimePtr(entity.UpdateTime),
	}, nil
}

func (s *Service) PermOptions(ctx context.Context, req *admindto.PermOptionsReq) (res *admindto.PermOptionsResp, err error) {
	entities, err := s.pm.ListEnabledForOptions(ctx, req.Module)
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

// PermDelete 删除权限点，走与批量相同的同事务校验 + 删除路径。
func (s *Service) PermDelete(ctx context.Context, req *admindto.PermDeleteReq) (*admindto.PermDeleteResp, error) {
	return s.PermBulkDelete(ctx, req)
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
			PermissionName: e.PermissionName,
			APIPath:        e.APIPath,
			APIMethod:      e.APIMethod,
			Status:         e.Status,
		})
	}
	return result, nil
}

func (s *Service) ExistsEnabledCode(ctx context.Context, code string) (bool, error) {
	return s.pm.ExistsEnabledCode(ctx, code)
}
