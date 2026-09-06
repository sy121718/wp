// Package navigationcontract 定义 navigation 模块对外契约（0-C）。
// navigation 表示公开站点导航，与后台权限菜单 menu 严格隔离（不可复用 sys_menus 表）。
package navigationcontract

import (
	"context"

	navigationdto "go_wp/internal/module/navigation/dto"
)

// NavigationService 公开站点导航管理契约。
type NavigationService interface {
	// Create 新建导航项。
	Create(ctx context.Context, req *navigationdto.CreateReq) (res *navigationdto.NavigationResp, err error)
	// Update 更新导航项（仅更新传入的非空字段）。
	Update(ctx context.Context, req *navigationdto.UpdateReq) (res *navigationdto.NavigationResp, err error)
	// Get 按 ID 查询导航项。
	Get(ctx context.Context, req *navigationdto.GetReq) (res *navigationdto.NavigationResp, err error)
	// List 按工程（可选 kind）列出导航项，sort_order 升序。
	List(ctx context.Context, req *navigationdto.ListReq) (list []*navigationdto.NavigationResp, err error)
	// Delete 删除导航项。
	Delete(ctx context.Context, req *navigationdto.DeleteReq) (err error)
	// Render 返回该工程该 kind 的导航 HTML 片段。
	// 这是未来构建期把导航编译进静态 Artifact 的接口（本轮仅 CRUD + 存储 + 隔离，不做编译集成）。
	Render(ctx context.Context, projectID, kind string) (html string, err error)
}
