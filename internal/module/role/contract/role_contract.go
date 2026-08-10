// Package rolecontract 角色模块对外暴露的接口契约。
package rolecontract

import (
	"context"

	roledto "go_wp/internal/module/role/dto"
)

// RoleService 定义角色模块对外暴露的业务能力。
type RoleService interface {
	// List 角色分页列表。
	List(ctx context.Context, req *roledto.ListReq) (*roledto.ListResp, error)
	// Detail 角色详情。
	Detail(ctx context.Context, req *roledto.DetailReq) (*roledto.DetailResp, error)
	// Create 新建角色。
	Create(ctx context.Context, req *roledto.CreateReq) error
	// Update 更新角色元信息。
	Update(ctx context.Context, req *roledto.UpdateReq) error
	// Delete 删除角色，内置角色不可删。
	Delete(ctx context.Context, req *roledto.DeleteReq) error

	// MenuList 查询角色拥有的菜单 ID 列表。
	MenuList(ctx context.Context, req *roledto.MenuListReq) (*roledto.MenuListResp, error)
	// MenuSave 全量替换角色菜单授权。
	MenuSave(ctx context.Context, req *roledto.MenuSaveReq) (*roledto.MenuSaveResp, error)
	// UserList 查询角色下的用户列表。
	UserList(ctx context.Context, req *roledto.UserListReq) (*roledto.UserListResp, error)
	// UserSave 全量替换角色用户绑定。
	UserSave(ctx context.Context, req *roledto.UserSaveReq) (*roledto.UserSaveResp, error)

	// --- 对外契约 ---

	// GetRoleCodesByUserID 查用户的角色编码列表。
	GetRoleCodesByUserID(ctx context.Context, userID uint64) ([]string, error)
	// GetEnabledRoleIDsByCodes 将角色编码解析为已启用角色 ID。
	GetEnabledRoleIDsByCodes(ctx context.Context, codes []string) ([]uint64, error)
}
