package workbenchhttp

import (
	"context"
	"net/http"

	mw "go_wp/pkg/middleware"

	"go_wp/internal/middleware/builtin"
	"go_wp/pkg/builder/core"
	pageview "go_wp/pkg/view"

	blockcontract "go_wp/internal/module/block/contract"
	blueprintcontract "go_wp/internal/module/blueprint/contract"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	pagecontract "go_wp/internal/module/page/contract"
	plugincontract "go_wp/internal/module/plugin/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationdto "go_wp/internal/module/presentation/dto"
	productcontract "go_wp/internal/module/product/contract"
	projectcontract "go_wp/internal/module/project/contract"
	workbenchservice "go_wp/internal/module/workbench/service"
	"go_wp/pkg/i18n"

	"github.com/gin-gonic/gin"
)

type Handle struct {
	pages pagecontract.PageService

	projects projectcontract.ProjectService

	collection core.CollectionResolver

	templatePreview TemplatePreviewPort

	instances presentationcontract.PresentationService

	blueprints blueprintcontract.BlueprintService

	contentStore ContentTranslationPort

	svc *workbenchservice.Service

	overviewOrders     OverviewOrderPort
	overviewAnalytics  OverviewAnalyticsPort
	overviewPageKinds  OverviewPageKindPort
	overviewCurrencies OverviewCurrencyPort
}

type TemplatePreviewPort interface {
	PreviewInstance(ctx context.Context, req *presentationdto.PreviewInstanceReq) (res *presentationdto.PreviewInstanceResp, err error)
}

type ContentTranslationPort interface {
	LoadDetails(ctx context.Context, lang string, hashes []string) (map[string]i18n.ContentTargetInfo, error)
	LoadDetailsForProject(ctx context.Context, projectID, lang string, hashes []string) (map[string]i18n.ContentTargetInfo, error)
	LoadTargets(ctx context.Context, lang string, hashes []string) (map[string]string, error)
	LoadTargetsForProject(ctx context.Context, projectID, lang string, hashes []string) (map[string]string, error)
	Upsert(ctx context.Context, items []i18n.ContentWriteItem) (written int, err error)
}

func New(pages pagecontract.PageService, projects projectcontract.ProjectService,
	blocks blockcontract.BlockService, plugins plugincontract.PluginService,
	collection core.CollectionResolver, contenttemplates contenttemplatecontract.ContentTemplateService,
	templatePreview TemplatePreviewPort) *Handle {

	svc := workbenchservice.New(pages, projects, blocks, contenttemplates, nil, nil)
	svc.SetPlugins(plugins)
	return &Handle{pages: pages, projects: projects, collection: collection,
		templatePreview: templatePreview, svc: svc}
}

func SetupWorkbenchRoutes(reg *mw.Registry, workbenchPages *gin.RouterGroup,
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
		pages: pages, projects: projects,
		collection:      collection,
		templatePreview: presentations, blueprints: blueprints,
		contentStore: contentStore,
	}

	h.svc = workbenchservice.New(pages, projects, blocks, contenttemplates, products, nil)
	h.svc.SetPlugins(plugins)

	g := workbenchPages

	registerWorkbenchShellPages(reg, g, h)

	g.GET("/workbench/preview", builtin.CasbinMiddlewareForPathAs("/api/page/draft/save", http.MethodPost), previewContext, h.Preview)
	mw.DeclarePageGetAsOn(reg, "/workbench/preview", "/api/page/draft/save", http.MethodPost)
	g.POST("/workbench/preview", builtin.CasbinMiddlewareForPath("/api/page/draft/save"), previewContext, h.PreviewDraft)
	mw.DeclarePageWriteOn(reg, http.MethodPost, "/workbench/preview", "/api/page/draft/save", http.MethodPost)

	g.POST("/workbench/inspector", builtin.CasbinMiddlewareForPath("/api/page/draft/save"),
		pageview.UseRenderer(pageview.RenderBare), h.InspectorPanel)
	mw.DeclarePageWriteOn(reg, http.MethodPost, "/workbench/inspector", "/api/page/draft/save", http.MethodPost)

	g.POST("/workbench/outline", builtin.CasbinMiddlewareForPath("/api/page/draft/save"),
		pageview.UseRenderer(pageview.RenderBare), h.OutlineTree)
	mw.DeclarePageWriteOn(reg, http.MethodPost, "/workbench/outline", "/api/page/draft/save", http.MethodPost)

	g.POST("/workbench/seo-score", builtin.CasbinMiddlewareForPath("/api/page/draft/save"), h.SEOScore)
	mw.DeclarePageWriteOn(reg, http.MethodPost, "/workbench/seo-score", "/api/page/draft/save", http.MethodPost)

	g.GET("/workbench/block/preview", builtin.CasbinMiddlewareForPathAs("/api/block/update", http.MethodPost), previewContext, h.BlockPreview)
	mw.DeclarePageGetAsOn(reg, "/workbench/block/preview", "/api/block/update", http.MethodPost)
	g.POST("/workbench/block/preview", builtin.CasbinMiddlewareForPath("/api/block/update"), previewContext, h.BlockPreviewDraft)
	mw.DeclarePageWriteOn(reg, http.MethodPost, "/workbench/block/preview", "/api/block/update", http.MethodPost)

	g.GET("/workbench/template/preview", builtin.CasbinMiddlewareForPathAs("/api/contenttemplate/update", http.MethodPost), previewContext, h.TemplatePreview)
	mw.DeclarePageGetAsOn(reg, "/workbench/template/preview", "/api/contenttemplate/update", http.MethodPost)
	g.POST("/workbench/template/preview", builtin.CasbinMiddlewareForPath("/api/contenttemplate/update"), previewContext, h.TemplatePreviewDraft)
	mw.DeclarePageWriteOn(reg, http.MethodPost, "/workbench/template/preview", "/api/contenttemplate/update", http.MethodPost)

	g.POST("/workbench/instance/save", builtin.CasbinMiddlewareForPath("/api/presentation/rebuild"), h.InstanceSave)
	mw.DeclarePageWriteOn(reg, http.MethodPost, "/workbench/instance/save", "/api/presentation/rebuild", http.MethodPost)

	g.POST("/workbench/navigation/create", builtin.CasbinMiddlewareForPath("/api/navigation/create"), h.InspectorNavigationCreate)
	mw.DeclarePageWriteOn(reg, http.MethodPost, "/workbench/navigation/create", "/api/navigation/create", http.MethodPost)
	return h
}

const (
	DashboardPageTemplate = "admin/dashboard"

	CanvasLayoutTemplate = "workbench/layout"

	InspectorPanelTemplate = "fragments/inspector_panel"
	OutlineTreeTemplate    = "fragments/outline_tree"
)

// registerWorkbenchShellPages 注册首页与工作台两个后台页面 GET。
// 鉴权对象取页面列表：这是后台入口的最小读取权限，不读业务数据。
func registerWorkbenchShellPages(reg *mw.Registry, g *gin.RouterGroup, h *Handle) {
	shell := []struct {
		path    string
		handler gin.HandlerFunc
	}{
		{"/admin", h.Dashboard},
		{"/workbench", h.Workbench},
	}
	for _, page := range shell {
		g.GET(page.path, builtin.CasbinMiddlewareForPath("/api/page/list"),
			pageview.UseRenderer(pageview.RenderPage), page.handler)
		mw.DeclarePageGetOn(reg, page.path, "/api/page/list")
	}
}
