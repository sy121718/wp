package adminservice

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	"go_wp/pkg/casbin"
)

// RoleMenuList 查询角色拥有的菜单 ID 列表。
// 通过 Casbin p 策略反查角色拥有的 permission_codes，
// 再通过菜单反查对应的 menu_ids。
func (s *Service) RoleMenuList(ctx context.Context, req *admindto.RoleMenuListReq) (res *admindto.RoleMenuListResp, err error) {
	role, err := s.rm.GetByID(ctx, req.RoleID)
	if err != nil {
		return nil, err
	}
	if role == nil {
		return nil, errors.New(adminenums.ErrRoleNotFound)
	}

	// 获取角色在 Casbin 中的全部 p 策略
	permissions, err := casbin.GetRolePermissions(role.RoleCode)
	if err != nil {
		return nil, err
	}

	// 提取 permission_code（p 策略的第 3 个元素）
	codes := make([]string, 0, len(permissions))
	for _, p := range permissions {
		codes = append(codes, p[2])
	}

	// 反查 menu_ids
	menuIDs, err := s.GetIDsByPermissionCodes(ctx, codes)
	if err != nil {
		return nil, err
	}

	return &admindto.RoleMenuListResp{MenuIDs: menuIDs}, nil
}

// RoleMenuSave 全量替换角色菜单授权。
// 流程：menu_ids → permission_codes → [path, method, code] → Casbin ReplaceRolePermissions。
func (s *Service) RoleMenuSave(ctx context.Context, req *admindto.RoleMenuSaveReq) (res *admindto.RoleMenuSaveResp, err error) {
	role, err := s.rm.GetByID(ctx, req.RoleID)
	if err != nil {
		return nil, err
	}
	if role == nil {
		return nil, errors.New(adminenums.ErrRoleNotFound)
	}

	// menu_ids → permission_codes
	codes, err := s.GetPermissionCodesByIDs(ctx, req.MenuIDs)
	if err != nil {
		return nil, err
	}

	// permission_codes → [path, method, code] 三元组
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

	// Casbin 全量替换
	if err = casbin.ReplaceRolePermissions(role.RoleCode, policies); err != nil {
		return nil, fmt.Errorf("保存角色权限失败: %w", err)
	}

	return &admindto.RoleMenuSaveResp{RoleID: req.RoleID}, nil
}

// RoleUserList 查询角色下的用户列表。
// 通过 Casbin g 策略反查 user_ids。
func (s *Service) RoleUserList(ctx context.Context, req *admindto.RoleUserListReq) (res *admindto.RoleUserListResp, err error) {
	role, err := s.rm.GetByID(ctx, req.RoleID)
	if err != nil {
		return nil, err
	}
	if role == nil {
		return nil, errors.New(adminenums.ErrRoleNotFound)
	}

	// Casbin 反查用户 ID
	userIDStrs, err := casbin.GetUserIDsByRoleCode(role.RoleCode)
	if err != nil {
		return nil, err
	}

	// 转换为 uint64
	userIDs := make([]uint64, 0, len(userIDStrs))
	for _, s := range userIDStrs {
		id, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			continue
		}
		userIDs = append(userIDs, id)
	}

	// 内存分页
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

	// 同包直调 admin 详情查询用户信息
	list := make([]admindto.RoleUserItem, 0, len(pageIDs))
	for _, id := range pageIDs {
		detail, err := s.AdminDetail(ctx, &admindto.AdminDetailReq{Id: id})
		if err != nil || detail == nil {
			// 查不到时只返回 ID
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

// RoleUserSave 全量替换角色用户绑定。
//
// 超管保护（审计项「RBAC 提权无超管保护」）：目标角色含超管权限
// （权限集覆盖全部启用权限点），或目标用户列表含超管账号时，仅超管可操作——
// 防止普通管理员把任意账号加进超管角色提权，或改绑超管账号。
func (s *Service) RoleUserSave(ctx context.Context, req *admindto.RoleUserSaveReq) (res *admindto.RoleUserSaveResp, err error) {
	role, err := s.rm.GetByID(ctx, req.RoleID)
	if err != nil {
		return nil, err
	}
	if role == nil {
		return nil, errors.New(adminenums.ErrRoleNotFound)
	}

	roleSuper, err := s.roleHasSuperAdminPermission(ctx, []string{role.RoleCode})
	if err != nil {
		return nil, err
	}
	targetSuper := false
	for _, id := range req.UserIDs {
		super, err := s.isSuperAdmin(ctx, id)
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

	// 转换 userIDs → string
	userIDStrs := make([]string, 0, len(req.UserIDs))
	for _, id := range req.UserIDs {
		userIDStrs = append(userIDStrs, strconv.FormatUint(id, 10))
	}

	// Casbin 全量替换 g 策略
	if err = casbin.ReplaceRoleUsers(role.RoleCode, userIDStrs); err != nil {
		return nil, fmt.Errorf("保存角色用户失败: %w", err)
	}

	return &admindto.RoleUserSaveResp{RoleID: req.RoleID}, nil
}
