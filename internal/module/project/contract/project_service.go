// Package projectcontract 定义 project 模块对外契约。
package projectcontract

import (
	"context"

	projectdto "go_wp/internal/module/project/dto"
)

// 请求/响应 DTO 重导出：跨模块调用方只依赖 contract，不直接 import project/dto。
type (
	CreateReq        = projectdto.CreateReq
	UpdateReq        = projectdto.UpdateReq
	DetailReq        = projectdto.DetailReq
	ProjectResp      = projectdto.ProjectResp
	ThemeCreateReq   = projectdto.ThemeCreateReq
	ThemeUpdateReq   = projectdto.ThemeUpdateReq
	ThemeActivateReq = projectdto.ThemeActivateReq
	ThemeResp        = projectdto.ThemeResp
	LocaleItem       = projectdto.LocaleItem
	LocalesSaveReq   = projectdto.LocalesSaveReq
	LocaleResp       = projectdto.LocaleResp
	// SiteSettings 站点级设置的结构化视图（构建期读取 GA4 测量 ID 等站点级字段）。
	SiteSettings = projectdto.SiteSettings
)

// ParseSiteSettings 解析 projects.settings JSON（缺失 / 非对象按零值处理）。
var ParseSiteSettings = projectdto.ParseSiteSettings

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
	// EnsureDefaultThemes 给尚无任何主题的工程补默认主题（后台风格色值，幂等）。
	// 供启动时跑一次，覆盖本能力上线前建的存量工程。
	EnsureDefaultThemes(ctx context.Context) (fixed int, err error)

	// ---- 站点语言清单（多语言 P3，docs/06-D §14 D10）----

	// ListLocales 列出站点语言清单（默认语言在前）。
	ListLocales(ctx context.Context, projectID string) (res []projectdto.LocaleResp, err error)
	// EnabledLangs 返回站点启用语言（默认语言在前；无清单时回退站点默认语言一种）。
	EnabledLangs(ctx context.Context, projectID string) (langs []string, err error)
	// DefaultLocale 返回站点默认语言（清单 is_default，缺失回退 i18n.default_lang）。
	DefaultLocale(ctx context.Context, projectID string) (lang string, err error)
	// SaveLocales 全量保存站点语言清单（至少一种语言、至多一个默认且默认必须启用）。
	SaveLocales(ctx context.Context, req *projectdto.LocalesSaveReq) (res []projectdto.LocaleResp, err error)
}
