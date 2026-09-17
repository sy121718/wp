// Package workbenchenums 承载可视化工作台（编辑器本体）的后台页面入口：
// 编辑器外壳 / 预览编译直出 / 结构树 / 检查器 / SEO 评分 / 编辑器桥接。
package workbenchhttp

import (
	"context"
	"errors"
	"net/http"

	"go_wp/internal/builder/core"

	blockcontract "go_wp/internal/module/block/contract"
	blueprintcontract "go_wp/internal/module/blueprint/contract"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	pagecontract "go_wp/internal/module/page/contract"
	plugincontract "go_wp/internal/module/plugin/contract"
	presentationdto "go_wp/internal/module/presentation/dto"
	productcontract "go_wp/internal/module/product/contract"
	projectcontract "go_wp/internal/module/project/contract"
	workbenchenums "go_wp/internal/module/workbench/enums"

	"go_wp/internal/web/shell"
	"go_wp/pkg/i18n"

	"github.com/gin-gonic/gin"
)

// Handle 工作台编辑器页面处理器；依赖全部经 SetupWorkbenchRoutes 参数注入。
type Handle struct {
	// pages 草稿 / 预览 / 目标解析契约。
	pages pagecontract.PageService
	// projects 站点工程契约（页面挂接主题的 settings 读取）。
	projects projectcontract.ProjectService
	// blocks 全局块契约（块编辑模式 / 块预览 / 全局块分组）。
	blocks blockcontract.BlockService
	// plugins 插件契约（组件库摘要与区块预设，可空降级）。
	plugins plugincontract.PluginService
	// collection 集合内容解析器（装配位；预览编译下沉 page 模块后本包不直接消费，
	// 保留字段维持装配签名稳定）。
	collection core.CollectionResolver
	// contenttemplates 内容模板契约（EDT-001 模板编辑模式）。
	contenttemplates contenttemplatecontract.ContentTemplateService
	// templatePreview 模板预览实例端口（presentation 的最窄能力）。
	templatePreview TemplatePreviewPort
	// blueprints 蓝图候选端口（可空降级；当前编辑器本体未消费，装配位保留）。
	blueprints blueprintcontract.BlueprintService
	// products 商品构建期数据源（检查器 entityref 下拉，可空降级）。
	products productcontract.ProductDataSource
	// contentStore 内容译文读写端口（可空降级；当前编辑器本体未消费，装配位保留）。
	contentStore ContentTranslationPort
}

// TemplatePreviewPort 模板工作台预览所需的最窄 presentation 能力。
type TemplatePreviewPort interface {
	PreviewInstance(ctx context.Context, req *presentationdto.PreviewInstanceReq) (res *presentationdto.PreviewInstanceResp, err error)
}

// ContentTranslationPort 工作台使用的内容译文读写端口（与 dashboard 同形状；
// 为 nil 时按默认实现惰性构造，测试注入隔离 schema 的写入器）。
type ContentTranslationPort interface {
	LoadDetails(ctx context.Context, lang string, hashes []string) (map[string]i18n.ContentTargetInfo, error)
	LoadDetailsForProject(ctx context.Context, projectID, lang string, hashes []string) (map[string]i18n.ContentTargetInfo, error)
	LoadTargets(ctx context.Context, lang string, hashes []string) (map[string]string, error)
	LoadTargetsForProject(ctx context.Context, projectID, lang string, hashes []string) (map[string]string, error)
	Upsert(ctx context.Context, items []i18n.ContentWriteItem) (written int, err error)
}

// New 按契约直构 Handle（测试与外部装配用；常规装配走 SetupWorkbenchRoutes，
// 其余依赖装配位字段留零值即可，handler 侧自行降级）。
func New(pages pagecontract.PageService, projects projectcontract.ProjectService,
	blocks blockcontract.BlockService, plugins plugincontract.PluginService,
	collection core.CollectionResolver, contenttemplates contenttemplatecontract.ContentTemplateService,
	templatePreview TemplatePreviewPort) *Handle {
	return &Handle{pages: pages, projects: projects, blocks: blocks, plugins: plugins,
		collection: collection, contenttemplates: contenttemplates, templatePreview: templatePreview}
}

