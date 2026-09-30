package adminservice

import (
	"context"
	"sort"
	"strconv"

	admindto "go_wp/internal/module/admin/dto"
	"go_wp/pkg/casbin"
)

// AdminRoleList 查询用户绑定的角色编码列表。
func (s *Service) AdminRoleList(ctx context.Context, req *admindto.AdminRoleListReq) (res *admindto.AdminRoleListResp, err error) {
	roleCodes, err := casbin.GetRoleCodesByUserID(strconv.FormatUint(req.UserID, 10))
	if err != nil {
		return nil, err
	}

	var list []admindto.AdminRoleListItem
	for _, code := range roleCodes {
		list = append(list, admindto.AdminRoleListItem{RoleCode: code})
	}

	return &admindto.AdminRoleListResp{List: list}, nil
}

// AdminRoleSave 全量替换用户绑定的角色。
// 前端直接传 role_codes，写入 Casbin g 策略。
//
// 超管保护（审计项「RBAC 提权无超管保护」）：目标用户是超管账号（is_admin=1）
// 或目标角色含超管权限（权限集覆盖全部启用权限点）时，仅超管可操作——
// 防止普通管理员借「替换角色绑定」把超管账号降权，或给自己绑定超管角色提权。
func (s *Service) AdminRoleSave(ctx context.Context, req *admindto.AdminRoleSaveReq) (res *admindto.AdminRoleSaveResp, err error) {
	targetSuper, err := s.IsSuperAdmin(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	roleSuper, err := s.roleHasSuperAdminPermission(ctx, req.RoleCodes)
	if err != nil {
		return nil, err
	}
	if err = s.requireSuperAdminForSensitiveTarget(ctx, req.OperatorID, targetSuper || roleSuper); err != nil {
		return nil, err
	}

	userIDStr := strconv.FormatUint(req.UserID, 10)
	if err = casbin.ReplaceUserRoleBindings(userIDStr, req.RoleCodes); err != nil {
		return nil, err
	}
	return &admindto.AdminRoleSaveResp{UserID: req.UserID}, nil
}

// AdminMenuList 查询用户的直接额外菜单和有效菜单（含角色继承）。
func (s *Service) AdminMenuList(ctx context.Context, req *admindto.AdminMenuListReq) (res *admindto.AdminMenuListResp, err error) {
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
		directMenuIDs, err = s.GetIDsByPermissionCodes(ctx, directCodes)
		if err != nil {
			return nil, err
		}
	}

	// 2. 获取用户全部有效权限（直接 + 角色继承）
	// Casbin Enforce 是按请求检查的，不能批量获取。
	// 但角色权限可以从 p, role_code, ... 获取。
	roleCodes, err := s.GetRoleCodesByUserID(ctx, req.UserID)
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
		effectiveMenuIDs, err = s.GetIDsByPermissionCodes(ctx, uniqueCodes)
		if err != nil {
			return nil, err
		}
	}

	return &admindto.AdminMenuListResp{
		DirectMenuIDs:    directMenuIDs,
		EffectiveMenuIDs: effectiveMenuIDs,
	}, nil
}

