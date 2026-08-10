// Package menucontract 菜单模块对外暴露的接口契约。
package menucontract

import (
	"context"

	menudto "go_wp/internal/module/menu/dto"
)

// RouteMeta 前端动态路由元信息。
type RouteMeta struct {
	Title    string   `json:"title"`
	Icon     string   `json:"icon,omitempty"`
	ShowLink bool     `json:"showLink"`
	Rank     int      `json:"rank,omitempty"`
	Auths    []string `json:"auths,omitempty"`
}

// RouteNode 前端动态路由节点。
type RouteNode struct {
	Path      string      `json:"path"`
	Name      string      `json:"name"`
	Component string      `json:"component,omitempty"`
	Redirect  string      `json:"redirect,omitempty"`
	Meta      RouteMeta   `json:"meta"`
	Children  []RouteNode `json:"children,omitempty"`
}

// MenuService 定义菜单模块对外暴露的业务能力。
type MenuService interface {
	// Tree 菜单树（含筛选）。
	Tree(ctx context.Context, req *menudto.TreeReq) ([]menudto.TreeNode, error)
	// Detail 菜单详情。
	Detail(ctx context.Context, req *menudto.DetailReq) (*menudto.DetailResp, error)
	// Create 新建菜单。
	Create(ctx context.Context, req *menudto.CreateReq) error
	// Update 更新菜单。
	Update(ctx context.Context, req *menudto.UpdateReq) error
	// Delete 批量删除菜单。
	Delete(ctx context.Context, req *menudto.DeleteReq) error

	// --- 对外契约（供 role/admin 模块调用） ---

	// GetPermissionCodesByIDs 根据 menu_id 列表收集 type=2/3 的 permission_code 并去重。
	GetPermissionCodesByIDs(ctx context.Context, menuIDs []uint64) ([]string, error)
	// GetIDsByPermissionCodes 根据 permission_code 列表反查 menu_id。
	GetIDsByPermissionCodes(ctx context.Context, codes []string) ([]uint64, error)
	// CountByPermissionCodes 统计引用指定权限编码的未删除菜单数。
	CountByPermissionCodes(ctx context.Context, codes []string) (int64, error)
	// BuildAuthorizedTree 根据有效 permission_code 列表构建用户可见菜单树。
	BuildAuthorizedTree(ctx context.Context, codes []string) ([]menudto.TreeNode, error)
	// BuildAuthorizedRoutes 根据有效 permission_code 列表构建前端动态路由树。
	BuildAuthorizedRoutes(ctx context.Context, codes []string) ([]RouteNode, error)
}
