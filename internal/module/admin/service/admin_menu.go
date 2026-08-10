package adminservice

import (
	"context"
	"strconv"

	admindto "go_wp/internal/module/admin/dto"
	"go_wp/pkg/casbin"
)

// MenuList 查询用户的直接额外菜单和有效菜单（含角色继承）。
func (s *Service) MenuList(ctx context.Context, req *admindto.MenuListReq) (res *admindto.MenuListResp, err error) {
	userIDStr := strconv.FormatUint(req.UserID, 10)

	// 1. 获取用户直接额外权限（p, user_id, ...）
	directPerms, err := casbin.GetUserDirectPermissions(userIDStr)
	if err != nil {
		return nil, err
	}

	// 直接权限的 codes → menu_ids
	directCodes := make([]string, 0, len(directPerms))
	for _, p := range directPerms {
		directCodes = append(directCodes, p[2])
	}

	var directMenuIDs []uint64
	if len(directCodes) > 0 {
		directMenuIDs, err = s.menuSvc.GetIDsByPermissionCodes(ctx, directCodes)
		if err != nil {
			return nil, err
		}
	}

	// 2. 获取用户全部有效权限（直接 + 角色继承）
	// Casbin Enforce 是按请求检查的，不能批量获取。
	// 但角色权限可以从 p, role_code, ... 获取。
	roleCodes, err := s.roleSvc.GetRoleCodesByUserID(ctx, req.UserID)
	if err != nil {
		return nil, err
	}

	// 收集有效 codes：直接 + 所有角色的
	effectiveCodes := make(map[string]struct{})
	for _, c := range directCodes {
		effectiveCodes[c] = struct{}{}
	}
	for _, rc := range roleCodes {
		rolePerms, err := casbin.GetRolePermissions(rc)
		if err != nil {
			return nil, err
		}
		for _, p := range rolePerms {
			effectiveCodes[p[2]] = struct{}{}
		}
	}

	// codes → menu_ids
	var effectiveMenuIDs []uint64
	if len(effectiveCodes) > 0 {
		uniqueCodes := make([]string, 0, len(effectiveCodes))
		for c := range effectiveCodes {
			uniqueCodes = append(uniqueCodes, c)
		}
		effectiveMenuIDs, err = s.menuSvc.GetIDsByPermissionCodes(ctx, uniqueCodes)
		if err != nil {
			return nil, err
		}
	}

	return &admindto.MenuListResp{
		DirectMenuIDs:    directMenuIDs,
		EffectiveMenuIDs: effectiveMenuIDs,
	}, nil
}

// MenuSave 全量替换用户的直接额外权限。
// menu_ids → permission_codes → [path, method, code] → Casbin ReplaceUserPermissions。
func (s *Service) MenuSave(ctx context.Context, req *admindto.MenuSaveReq) (res *admindto.MenuSaveResp, err error) {
	// menu_ids → permission_codes
	codes, err := s.menuSvc.GetPermissionCodesByIDs(ctx, req.MenuIDs)
	if err != nil {
		return nil, err
	}

	// permission_codes → [path, method, code]
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

	// Casbin 全量替换用户直接权限
	userIDStr := strconv.FormatUint(req.UserID, 10)
	if err = casbin.ReplaceUserPermissions(userIDStr, policies); err != nil {
		return nil, err
	}

	return &admindto.MenuSaveResp{UserID: req.UserID}, nil
}
