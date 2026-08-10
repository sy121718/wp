package adminservice

import (
	"context"
	"sort"
	"strconv"

	admindto "go_wp/internal/module/admin/dto"
	"go_wp/pkg/casbin"
)

// Routes 返回当前用户有效路由树、角色 codes 和有效 permission codes。
// 供前端动态路由初始化和按钮权限判断。
func (s *Service) Routes(ctx context.Context, userID uint64) (res *admindto.RoutesResp, err error) {
	userIDStr := strconv.FormatUint(userID, 10)

	// 1. 获取启用角色编码列表，保持与 Casbin g2 角色状态语义一致。
	roleCodes, err := s.roleSvc.GetRoleCodesByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	// 2. 收集全部有效 permission codes
	effectiveCodes := make(map[string]struct{})

	// 2a. 用户直接权限
	directPerms, err := casbin.GetUserDirectPermissions(userIDStr)
	if err != nil {
		return nil, err
	}
	for _, p := range directPerms {
		effectiveCodes[p[2]] = struct{}{}
	}

	// 2b. 角色继承权限
	for _, rc := range roleCodes {
		rolePerms, err := casbin.GetRolePermissions(rc)
		if err != nil {
			return nil, err
		}
		for _, p := range rolePerms {
			effectiveCodes[p[2]] = struct{}{}
		}
	}

	// 3. 转为稳定数组并构建当前用户可见路由树。
	uniqueCodes := make([]string, 0, len(effectiveCodes))
	for c := range effectiveCodes {
		uniqueCodes = append(uniqueCodes, c)
	}
	sort.Strings(uniqueCodes)
	if roleCodes == nil {
		roleCodes = []string{}
	}

	routes, err := s.menuSvc.BuildAuthorizedRoutes(ctx, uniqueCodes)
	if err != nil {
		return nil, err
	}

	return &admindto.RoutesResp{
		Routes:          routes,
		Roles:           roleCodes,
		PermissionCodes: uniqueCodes,
	}, nil
}
