package permissioncontract

import (
	"context"

	permissiondto "go_wp/internal/module/permission/dto"
)

// PermissionService 定义权限点模块对外暴露的业务能力。
type PermissionService interface {
	// List 权限点列表（分页 + 筛选）。
	List(ctx context.Context, req *permissiondto.ListReq) (*permissiondto.ListResp, error)
	// Detail 权限点详情。
	Detail(ctx context.Context, req *permissiondto.DetailReq) (*permissiondto.DetailResp, error)
	// Options 启用权限选项（菜单表单使用，不分页）。
	Options(ctx context.Context, req *permissiondto.OptionsReq) (*permissiondto.OptionsResp, error)
	// Create 新建权限点。
	Create(ctx context.Context, req *permissiondto.CreateReq) (*permissiondto.CreateResp, error)
	// Update 更新权限点。
	Update(ctx context.Context, req *permissiondto.UpdateReq) (*permissiondto.UpdateResp, error)
	// Delete 批量删除权限点。
	Delete(ctx context.Context, req *permissiondto.DeleteReq) (*permissiondto.DeleteResp, error)

	// --- 对外契约（供 menu/role/admin 模块调用） ---

	// ListByCodes 按 permission_code 列表批量查权限摘要。
	ListByCodes(ctx context.Context, codes []string) ([]permissiondto.PermissionBrief, error)
	// ExistsEnabledCode 检查 code 是否存在且启用。
	ExistsEnabledCode(ctx context.Context, code string) (bool, error)
}