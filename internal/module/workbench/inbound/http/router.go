// Package workbenchhttp 承载可视化工作台（编辑器本体）的后台页面入口：
// 编辑器外壳 / 预览编译直出 / 结构树 / 检查器 / SEO 评分 / 编辑器桥接。
package workbenchhttp

import (
	"context"
	"net/http"
	"sort"
	"time"

	"go_wp/internal/builder/core"
	"go_wp/internal/middleware/builtin"

	blockcontract "go_wp/internal/module/block/contract"
	blueprintcontract "go_wp/internal/module/blueprint/contract"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	plugincontract "go_wp/internal/module/plugin/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationdto "go_wp/internal/module/presentation/dto"
	productcontract "go_wp/internal/module/product/contract"
	projectcontract "go_wp/internal/module/project/contract"
	workbenchenums "go_wp/internal/module/workbench/enums"
	workbenchservice "go_wp/internal/module/workbench/service"

	"go_wp/internal/web/shell"
	"go_wp/pkg/i18n"
	"go_wp/pkg/utils"

	"github.com/gin-gonic/gin"
)

// Handle 工作台编辑器页面处理器；依赖全部经 SetupWorkbenchRoutes 参数注入。
type Handle struct {
	// pages 草稿 / 预览 / 目标解析契约。
	pages pagecontract.PageService
	// projects 站点工程契约（页面挂接主题的 settings 读取）。
	projects projectcontract.ProjectService
	// collection 集合内容解析器（装配位；预览编译下沉 page 模块后本包不直接消费，
	// 保留字段维持装配签名稳定）。
	collection core.CollectionResolver
	// templatePreview 模板预览实例端口（presentation 的最窄能力）。
	templatePreview TemplatePreviewPort
	// instances 实例编辑模式端口（docs/04-C：?instance= 画布改覆盖文档）。
	instances presentationcontract.PresentationService
	// blueprints 蓝图候选端口（可空降级；当前编辑器本体未消费，装配位保留）。
	blueprints blueprintcontract.BlueprintService
	// contentStore 内容译文读写端口（可空降级；当前编辑器本体未消费，装配位保留）。
	contentStore ContentTranslationPort
	// svc 本模块编排服务：跨模块取数（页面 / 主题 / 块 / 模板 / 导航 / 商品 / 插件）、
	// 检查器面板与结构树的 HTML 拼装、预览编译分类全在它里面。
	//
	// handler 与它的分工：handler 解析请求 → 调 svc → 渲染；svc 不认识 gin。
	// 除了下面几个「未下沉的装配位」（collection / blueprints / contentStore 与
	// templatePreview / instances 两个端口），其余跨模块能力一律经 svc 取用。
	svc *workbenchservice.Service
}

// TemplatePreviewPort 模板工作台预览所需的最窄 presentation 能力。
type TemplatePreviewPort interface {
	PreviewInstance(ctx context.Context, req *presentationdto.PreviewInstanceReq) (res *presentationdto.PreviewInstanceResp, err error)
}

// ContentTranslationPort 工作台使用的内容译文读写端口（与回迁前的 dashboard 实现同形状；
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
	// 签名保持不变（测试与外部装配直构该函数）；参数按归属拆给 svc 与 Handle 自留的端口。
	// products / navigations 不在签名里：前者由装配侧经 svc.SetProducts 注入，
	// 后者经 Handle.SetNavigationPicker 转发。
	svc := workbenchservice.New(pages, projects, blocks, contenttemplates, nil, nil)
	svc.SetPlugins(plugins)
	return &Handle{pages: pages, projects: projects, collection: collection,
		templatePreview: templatePreview, svc: svc}
}

