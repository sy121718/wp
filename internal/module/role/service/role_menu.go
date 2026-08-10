// Package roleservice 角色模块业务逻辑实现。
package roleservice

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	admindto "go_wp/internal/module/admin/dto"
	roledto "go_wp/internal/module/role/dto"
	roleenums "go_wp/internal/module/role/enums"
	"go_wp/pkg/casbin"
)

// MenuList 查询角色拥有的菜单 ID 列表。
// 通过 Casbin p 策略反查角色拥有的 permission_codes，
// 再通过 menu contract 反查对应的 menu_ids。
func (s *Service) MenuList(ctx context.Context, req *roledto.MenuListReq) (res *roledto.MenuListResp, err error) {
	role, err := s.rm.GetByID(ctx, req.RoleID)
	if err != nil {
		return nil, err
	}
	if role == nil {
		return nil, errors.New(roleenums.ErrRoleNotFound)
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

	// 通过 menu contract 反查 menu_ids
	menuIDs, err := s.menuSvc.GetIDsByPermissionCodes(ctx, codes)
	if err != nil {
		return nil, err
	}

	return &roledto.MenuListResp{MenuIDs: menuIDs}, nil
}

// MenuSave 全量替换角色菜单授权。
// 流程：menu_ids → permission_codes → [path, method, code] → Casbin ReplaceRolePermissions。
func (s *Service) MenuSave(ctx context.Context, req *roledto.MenuSaveReq) (res *roledto.MenuSaveResp, err error) {
	role, err := s.rm.GetByID(ctx, req.RoleID)
	if err != nil {
		return nil, err
	}
	if role == nil {
		return nil, errors.New(roleenums.ErrRoleNotFound)
	}

	// menu_ids → permission_codes
	codes, err := s.menuSvc.GetPermissionCodesByIDs(ctx, req.MenuIDs)
	if err != nil {
		return nil, err
	}

	// permission_codes → [path, method, code] 三元组
	var policies [][3]string
	if len(codes) > 0 {
		briefs, err := s.permSvc.ListByCodes(ctx, codes)
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

	return &roledto.MenuSaveResp{RoleID: req.RoleID}, nil
}

// UserList 查询角色下的用户列表。
// 通过 Casbin g 策略反查 user_ids。
func (s *Service) UserList(ctx context.Context, req *roledto.UserListReq) (res *roledto.UserListResp, err error) {
	role, err := s.rm.GetByID(ctx, req.RoleID)
	if err != nil {
		return nil, err
	}
	if role == nil {
		return nil, errors.New(roleenums.ErrRoleNotFound)
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

	// 通过 admin contract 查询用户详情
	list := make([]roledto.UserItem, 0, len(pageIDs))
	for _, id := range pageIDs {
		if s.adminSvc == nil {
			list = append(list, roledto.UserItem{ID: id})
			continue
		}
		detail, err := s.adminSvc.Detail(ctx, &admindto.DetailReq{Id: id})
		if err != nil || detail == nil {
			// 查不到时只返回 ID
			list = append(list, roledto.UserItem{ID: id})
			continue
		}
		list = append(list, roledto.UserItem{
			ID:       id,
			Username: detail.Username,
			Name:     detail.Name,
			Email:    detail.Email,
			Status:   detail.Status,
		})
	}

	return &roledto.UserListResp{Total: total, List: list}, nil
}

// UserSave 全量替换角色用户绑定。
func (s *Service) UserSave(ctx context.Context, req *roledto.UserSaveReq) (res *roledto.UserSaveResp, err error) {
	role, err := s.rm.GetByID(ctx, req.RoleID)
	if err != nil {
		return nil, err
	}
	if role == nil {
		return nil, errors.New(roleenums.ErrRoleNotFound)
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

	return &roledto.UserSaveResp{RoleID: req.RoleID}, nil
}