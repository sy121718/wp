package adminservice

import (
	"context"
	"strconv"

	admindto "go_wp/internal/module/admin/dto"
	"go_wp/pkg/casbin"
)

// RoleList 查询用户绑定的角色编码列表。
func (s *Service) RoleList(ctx context.Context, req *admindto.RoleListReq) (res *admindto.RoleListResp, err error) {
	roleCodes, err := casbin.GetRoleCodesByUserID(strconv.FormatUint(req.UserID, 10))
	if err != nil {
		return nil, err
	}

	var list []admindto.RoleListItem
	for _, code := range roleCodes {
		list = append(list, admindto.RoleListItem{RoleCode: code})
	}

	return &admindto.RoleListResp{List: list}, nil
}

// RoleSave 全量替换用户绑定的角色。
// 前端直接传 role_codes，写入 Casbin g 策略。
func (s *Service) RoleSave(ctx context.Context, req *admindto.RoleSaveReq) (res *admindto.RoleSaveResp, err error) {
	userIDStr := strconv.FormatUint(req.UserID, 10)
	if err = casbin.ReplaceUserRoleBindings(userIDStr, req.RoleCodes); err != nil {
		return nil, err
	}
	return &admindto.RoleSaveResp{UserID: req.UserID}, nil
}