// Dashboard 仪表盘首页（模板 admin/dashboard.html 由 internal/templates 集中管理）。
//
// 只放真实数据：统计口径全部来自 page / project 契约的只读面，本页不做任何计数缓存 ——
// 缓存会让「刚发布的页面」在概览里迟到，而概览的全部价值就是「现在是什么状态」。
func (h *Handle) Dashboard(c *gin.Context) {
	// 依赖缺失（空 Handle）时降级为空概览，而不是 panic：生产装配必然注入这些契约，
	// 但「只挂外壳、不装业务服务」的渲染测试与降级装配路径都可能走到这里 ——
	// 首页是登录后的第一跳，它挂掉等于后台进不去。
	if h.projects == nil || h.pages == nil {
		c.HTML(http.StatusOK, "admin/dashboard", shell.Prepare(c, gin.H{
			"title": workbenchenums.MsgDashboardTitle,
			"menu":  "dashboard",
		}))
		return
	}

	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		// 概览是**只读汇总页**：数据源读不到不该让整页 500 —— 页面壳照常渲染、
		// 统计区退回空态、页头说明口径暂时不可用，用户仍然能用侧栏去别的页面干活。
		// （整页 500 会把导航一起打掉，把一个"概览暂时没有数字"变成"后台进不去"。）
		c.HTML(http.StatusOK, "admin/dashboard", shell.Prepare(c, gin.H{
			"title": workbenchenums.MsgDashboardTitle,
			"menu":  "dashboard",
			"Err":   err.Error(),
		}))
		return
	}

	type dashRow struct {
		updated time.Time
		data    gin.H
	}
	var (
		pageTotal, pagePublished, pageStale int
		rows                                []dashRow
	)
	for _, p := range projects {
		list, lerr := h.pages.List(ctx, &pagedto.ListReq{ProjectID: p.ID})
		if lerr != nil {
			// 单个工程读失败不让整个概览不可用：跳过它，其余照常汇总。
			continue
		}
		for _, pg := range list {
			published := pg.ActiveArtifactID != nil || pg.ActivePath != nil
			pageTotal++
			if published {
				pagePublished++
			}
			if pg.Stale {
				pageStale++
			}
			rows = append(rows, dashRow{
				updated: pg.UpdatedAt.Time(),
				data: gin.H{
					"ID": pg.ID, "Project": p.Name, "Path": pg.DraftPath, "Kind": pg.Kind,
					"Published": published, "Stale": pg.Stale, "Version": pg.DraftVersion,
					"UpdatedAt": pg.UpdatedAt.Time().Format(utils.LayoutSecond),
				},
			})
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].updated.After(rows[j].updated) })
	// 概览只列最近的一屏：更早的走 /admin/pages 的完整列表与筛选。
	const recentLimit = 8
	recentPages := make([]gin.H, 0, recentLimit)
	for i, r := range rows {
		if i >= recentLimit {
			break
		}
		recentPages = append(recentPages, r.data)
	}

	c.HTML(http.StatusOK, "admin/dashboard", shell.Prepare(c, gin.H{
		"title":         workbenchenums.MsgDashboardTitle,
		"menu":          "dashboard",
		"ProjectCount":  len(projects),
		"PageTotal":     pageTotal,
		"PagePublished": pagePublished,
		"PageDraft":     pageTotal - pagePublished,
		"PageStale":     pageStale,
		"RecentPages":   recentPages,
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
		pages: pages, projects: projects,
		collection:      collection,
		templatePreview: presentations, blueprints: blueprints,
		contentStore: contentStore,
	}
	// 编排服务的构造与 Handle 同源：装配签名不变，注入点（SetInstanceOverrideDeps /
	// SetNavigationPicker）也照旧 —— 只是后者现在转发给 svc。
	h.svc = workbenchservice.New(pages, projects, blocks, contenttemplates, products, nil)
	h.svc.SetPlugins(plugins)
	g := workbenchPages
	// 仪表盘只挂 /admin。
	//
	// 曾经同时挂 /（历史入口）与 /admin（菜单树里「仪表盘」的 path）。
	// 现在 **/ 归前台首页**（站点独占域名根，与生产部署一致）——
	// 再在根上挂控制台页面会把前台首页整个顶掉，而且从后台点「仪表盘」
	// 会跳到店铺首页，看起来像登录失效。菜单 path 本来就是 /admin，无需改动。
	g.GET("/admin", h.Dashboard)
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
	// 实例编辑模式保存（docs/04-C）：覆盖文档 + 重编译发布，只改本实例。
	//
	// Casbin 必须在这里显式挂（与下一行 navigation/create 同一形态）：workbenchPages
	// 组只挂了 Session+CSRF+权限上下文（internal/routers/assembly.go），组链里**没有**
	// 鉴权判定 —— 漏挂的后果是任意已登录账号（含只读角色）都能写。
	//
	// 权限点复用 API 侧的 presentation:rebuild：本端点与 POST /api/presentation/rebuild
	// 是同一件事（改实例文档并重编译发布），而「页面写端点复用 API 权限点」是全仓库的
	// 既定口径（11 个模块都这么做），新增独立权限点会让「权限点 ↔ 路由」多一份重复真源。
	g.POST("/workbench/instance/save", builtin.CasbinMiddlewareForPath("/api/presentation/rebuild"), h.InstanceSave)
	// 检查器内就地新建菜单项（nav 组件的「具体菜单项」字段）：写回走 navigation 契约的
	// Create，权限点沿用既有 /api/navigation/create —— workbenchPages 组只挂了
	// Session+CSRF+权限上下文，Casbin 要在这里显式挂，否则是「页面没挂鉴权」。
	g.POST("/workbench/navigation/create", builtin.CasbinMiddlewareForPath("/api/navigation/create"), h.InspectorNavigationCreate)
	return h
}
