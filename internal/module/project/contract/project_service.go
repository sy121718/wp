// Package projectcontract 定义 project 模块对外契约。
package projectcontract

import (
	"context"

	projectdto "go_wp/internal/module/project/dto"
)

// ProjectService 站点工程与 SiteSettings 业务能力。
type ProjectService interface {
	Create(ctx context.Context, req *projectdto.CreateReq) (res *projectdto.ProjectResp, err error)
	// List 列出全部站点工程。
	List(ctx context.Context) (res []projectdto.ProjectResp, err error)
	Detail(ctx context.Context, req *projectdto.DetailReq) (res *projectdto.ProjectResp, err error)
	Update(ctx context.Context, req *projectdto.UpdateReq) (res *projectdto.ProjectResp, err error)
	Exists(ctx context.Context, id string) (exists bool, err error)

	// ---- 站点主题（多套并存，单套激活；页面挂接主题）----

	// ListThemes 列出工程全部主题（激活在前）。
	ListThemes(ctx context.Context, projectID string) (res []projectdto.ThemeResp, err error)
	// ListThemesByBlockID 列出绑定了指定全局块（页眉/页脚槽位）的全部主题。
	ListThemesByBlockID(ctx context.Context, blockID string) (res []projectdto.ThemeResp, err error)
	// GetTheme 按 ID 取单个主题。
	GetTheme(ctx context.Context, id string) (res *projectdto.ThemeResp, err error)
	// CreateTheme 新建主题（同工程名称唯一；工程首个主题自动激活）。
	CreateTheme(ctx context.Context, req *projectdto.ThemeCreateReq) (res *projectdto.ThemeResp, err error)
	// UpdateTheme 更新主题名称与设置（颜色/字体/页眉页脚引用）。
	UpdateTheme(ctx context.Context, req *projectdto.ThemeUpdateReq) (res *projectdto.ThemeResp, err error)
	// ActivateTheme 激活主题（整站前端切换，页面内容不动）。
	ActivateTheme(ctx context.Context, req *projectdto.ThemeActivateReq) (err error)
	// DeleteTheme 删除主题（激活态拒绝）。
	DeleteTheme(ctx context.Context, id string) (err error)
	// GetActiveTheme 取工程当前激活主题。
	GetActiveTheme(ctx context.Context, projectID string) (res *projectdto.ThemeResp, err error)
}
