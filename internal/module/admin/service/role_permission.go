package adminservice

import (
	"context"
	"errors"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	"go_wp/pkg/casbin"
)

// RolePermissionTree 返回角色权限分配所需的完整授权树与该角色当前的勾选集合。
//
// 这是「角色分权」的唯一读入口，页面（/admin/roles/permissions）与 JSON 接口共用它。
// 权限来源只有一份（sys_casbin_rule 里 sub=role_code 的 p 策略）：
//
//	p 策略的 code 列 → 反查 sys_menus.permission_code → menu_id 集合 → 向上补齐祖先
//
// 目录（type=1）没有权限码，反查永远查不出它们；按钮（type=3）的父菜单也不会自动
// 出现在结果里 —— 两件事都由 withAncestorMenuIDs 补齐（那里有完整论证）。
//
// 树里包含**已被禁用的已勾选项**：角色先勾、菜单后禁用的情况下，如果树里没有它，
// 页面渲染不出这个勾选，管理员一保存就把授权静默删掉了（见 buildPermissionTree）。
func (s *Service) RolePermissionTree(ctx context.Context, roleID uint64) (res *admindto.RolePermissionTreeResp, err error) {
	role, err := s.rm.GetByID(ctx, roleID)
	if err != nil {
		return nil, err
	}
	if role == nil {
		return nil, errors.New(adminenums.ErrRoleNotFound)
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