// pageOf 按 id 读取页面（画布 / 预览共用同一取数口径）。
//
// Detail 把 projectID 当**必填的越权防护 scope**（少它只会得到「参数缺失」，
// 看起来像「页面不存在」）。画布路由手上只有 pageId，所以先用只读的
// ProjectOfPage 问「这个页面属于谁」，再按 scope 取详情。
func (h *Handle) pageOf(c *gin.Context, pageID string) (*pagecontract.PageResp, error) {
	if h.pages == nil {
		return nil, errors.New("页面服务未装配")
	}
	ctx := c.Request.Context()
	projectID, err := h.pages.ProjectOfPage(ctx, pageID)
	if err != nil {
		return nil, err
	}
	return h.pages.Detail(ctx, &pagecontract.DetailReq{ProjectID: projectID, ID: pageID})
}

// Dashboard 仪表盘首页（模板 admin/dashboard.html 由 internal/templates 集中管理）。
func (h *Handle) Dashboard(c *gin.Context) {
	c.HTML(http.StatusOK, "admin/dashboard", shell.Prepare(c, gin.H{
		"title": workbenchenums.MsgDashboardTitle,
		"menu":  "dashboard",
	}))
}

// SetupWorkbenchRoutes 注册仪表盘首页（"/"）与编辑器本体全部路由。
// workbenchPages 由装配层持有（根级前缀，Session + CSRF + 权限上下文中间件已挂）。
// 不在此范围：/workbench/settings、/workbench/global（project 代理）、
// /workbench/history、/workbench/seo-score-panel（page 代理）。
func SetupWorkbenchRoutes(workbenchPages *gin.RouterGroup,
	pages pagecontract.PageService,
	projects projectcontract.ProjectService,
	blocks blockcontract.BlockService,
	plugins plugincontract.PluginService,
	collection core.CollectionResolver,
	contenttemplates contenttemplatecontract.ContentTemplateService,
	presentations TemplatePreviewPort,
	blueprints blueprintcontract.BlueprintService,
	products productcontract.ProductDataSource,
	contentStore ContentTranslationPort,
) *Handle {
	h := &Handle{
		pages: pages, projects: projects, blocks: blocks, plugins: plugins,
		collection: collection, contenttemplates: contenttemplates,
		templatePreview: presentations, blueprints: blueprints,
		products: products, contentStore: contentStore,
	}
	g := workbenchPages
	// 仪表盘首页。
	g.GET("/", h.Dashboard)
	// 编辑器外壳（?id= 页面 / ?block= 全局块 / ?template= 内容模板）。
	g.GET("/workbench", h.Workbench)
	// 预览编译直出（GET 已保存草稿 / POST 未保存草稿）。
	g.GET("/workbench/preview", h.Preview)
	g.POST("/workbench/preview", h.PreviewDraft)
	// 检查器面板片段（HTMX）：schema → 表单 HTML 由服务端渲染。
	g.POST("/workbench/inspector", h.InspectorPanel)
	// 结构树片段（HTMX）：树 HTML 由服务端渲染，客户端只做一次事件委托。
	g.POST("/workbench/outline", h.OutlineTree)
	// SEO 评分：只读分析草稿，返回评分与逐项建议。
	g.POST("/workbench/seo-score", h.SEOScore)
	// 全局块画布预览（块编辑模式 iframe 内嵌）。
	g.GET("/workbench/block/preview", h.BlockPreview)
	// 内容模板画布预览（EDT-001）：GET 已保存 / POST 未保存草稿。
	g.GET("/workbench/template/preview", h.TemplatePreview)
	g.POST("/workbench/template/preview", h.TemplatePreviewDraft)
	return h
}
