package aihttp

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"go_wp/config"
	"go_wp/internal/mcp"
	aicontract "go_wp/internal/module/ai/contract"
	aimodel "go_wp/internal/module/ai/model"
	aiservice "go_wp/internal/module/ai/service"
	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	"go_wp/internal/permission"
)

func SetupAIRoutes(authorizedAPI *permission.RouteGroup, adminPages *gin.RouterGroup, db *gorm.DB, toolRegistry *mcp.Registry, mcpRoot gin.IRoutes, aiConfig sysconfigcontract.Service) aicontract.AIService {
	svc := aiservice.NewService(aimodel.NewAIModel(db))
	if v, err := config.GetViper(); err == nil && v != nil {
		svc.SetCipherSecret(v.GetString("app.secret"))
	}

	callLog := aimodel.NewCallLogModel(db)
	svc.SetCallLogWriter(callLog)

	handle := NewHandle(svc)

	sessionSvc := aiservice.NewSessionService(aimodel.NewSessionModel(db))

	sessionSvc.SetChatPort(svc)

	if toolRegistry != nil {
		sessionSvc.SetToolProvider(&toolProvider{runner: mcp.NewRunner(toolRegistry, casbinAuthorizer)})
	}

	sessionSvc.SetToolCallLogWriter(aimodel.NewToolCallLogModel(db))

	sessionSvc.SetCallLogReader(callLog)
	sessionHandle := NewSessionHandle(sessionSvc)

	tokenSvc := aiservice.NewAccessTokenService(aimodel.NewAccessTokenModel(db))
	tokenSvc.SetScopeValidator(func(p string) bool { return permission.Known(permission.Perm(p)) })
	tokenHandle := NewTokenHandle(tokenSvc)

	if mcpRoot != nil && toolRegistry != nil {
		recorder := aiservice.NewToolCallRecorder(aimodel.NewToolCallLogModel(db))
		endpoint := NewMcpEndpoint(tokenSvc, toolRegistry, recorder)

		endpoint.SetConfigReader(aiConfig)
		mcpRoot.POST("/mcp", endpoint.Handle)
	}
	g := authorizedAPI.Group("/ai")

	g.GET("/provider/list", permission.AIProviderList, handle.ListProviders)
	g.GET("/provider/get", permission.AIProviderGet, handle.GetProvider)
	g.GET("/provider/models/list", permission.AIProviderModelsList, handle.ListModels)

	g.POST("/provider/save", permission.AIProviderSave, handle.SaveProvider)
	g.POST("/provider/delete", permission.AIProviderDelete, handle.DeleteProvider)
	g.POST("/provider/status", permission.AIProviderStatus, handle.SetProviderStatus)
	g.POST("/provider/models/save", permission.AIProviderModelsSave, handle.SaveModels)
	g.POST("/provider/models/restore", permission.AIProviderModelsRestore, handle.RestoreDefaultModels)
	g.POST("/provider/models/fetch", permission.AIProviderModelsFetch, handle.FetchAvailableModels)

	// 候选与追加各有独立权限点：前者会解密密钥打远端 /models，后者会改写目录。
	g.POST("/provider/models/candidates", permission.AIProviderModelsCandidates, handle.ModelCandidates)
	g.POST("/provider/models/append", permission.AIProviderModelsAppend, handle.AppendModels)

	g.GET("/session/list", permission.AISessionList, sessionHandle.List)
	g.GET("/session/get", permission.AISessionGet, sessionHandle.Get)
	g.GET("/session/events", permission.AISessionEvents, sessionHandle.Events)
	g.POST("/session/fold/plan", permission.AISessionFoldPlan, sessionHandle.FoldPlan)
	g.POST("/session/append", permission.AISessionAppend, sessionHandle.Append)
	g.POST("/session/rename", permission.AISessionRename, sessionHandle.Rename)
	g.POST("/session/archive", permission.AISessionArchive, sessionHandle.Archive)
	g.POST("/session/fold", permission.AISessionFold, sessionHandle.Fold)
	g.POST("/session/send", permission.AISessionSend, sessionHandle.Send)

	g.POST("/chat", permission.AIChat, handle.Chat)

	g.GET("/token/list", permission.AITokenList, tokenHandle.List)
	g.POST("/token/create", permission.AITokenCreate, tokenHandle.Create)
	g.POST("/token/revoke", permission.AITokenRevoke, tokenHandle.Revoke)

	// 对外接入点开关有自己的权限点：它不改令牌，改的是「本站数据要不要对外开口」。
	g.POST("/token/toggle", permission.AITokenToggle, tokenHandle.SetMcpEnabled)

	SetupAIPages(adminPages, svc, sessionSvc, tokenSvc, toolRegistry, aiConfig)

	return svc
}
