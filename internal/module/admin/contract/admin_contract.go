package admincontract

import (
	"context"

	admindto "go_wp/internal/module/admin/dto"
)

// AdminService 定义 handle 层需要的 admin 业务能力（对外暴露契约）。
type AdminService interface {
	// List 管理员列表（分页 + 筛选）。
	List(ctx context.Context, req *admindto.ListReq) (*admindto.ListResp, error)
	// Login 管理员登录。
	Login(ctx context.Context, req *admindto.LoginReq, clientIP string) (*admindto.LoginResp, error)
	// Logout 注销当前管理员会话。
	Logout(ctx context.Context, userID uint64) error
	// Profile 获取当前登录管理员的个人信息。
	Profile(ctx context.Context, userID uint64) (*admindto.ProfileResp, error)
	// Create 新增管理员。
	Create(ctx context.Context, req *admindto.CreateReq) (*admindto.CreateResp, error)
	// Edit 修改管理员信息。
	Edit(ctx context.Context, req *admindto.EditReq) (*admindto.EditResp, error)
	// Detail 获取单个管理员详情。
	Detail(ctx context.Context, req *admindto.DetailReq) (*admindto.DetailResp, error)
	// Delete 删除管理员。
	Delete(ctx context.Context, req *admindto.DeleteReq) (*admindto.DeleteResp, error)

	// --- 角色绑定 ---

	// RoleList 查询用户绑定的角色编码列表。
	RoleList(ctx context.Context, req *admindto.RoleListReq) (*admindto.RoleListResp, error)
	// RoleSave 全量替换用户绑定的角色。
	RoleSave(ctx context.Context, req *admindto.RoleSaveReq) (*admindto.RoleSaveResp, error)

	// --- 直接额外权限 ---

	// MenuList 查询用户的直接额外菜单和有效菜单（含角色继承）。
	MenuList(ctx context.Context, req *admindto.MenuListReq) (*admindto.MenuListResp, error)
	// MenuSave 全量替换用户的直接额外权限。
	MenuSave(ctx context.Context, req *admindto.MenuSaveReq) (*admindto.MenuSaveResp, error)

	// --- 动态路由权限投影 ---

	// Routes 返回当前用户有效路由树、角色 codes 和有效 permission codes。
	Routes(ctx context.Context, userID uint64) (*admindto.RoutesResp, error)

	// --- 部门相关 ---

	// ListByDeptID 按部门 ID 分页查管理员列表。
	ListByDeptID(ctx context.Context, req *admindto.ListByDeptIDReq) (*admindto.ListByDeptIDResp, error)
	// BatchSetDeptID 批量设置用户的部门 ID。
	BatchSetDeptID(ctx context.Context, req *admindto.BatchSetDeptIDReq) error
	// CountByDeptID 统计某部门下的管理员数量。
	CountByDeptID(ctx context.Context, deptID uint64) (int64, error)
}
