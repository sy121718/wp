package aihttp

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"context"

	"go_wp/config"
	"go_wp/pkg/mcp"
	aicontract "go_wp/internal/module/ai/contract"
	aienums "go_wp/internal/module/ai/enums"
	aimodel "go_wp/internal/module/ai/model"
	aiservice "go_wp/internal/module/ai/service"
	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	mw "go_wp/pkg/middleware"
)

func SetupAIRoutes(authorizedAPI *mw.RouteGroup, adminPages *gin.RouterGroup, db *gorm.DB, toolRegistry *mcp.Registry, mcpRoot gin.IRoutes, aiConfig sysconfigcontract.Service, dir aicontract.PermDirectory) aicontract.AIService {
	svc := aiservice.NewService(aimodel.NewAIModel(db))
	if v, err := config.GetViper(); err == nil && v != nil {
		svc.SetCipherSecret(v.GetString("app.secret"))
	}

	callLog := aimodel.NewCallLogModel(db)
	svc.SetCallLogWriter(callLog)

	handle := NewHandle(svc)

	sessionSvc := aiservice.NewSessionService(aimodel.NewSessionModel(db))

	sessionSvc.SetChatPort(svc)

	// 两个执行入口（站内工具 Runner 与外部 MCP 端点）共用同一个授权闭包：
	// 都按「登记的真实路由绑定 + Casbin 主体/对象/方法」判断，缺一即拒绝。
	authorize := casbinAuthorizer
	if toolRegistry != nil {
		sessionSvc.SetToolProvider(&toolProvider{runner: mcp.NewRunner(toolRegistry, authorize)})
	}

	sessionSvc.SetToolCallLogWriter(aimodel.NewToolCallLogModel(db))

	sessionSvc.SetCallLogReader(callLog)
	sessionHandle := NewSessionHandle(sessionSvc)

	tokenSvc := aiservice.NewAccessTokenService(aimodel.NewAccessTokenModel(db))
	tokenSvc.SetScopeValidator(func(ctx context.Context, p string) bool {
		// 令牌 scope 必须同时是「已启用的目录项」与「真实登记过路由的操作」：
		// 只看目录会放行没有执行入口的码，查询失败按拒绝处理。
		if dir == nil || len(mw.RoutesOf(p)) == 0 {
			return false
		}
		ok, err := dir.Exists(ctx, p)
		return err == nil && ok
	})
	tokenHandle := NewTokenHandle(tokenSvc)

	if mcpRoot != nil && toolRegistry != nil {
		recorder := aiservice.NewToolCallRecorder(aimodel.NewToolCallLogModel(db))
		endpoint := NewMcpEndpoint(tokenSvc, toolRegistry, recorder)
		endpoint.SetAccountAuthorizer(authorize)

		endpoint.SetConfigReader(aiConfig)
		mcpRoot.POST("/mcp", endpoint.Handle)
	}
	g := authorizedAPI.Group("/ai")

	g.GET("/provider/list", aienums.AIProviderList, handle.ListProviders)
	g.GET("/provider/get", aienums.AIProviderGet, handle.GetProvider)
	g.GET("/provider/models/list", aienums.AIProviderModelsList, handle.ListModels)

	g.POST("/provider/save", aienums.AIProviderSave, handle.SaveProvider)
	g.POST("/provider/delete", aienums.AIProviderDelete, handle.DeleteProvider)
	g.POST("/provider/status", aienums.AIProviderStatus, handle.SetProviderStatus)
	g.POST("/provider/models/save", aienums.AIProviderModelsSave, handle.SaveModels)
	g.POST("/provider/models/restore", aienums.AIProviderModelsRestore, handle.RestoreDefaultModels)
	g.POST("/provider/models/fetch", aienums.AIProviderModelsFetch, handle.FetchAvailableModels)

	// 候选与追加各有独立权限点：前者会解密密钥打远端 /models，后者会改写目录。
	g.POST("/provider/models/candidates", aienums.AIProviderModelsCandidates, handle.ModelCandidates)
	g.POST("/provider/models/append", aienums.AIProviderModelsAppend, handle.AppendModels)

	g.GET("/session/list", aienums.AISessionList, sessionHandle.List)
	g.GET("/session/get", aienums.AISessionGet, sessionHandle.Get)
	g.GET("/session/events", aienums.AISessionEvents, sessionHandle.Events)
	g.POST("/session/fold/plan", aienums.AISessionFoldPlan, sessionHandle.FoldPlan)
	g.POST("/session/append", aienums.AISessionAppend, sessionHandle.Append)
	g.POST("/session/rename", aienums.AISessionRename, sessionHandle.Rename)
	g.POST("/session/archive", aienums.AISessionArchive, sessionHandle.Archive)
	g.POST("/session/fold", aienums.AISessionFold, sessionHandle.Fold)
	g.POST("/session/send", aienums.AISessionSend, sessionHandle.Send)

	g.POST("/chat", aienums.AIChat, handle.Chat)

	g.GET("/token/list", aienums.AITokenList, tokenHandle.List)
	g.POST("/token/create", aienums.AITokenCreate, tokenHandle.Create)
	g.POST("/token/revoke", aienums.AITokenRevoke, tokenHandle.Revoke)

	// 对外接入点开关有自己的权限点：它不改令牌，改的是「本站数据要不要对外开口」。
	g.POST("/token/toggle", aienums.AITokenToggle, tokenHandle.SetMcpEnabled)

	SetupAIPages(adminPages, svc, sessionSvc, tokenSvc, toolRegistry, aiConfig, dir)

	return svc
}
