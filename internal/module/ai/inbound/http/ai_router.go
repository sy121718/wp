// ai_router.go — ai 模块自装配：建 model / service、注册 JSON 接口与后台页面路由。
//
// 装配口径与 inventory / webhook 一致：本模块自己取 db 造仓储与服务；密钥来源沿用项目
// 既有配置项 `app.secret`（与 webhook / mail / inventory 同源），不新造配置项。
package aihttp

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"go_wp/config"
	"go_wp/internal/mcp"
	"go_wp/internal/middleware/builtin"
	aicontract "go_wp/internal/module/ai/contract"
	aimodel "go_wp/internal/module/ai/model"
	aiservice "go_wp/internal/module/ai/service"
	"go_wp/internal/permission"
)

// SetupAIRoutes 装配 ai 模块并注册路由，返回给装配层（供其它模块以契约消费）。
//
// toolRegistry 是**跨模块**的工具注册表（各领域模块在装配期把自己的只读工具注册进去）：
// 传 nil 表示不接工具（用例测试与轻装配场景），此时会话页退化成不带工具的一问一答。
// 注册表是指针，且各模块的装配顺序在本调用之后 —— 「先建、后填、运行期读」正是它能工作的原因，
// 不要在这里缓存它的内容（见 ai_tools_wire.go 关于 Specs 的注释）。
//
// adminPages 为 nil 时只挂 JSON 接口（用例测试与轻装配场景）。
// mcpRoot 是**根路由**（不是 /api 下的组）：外部接入点的路径就是 `/mcp`，
// 且它自带 PAT 鉴权，不走 authorizedAPI 的三层链（会话 / CSRF / Casbin）——
// 传 nil 表示不暴露外部接入点（装配层的显式选择，而不是「忘了传」）。
func SetupAIRoutes(authorizedAPI *permission.RouteGroup, adminPages *gin.RouterGroup, db *gorm.DB, toolRegistry *mcp.Registry, mcpRoot gin.IRoutes) aicontract.AIService {
	svc := aiservice.NewService(aimodel.NewAIModel(db))
	if v, err := config.GetViper(); err == nil && v != nil {
		svc.SetCipherSecret(v.GetString("app.secret"))
	}
	// 调用流水（ai_call_log）一个实例两个方向：
	//   · 写侧注到**出站层** —— 流水记的是「打了一次上游」，唯一出站点是 Service.Chat，
	//     挂在那里无论谁发起对话都记得到（详见 ai_call_log.go）；
	//   · 读侧注到**会话层** —— 会话行悬浮卡要显示「最近调用」（详见 ai_session_calls.go）。
	// 同一个 CallLogModel 即可：它无状态，只是这张表的访问入口。
	callLog := aimodel.NewCallLogModel(db)
	svc.SetCallLogWriter(callLog)

	handle := NewHandle(svc)
	// 会话层与配置层零耦合：会话只记 provider_key / model_id 两个字符串，用独立的 model 与 service，
	// 不 import 配置面的 Service。
	sessionSvc := aiservice.NewSessionService(aimodel.NewSessionModel(db))
	// 对话能力以窄接口注入，sessionSvc 不 import 配置面的 Service：会话页的「发消息」走这条路。
	sessionSvc.SetChatPort(svc)
	// 工具能力（可选增强）：注册表由各领域模块在装配期填入，权限判定复用 Casbin
	//（obj=路由路径、act=HTTP 方法，与页面中间件同一套策略，见 ai_tools_wire.go）。
	// 未传注册表时不注入 —— 会话页照常一问一答，而不是整个发消息功能不可用。
	if toolRegistry != nil {
		sessionSvc.SetToolProvider(&toolProvider{runner: mcp.NewRunner(toolRegistry, casbinAuthorizer)})
	}
	// 工具调用流水（审计）：写法与调用流水一致（旁路观测、异步写）。
	// 未注入时审计是空操作 —— 审计写不进去不该让对话失败（见 ai_tool_call_log.go）。
	sessionSvc.SetToolCallLogWriter(aimodel.NewToolCallLogModel(db))
	// 调用流水的读侧（悬浮卡的「最近调用」）。
	sessionSvc.SetCallLogReader(callLog)
	sessionHandle := NewSessionHandle(sessionSvc)
	// 对外访问令牌（PAT）：签发 / 列表 / 撤销。
	// 权限点的**存在性**校验由装配层注入（service 层不 import permission 包，与工具判权限同纪律）。
	tokenSvc := aiservice.NewAccessTokenService(aimodel.NewAccessTokenModel(db))
	tokenSvc.SetScopeValidator(func(p string) bool { return permission.Known(permission.Perm(p)) })
	tokenHandle := NewTokenHandle(tokenSvc)

	// 外部接入点 POST /mcp（MCP over JSON-RPC）：身份是 PAT，权限是「令牌 scope ∩ 账号权限」。
	// 与工具同源：工具清单来自同一个注册表，所以「站内助手能查的」与「外部能查的」是同一组能力，
	// 差别只在**谁被允许调哪些**。
	if mcpRoot != nil && toolRegistry != nil {
		recorder := aiservice.NewToolCallRecorder(aimodel.NewToolCallLogModel(db))
		mcpRoot.POST("/mcp", NewMcpEndpoint(tokenSvc, toolRegistry, recorder).Handle)
	}
	g := authorizedAPI.Group("/ai")

	// 权限口径：一码一路由（sys_permission 的 permission_code 唯一），配置面按资源逐个列点。
	// 供应商查询。
	g.GET("/provider/list", permission.AIProviderList, handle.ListProviders)
	g.GET("/provider/get", permission.AIProviderGet, handle.GetProvider)
	g.GET("/provider/models/list", permission.AIProviderModelsList, handle.ListModels)

	// 供应商写入：保存 / 删除 / 启停，以及模型目录的保存 / 恢复默认 / 拉取可用。
	g.POST("/provider/save", permission.AIProviderSave, handle.SaveProvider)
	g.POST("/provider/delete", permission.AIProviderDelete, handle.DeleteProvider)
	g.POST("/provider/status", permission.AIProviderStatus, handle.SetProviderStatus)
	g.POST("/provider/models/save", permission.AIProviderModelsSave, handle.SaveModels)
	g.POST("/provider/models/restore", permission.AIProviderModelsRestore, handle.RestoreDefaultModels)
	g.POST("/provider/models/fetch", permission.AIProviderModelsFetch, handle.FetchAvailableModels)

	// 会话层：查询（列表 / 详情 / 事件）与折叠建议各用各自的权限点；
	// 写入（追加 / 改名 / 归档 / 折叠）同样一码一路由，不共用聚合码。
	g.GET("/session/list", permission.AISessionList, sessionHandle.List)
	g.GET("/session/get", permission.AISessionGet, sessionHandle.Get)
	g.GET("/session/events", permission.AISessionEvents, sessionHandle.Events)
	g.POST("/session/fold/plan", permission.AISessionFoldPlan, sessionHandle.FoldPlan)
	g.POST("/session/append", permission.AISessionAppend, sessionHandle.Append)
	g.POST("/session/rename", permission.AISessionRename, sessionHandle.Rename)
	g.POST("/session/archive", permission.AISessionArchive, sessionHandle.Archive)
	g.POST("/session/fold", permission.AISessionFold, sessionHandle.Fold)

	// 对话入口：一次请求一个模型，权限点独立。
	g.POST("/chat", permission.AIChat, handle.Chat)

	// 对外访问令牌：一码一路由（列表 / 签发 / 撤销各一个权限点）。
	g.GET("/token/list", permission.AITokenList, tokenHandle.List)
	g.POST("/token/create", permission.AITokenCreate, tokenHandle.Create)
	g.POST("/token/revoke", permission.AITokenRevoke, tokenHandle.Revoke)

	if adminPages != nil {
		page := NewPageHandle(svc)
		// MCP 与外部访问页：读权限点用 token/list（与菜单的 permission_code 同一个值 ——
		// 两处不一致会出现「菜单看得见、点进去 403」），两个写操作各用自己的权限点。
		mcpPage := NewMcpPageHandle(tokenSvc, toolRegistry)
		adminPages.GET("/ai/mcp", builtin.CasbinMiddlewareForPathAs("/api/ai/token/list", http.MethodGet), mcpPage.Page)
		adminPages.POST("/ai/mcp/token/create", builtin.CasbinMiddlewareForPath("/api/ai/token/create"), mcpPage.TokenCreate)
		adminPages.POST("/ai/mcp/token/revoke", builtin.CasbinMiddlewareForPath("/api/ai/token/revoke"), mcpPage.TokenRevoke)
		// 页面路径与权限点路径不同，必须显式指定 casbin obj（口径见 sysconfig 页面路由）。
		adminPages.GET("/ai/providers", builtin.CasbinMiddlewareForPathAs("/api/ai/provider/list", http.MethodGet), page.ProvidersPage)

		adminPages.POST("/ai/providers/save", builtin.CasbinMiddlewareForPath("/api/ai/provider/save"), page.ProviderSave)
		adminPages.POST("/ai/providers/delete", builtin.CasbinMiddlewareForPath("/api/ai/provider/delete"), page.ProviderDelete)
		adminPages.POST("/ai/providers/status", builtin.CasbinMiddlewareForPath("/api/ai/provider/status"), page.ProviderStatus)

		adminPages.POST("/ai/providers/models/save", builtin.CasbinMiddlewareForPath("/api/ai/provider/models/save"), page.ModelsSave)
		adminPages.POST("/ai/providers/models/restore", builtin.CasbinMiddlewareForPath("/api/ai/provider/models/restore"), page.ModelsRestore)
		adminPages.POST("/ai/providers/models/fetch", builtin.CasbinMiddlewareForPath("/api/ai/provider/models/fetch"), page.ModelsFetch)
		// 增删行不改库，只重渲染目录区，沿用保存的权限点。
		adminPages.POST("/ai/providers/models/row/add", builtin.CasbinMiddlewareForPath("/api/ai/provider/models/save"), page.ModelsRowAdd)
		adminPages.POST("/ai/providers/models/row/delete", builtin.CasbinMiddlewareForPath("/api/ai/provider/models/save"), page.ModelsRowDelete)
		// 候选拉取沿用 fetch 权限点（同一能力：谁能拉候选，谁就能走旧的直接拉取）；
		// 勾选后追加沿用 save 权限点（落到目录里的写操作只有一个口径）。
		adminPages.POST("/ai/providers/models/candidates", builtin.CasbinMiddlewareForPath("/api/ai/provider/models/fetch"), page.ModelsCandidates)
		adminPages.POST("/ai/providers/models/append", builtin.CasbinMiddlewareForPath("/api/ai/provider/models/save"), page.ModelsAppend)

		// 会话页：路径与权限点路径不一致，逐个显式指定 casbin obj（口径同上）。
		sessionPage := NewSessionPageHandle(sessionSvc, svc)
		adminPages.GET("/ai/sessions", builtin.CasbinMiddlewareForPathAs("/api/ai/session/list", http.MethodGet), sessionPage.SessionsPage)
		adminPages.POST("/ai/sessions/append", builtin.CasbinMiddlewareForPath("/api/ai/session/append"), sessionPage.SessionAppend)
		adminPages.POST("/ai/sessions/rename", builtin.CasbinMiddlewareForPath("/api/ai/session/rename"), sessionPage.SessionRename)
		adminPages.POST("/ai/sessions/archive", builtin.CasbinMiddlewareForPath("/api/ai/session/archive"), sessionPage.SessionArchive)
		adminPages.POST("/ai/sessions/fold", builtin.CasbinMiddlewareForPath("/api/ai/session/fold"), sessionPage.SessionFold)
		// 发消息借对话入口的 casbin obj（发消息本质是一次对话），不新增权限点。
		adminPages.POST("/ai/sessions/send", builtin.CasbinMiddlewareForPath("/api/ai/chat"), sessionPage.SessionSend)
	}

	return svc
}
