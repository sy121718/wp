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

func SetupAIPages(adminPages *gin.RouterGroup, svc aicontract.AIService,
	sessionSvc *aiservice.SessionService, tokenSvc *aiservice.AccessTokenService,
	toolRegistry *mcp.Registry, aiConfig sysconfigcontract.Service) {
	if adminPages == nil {
		return
	}
	sessionPage := NewSessionPageHandle(sessionSvc, svc)

	mcpPage := NewMcpPageHandle(tokenSvc, toolRegistry)

	mcpPage.SetConfigService(aiConfig)
	adminPages.GET("/ai/mcp", adminui.PageAuthz("/api/ai/token/list"), adminui.PageSlot(McpPageTemplate), mcpPage.Page)

	// 令牌的生成 / 撤销 / 对外接入点开关都只有 /api 一条入口（ai_router.go 的
	// /ai/token/{create,revoke,toggle}）：页面侧的 POST 双轨已经清退。

	// 老地址：532 迁移把「AI 模型」菜单并进了 /admin/ai/sessions?tab=models，
	// 这条留着只为旧书签不 404，渲染的是同一个页面（不发 302）。
	adminPages.GET("/ai/providers", adminui.PageAuthz("/api/ai/provider/list"), adminui.PageSlot(SessionsPageTemplate), sessionPage.ProvidersPage)

	// 供应商与模型的写操作只有 /api 一条入口（见 ai_router.go 的 /ai/provider/*）：
	// 页面侧的 POST 双轨已经清退 —— 同一件事有两个 URL 就必然有两份绑定与两份出口，
	// 前端也永远不知道哪一条才是真的。写基座直接打 /api，这里不再保留转发路由。

	adminPages.GET("/ai/sessions", adminui.PageAuthz("/api/ai/session/list"), adminui.PageSlot(SessionsPageTemplate), sessionPage.SessionsPage)

	// 会话的 append / rename / archive / fold / send 同样只有 /api 一条入口
	// （ai_router.go 的 /ai/session/* 与 /ai/chat）。

	// 例外：SSE。它是字节流（AGENTS「响应协议」：字节流直接写 body 后 return ErrHandled），
	// 不是 {code,message,data} 的 JSON 出口 —— 流式响应没法先给一个 JSON 信封。
	// 所以这条留在页面组上，但它读的仍是 /api/ai/chat 的权限点。
	adminPages.POST("/ai/ask/stream", builtin.CasbinMiddlewareForPath("/api/ai/chat"), sessionPage.FabAskStream)

	adminPages.GET("/ai/fab/history", builtin.CasbinMiddlewareForPathAs("/api/ai/session/events", http.MethodGet), sessionPage.FabHistory)
}

const (
	McpPageTemplate = "admin/ai/mcp"

	SessionsPageTemplate = "admin/ai/sessions"

	ProviderPickerPartialTemplate = "admin/ai/provider_picker"
)
