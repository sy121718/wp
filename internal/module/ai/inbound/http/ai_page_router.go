package aihttp

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/internal/mcp"
	"go_wp/internal/middleware/builtin"
	aicontract "go_wp/internal/module/ai/contract"
	aiservice "go_wp/internal/module/ai/service"
	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
)

const FabResultTemplate = "partials/ai_fab_result"

func SetupAIPages(adminPages *gin.RouterGroup, svc aicontract.AIService,
	sessionSvc *aiservice.SessionService, tokenSvc *aiservice.AccessTokenService,
	toolRegistry *mcp.Registry, aiConfig sysconfigcontract.Service) {
	if adminPages == nil {
		return
	}
	page := NewPageHandle(svc)

	mcpPage := NewMcpPageHandle(tokenSvc, toolRegistry)

	mcpPage.SetConfigService(aiConfig)
	adminPages.GET("/ai/mcp", adminui.PageAuthz("/api/ai/token/list"), adminui.PageSlot(McpPageTemplate), mcpPage.Page)
	adminPages.POST("/ai/mcp/token/create", builtin.CasbinMiddlewareForPath("/api/ai/token/create"), adminui.BareSlot(McpTokensPartialTemplate), mcpPage.TokenCreate)
	adminPages.POST("/ai/mcp/token/revoke", builtin.CasbinMiddlewareForPath("/api/ai/token/revoke"), adminui.BareSlot(McpTokensPartialTemplate), mcpPage.TokenRevoke)

	adminPages.POST("/ai/mcp/toggle", builtin.CasbinMiddlewareForPath("/api/ai/token/create"), mcpPage.McpToggle)

	adminPages.GET("/ai/providers", adminui.PageAuthz("/api/ai/provider/list"), page.ProvidersPage)

	adminPages.POST("/ai/providers/save", builtin.CasbinMiddlewareForPath("/api/ai/provider/save"), page.ProviderSave)
	adminPages.POST("/ai/providers/delete", builtin.CasbinMiddlewareForPath("/api/ai/provider/delete"), page.ProviderDelete)
	adminPages.POST("/ai/providers/status", builtin.CasbinMiddlewareForPath("/api/ai/provider/status"), page.ProviderStatus)

	adminPages.POST("/ai/providers/models/save", builtin.CasbinMiddlewareForPath("/api/ai/provider/models/save"), page.ModelsSave)
	adminPages.POST("/ai/providers/models/restore", builtin.CasbinMiddlewareForPath("/api/ai/provider/models/restore"), page.ModelsRestore)
	adminPages.POST("/ai/providers/models/fetch", builtin.CasbinMiddlewareForPath("/api/ai/provider/models/fetch"), page.ModelsFetch)

	adminPages.POST("/ai/providers/models/row/add", builtin.CasbinMiddlewareForPath("/api/ai/provider/models/save"), page.ModelsRowAdd)
	adminPages.POST("/ai/providers/models/row/delete", builtin.CasbinMiddlewareForPath("/api/ai/provider/models/save"), page.ModelsRowDelete)

	adminPages.POST("/ai/providers/models/candidates", builtin.CasbinMiddlewareForPath("/api/ai/provider/models/fetch"), adminui.BareSlot(ProviderModelsPartialTemplate), page.ModelsCandidates)
	adminPages.POST("/ai/providers/models/append", builtin.CasbinMiddlewareForPath("/api/ai/provider/models/save"), adminui.BareSlot(ProviderModelsPartialTemplate), page.ModelsAppend)

	sessionPage := NewSessionPageHandle(sessionSvc, svc)
	adminPages.GET("/ai/sessions", adminui.PageAuthz("/api/ai/session/list"), adminui.PageSlot(SessionsPageTemplate), sessionPage.SessionsPage)
	adminPages.POST("/ai/sessions/append", builtin.CasbinMiddlewareForPath("/api/ai/session/append"), sessionPage.SessionAppend)
	adminPages.POST("/ai/sessions/rename", builtin.CasbinMiddlewareForPath("/api/ai/session/rename"), sessionPage.SessionRename)
	adminPages.POST("/ai/sessions/archive", builtin.CasbinMiddlewareForPath("/api/ai/session/archive"), sessionPage.SessionArchive)
	adminPages.POST("/ai/sessions/fold", builtin.CasbinMiddlewareForPath("/api/ai/session/fold"), sessionPage.SessionFold)

	adminPages.POST("/ai/sessions/send", builtin.CasbinMiddlewareForPath("/api/ai/chat"), sessionPage.SessionSend)

	adminPages.POST("/ai/ask",
		builtin.CasbinMiddlewareForPath("/api/ai/chat"),
		adminui.BareSlot(FabResultTemplate),
		sessionPage.FabAsk)

	adminPages.POST("/ai/ask/stream", builtin.CasbinMiddlewareForPath("/api/ai/chat"), sessionPage.FabAskStream)

	adminPages.GET("/ai/fab/history", builtin.CasbinMiddlewareForPathAs("/api/ai/session/events", http.MethodGet), sessionPage.FabHistory)
}

const (
	McpPageTemplate = "admin/ai/mcp"

	McpTokensPartialTemplate = "admin/ai/mcp_tokens"

	SessionsPageTemplate = "admin/ai/sessions"

	ProviderModelsPartialTemplate = "admin/ai/provider_models"

	ProviderPickerPartialTemplate = "admin/ai/provider_picker"
)
