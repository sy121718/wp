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
	// SetLocaleRetirePort 注入语言下线端口（装配期调用）。
	//
	// 放在接口里而不是 concrete 方法：装配层拿到的是 ProjectService 接口，
	// 而端口必须由产物侧实现后注入 —— 与本项目其它消费者侧端口同一形状。
	SetLocaleRetirePort(port LocaleRetirePort)
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

	// ---- 结构模板候选（主题设置页的「选结构模板」下拉）----

	// StructureTemplateOptions 列出本工程可绑定的结构模板（页眉 / 页脚）。
	//
	// 端口未注入时返回空列表、不报错：下拉少几个候选是配置面变窄，
	// 不该让「保存主题设置」这一整件事失败（该能力缺失会在启动日志里留一行 Warn）。
	StructureTemplateOptions(ctx context.Context, projectID string) (opts []StructureTemplateOption, err error)
}

// StructureTemplateOption 结构模板候选下拉项（主题设置页的「选结构模板」）。
//
// 只带下拉需要的字段：id / 名字 / 类型（header / footer）/ 是否当前生效。
// 文档与版本不进这里 —— 下拉不预览排版，选了之后由构建期解析。
type StructureTemplateOption struct {
	ID   string
	Name string
	// EntityType header / footer（只要这两种会出现在下拉里）。
	EntityType string
	// IsDefault 是否是该类型的当前生效模板（页面在选项后标注「当前生效」）。
	IsDefault bool
}

// StructureTemplateOptionsPort 结构模板候选的只读端口（消费者侧最窄接口）。
//
// 为什么由 project 声明、由装配层实现：project 不认识 content_templates 表，
// 也不 import contenttemplate 的 service/model —— 端口留在消费者侧是本项目的既有形状
// （与 LocaleRetirePort 同一手法）。
type StructureTemplateOptionsPort interface {
	// ListStructureTemplateOptions 列出工程内全部结构模板（页眉 / 页脚）。
	ListStructureTemplateOptions(ctx context.Context, projectID string) (opts []StructureTemplateOption, err error)
}

// LocaleRetirePort 禁用语言后的路由下线端口（审计 I18N-017）。
//
// 为什么由 project 声明、由产物侧实现：语言清单在 project 手里，而「这个语言有哪些
// 已激活路径」只有 page / presentation 知道 —— 反向依赖（project → page）会成环
// （page 依赖 project）。端口留在消费者侧，是本项目的既有形状。
//
// 未注入时的行为见 SaveLocales：不禁用、只记日志，不制造孤立路由。
type LocaleRetirePort interface {
	// LocaleRetireImpact 返回该语言当前的已激活路径数（确认前给运营看代价）。
	LocaleRetireImpact(ctx context.Context, projectID, lang string) (affected int, err error)
	// RetireLocale 下线该语言的全部已激活路由并清理其发布记录，返回实际处理数。
	//
	// 没有「写 301 到默认语言」这个开关：那需要为目标路径生成一份重定向产物
	//（publication.Redirect 只接受 ArtifactID / PageID），是另一个量级的工作；
	// 审计里这条本就是可选项。留一个永远被忽略的参数比不留更坏 —— 调用方会以为它生效了。
	RetireLocale(ctx context.Context, projectID, lang string) (retired int, err error)
}
