package workbenchhttp

import (
	"context"

	"go_wp/pkg/builder/core"
	"go_wp/internal/middleware/builtin"
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

	"go_wp/internal/adminui"
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
		pages: pages, projects: projects,
		collection:      collection,
		templatePreview: presentations, blueprints: blueprints,
		contentStore: contentStore,
	}

	h.svc = workbenchservice.New(pages, projects, blocks, contenttemplates, products, nil)
	h.svc.SetPlugins(plugins)

	g := workbenchPages

	g.GET("/admin", pageview.UseRenderer(adminui.RenderPage), h.Dashboard)

	g.GET("/workbench", pageview.UseRenderer(adminui.RenderPage), h.Workbench)

	g.GET("/workbench/preview", previewContext, h.Preview)
	g.POST("/workbench/preview", previewContext, h.PreviewDraft)

	g.POST("/workbench/inspector", pageview.UseRenderer(adminui.RenderBare), h.InspectorPanel)

	g.POST("/workbench/outline", pageview.UseRenderer(adminui.RenderBare), h.OutlineTree)

	g.POST("/workbench/seo-score", h.SEOScore)

	g.GET("/workbench/block/preview", previewContext, h.BlockPreview)
	g.POST("/workbench/block/preview", builtin.CasbinMiddlewareForPath("/api/block/update"), previewContext, h.BlockPreviewDraft)

	g.GET("/workbench/template/preview", previewContext, h.TemplatePreview)
	g.POST("/workbench/template/preview", previewContext, h.TemplatePreviewDraft)

	g.POST("/workbench/instance/save", builtin.CasbinMiddlewareForPath("/api/presentation/rebuild"), h.InstanceSave)

	g.POST("/workbench/navigation/create", builtin.CasbinMiddlewareForPath("/api/navigation/create"), h.InspectorNavigationCreate)
	return h
}

const (
	DashboardPageTemplate = "admin/dashboard"

	CanvasLayoutTemplate = "workbench/layout"

	InspectorPanelTemplate = "fragments/inspector_panel"
	OutlineTreeTemplate    = "fragments/outline_tree"
)