// AdminMenuSave 全量替换用户的直接额外权限。
// menu_ids → permission_codes → [path, method, code] → Casbin ReplaceUserPermissions。
//
// 超管保护：**与 AdminRoleSave 同款，此前这条路径漏了**（它没有任何界面调用方，
// 所以缺口一直没被走到）。直接权限是 p 策略里 r.sub == p.sub 那一支，写入集合一旦覆盖
// 全部启用权限点，目标账号即刻等价超管 —— 而入口只需要 admin:menu_save 这一个权限点。
// 三个判据与 RoleMenuSave 一致：新集合是否超管等价、目标账号是否本来就是超管，
// 任一为真则仅超管可操作（requireSuperAdminForSensitiveTarget）。
func (s *Service) AdminMenuSave(ctx context.Context, req *admindto.AdminMenuSaveReq) (res *admindto.AdminMenuSaveResp, err error) {
	// menu_ids → permission_codes
	codes, err := s.GetPermissionCodesByIDs(ctx, req.MenuIDs)
	if err != nil {
		return nil, err
	}

	willBeSuper, err := s.codesCoverAllEnabled(ctx, codes)
	if err != nil {
		return nil, err
	}
	targetSuper, err := s.IsSuperAdmin(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	if err = s.requireSuperAdminForSensitiveTarget(ctx, req.OperatorID, willBeSuper || targetSuper); err != nil {
		return nil, err
	}

	// permission_codes → [path, method, code]
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

	// Casbin 全量替换用户直接权限
	userIDStr := strconv.FormatUint(req.UserID, 10)
	if err = casbin.ReplaceUserPermissions(userIDStr, policies); err != nil {
		return nil, err
	}

	return &admindto.AdminMenuSaveResp{UserID: req.UserID}, nil
}

// AdminRoutes 返回当前用户有效路由树、角色 codes 和有效 permission codes。
// 供前端动态路由初始化和按钮权限判断。lang 为请求语言，用于菜单标题翻译。
func (s *Service) AdminRoutes(ctx context.Context, userID uint64, lang string) (res *admindto.AdminRoutesResp, err error) {
	roleCodes, uniqueCodes, err := s.collectEffectiveCodes(ctx, userID)
	if err != nil {
		return nil, err
	}

	routes, err := s.BuildAuthorizedRoutes(ctx, uniqueCodes, lang)
	if err != nil {
		return nil, err
	}

	return &admindto.AdminRoutesResp{
		Routes:          routes,
		Roles:           roleCodes,
		PermissionCodes: uniqueCodes,
	}, nil
}

// collectEffectiveCodes 收集用户全部有效权限码（用户直接权限 + 角色继承权限），
// 返回（启用角色码列表, 排序后的有效权限码列表）。
//
// 供 AdminRoutes（动态路由）与 EffectivePermissionCodes（渲染层过滤）复用，
// 保证「API 鉴权放行的权限」与「页面/按钮显示的权限」永远同源。
func (s *Service) collectEffectiveCodes(ctx context.Context, userID uint64) (roleCodes, codes []string, err error) {
	userIDStr := strconv.FormatUint(userID, 10)

	// 1. 启用角色编码列表，保持与 Casbin g2 角色状态语义一致。
	roleCodes, err = s.GetRoleCodesByUserID(ctx, userID)
	if err != nil {
		return nil, nil, err
	}

	// 2. 收集全部有效 permission codes
	effectiveCodes := make(map[string]struct{})

	// 2a. 用户直接权限
	directPerms, err := casbin.GetUserDirectPermissions(userIDStr)
	if err != nil {
		return nil, nil, err
	}
	for _, p := range directPerms {
		effectiveCodes[p[2]] = struct{}{}
	}

	// 2b. 角色继承权限
	for _, rc := range roleCodes {
		rolePerms, err := casbin.GetRolePermissions(rc)
		if err != nil {
			return nil, nil, err
		}
		for _, p := range rolePerms {
			effectiveCodes[p[2]] = struct{}{}
		}
	}

	// 3. 转为稳定数组（确定性渲染顺序）。
	codes = make([]string, 0, len(effectiveCodes))
	for c := range effectiveCodes {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	if roleCodes == nil {
		roleCodes = []string{}
	}
	return roleCodes, codes, nil
}

// EffectivePermissionCodes 返回用户全部有效权限码（直接 + 角色继承）。
//
// 渲染层入口：dashboard 页面据此注入 HasPerm，做菜单 / 按钮 / 字段的可见性过滤，
// 与 Casbin API 鉴权共用同一份权限来源，避免「看得到但点不了」或反向不一致。
func (s *Service) EffectivePermissionCodes(ctx context.Context, userID uint64) ([]string, error) {
	_, codes, err := s.collectEffectiveCodes(ctx, userID)
	if err != nil {
		return nil, err
	}
	return codes, nil
}
