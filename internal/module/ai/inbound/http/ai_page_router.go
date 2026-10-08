package aihttp

// ai_page_router.go — AI 后台页面（/admin/ai/*）与全局悬浮球的注册落点。
//
// 只做注册：`/admin` 组的中间件链（Session + CSRF + 权限上下文）由装配层统一挂好，
// handler 在 ai_page.go。路由注册只出现在 *_router.go，门禁
// scripts/check-route-registration-placement.sh 守这条。
//
// 页面路径与权限点路径不一致（页面前缀 /admin/ai/*，权限点 /api/ai/*），必须显式指定
// casbin obj —— 直接按页面路径 enforce 会全员 403（权限点表里没有页面路径）。
// 页面 GET 已挂 Casbin（借对应读权限点，含 CasbinMiddlewareForPathAs 的动词修正）。

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"go_wp/internal/mcp"
	"go_wp/internal/middleware/builtin"
	aicontract "go_wp/internal/module/ai/contract"
	aiservice "go_wp/internal/module/ai/service"
	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	"go_wp/internal/shell"
)

// SetupAIPages 注册 AI 后台页面与悬浮球；adminPages 为 nil 时整体跳过。
//
// 会话层与令牌层用具体 service 类型（构造页面处理器要它们的具体方法），
// 与 ai_router.go 的 API 装配共用同一批实例 —— 不是第二份。
func SetupAIPages(adminPages *gin.RouterGroup, svc aicontract.AIService,
	sessionSvc *aiservice.SessionService, tokenSvc *aiservice.AccessTokenService,
	toolRegistry *mcp.Registry, aiConfig sysconfigcontract.Service) {
	if adminPages == nil {
		return
	}
	page := NewPageHandle(svc)
	// MCP 与外部访问页：读权限点用 token/list（与菜单的 permission_code 同一个值 ——
	// 两处不一致会出现「菜单看得见、点进去 403」），两个写操作各用自己的权限点。
	mcpPage := NewMcpPageHandle(tokenSvc, toolRegistry)
	// 开关状态与切换都走 sysconfig（GroupAI / mcp_enabled）；与服务端可达性判定同源。
	mcpPage.SetConfigService(aiConfig)
	adminPages.GET("/ai/mcp", shell.PageAuthz("/api/ai/token/list"), mcpPage.Page)
	adminPages.POST("/ai/mcp/token/create", builtin.CasbinMiddlewareForPath("/api/ai/token/create"), mcpPage.TokenCreate)
	adminPages.POST("/ai/mcp/token/revoke", builtin.CasbinMiddlewareForPath("/api/ai/token/revoke"), mcpPage.TokenRevoke)
	// 开关键：改的是全站可达性，复用「管理令牌」那个权限点 —— 能给外部发令牌的人
	// 本来就是决定「外部能不能进来」的人，多一个权限点只会让两处授权状态有机会不一致。
	adminPages.POST("/ai/mcp/toggle", builtin.CasbinMiddlewareForPath("/api/ai/token/create"), mcpPage.McpToggle)
	// 页面路径与权限点路径不同，必须显式指定 casbin obj（口径见 sysconfig 页面路由）。
	adminPages.GET("/ai/providers", shell.PageAuthz("/api/ai/provider/list"), page.ProvidersPage)

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
	adminPages.GET("/ai/sessions", shell.PageAuthz("/api/ai/session/list"), sessionPage.SessionsPage)
	adminPages.POST("/ai/sessions/append", builtin.CasbinMiddlewareForPath("/api/ai/session/append"), sessionPage.SessionAppend)
	adminPages.POST("/ai/sessions/rename", builtin.CasbinMiddlewareForPath("/api/ai/session/rename"), sessionPage.SessionRename)
	adminPages.POST("/ai/sessions/archive", builtin.CasbinMiddlewareForPath("/api/ai/session/archive"), sessionPage.SessionArchive)
	adminPages.POST("/ai/sessions/fold", builtin.CasbinMiddlewareForPath("/api/ai/session/fold"), sessionPage.SessionFold)
	// 发消息借对话入口的 casbin obj（发消息本质是一次对话），不新增权限点。
	adminPages.POST("/ai/sessions/send", builtin.CasbinMiddlewareForPath("/api/ai/chat"), sessionPage.SessionSend)
	// 全局悬浮球（每个后台页面都有入口）：同样借对话入口的 casbin obj。
	// 返回的是**片段**而不是重定向 —— 回答要出现在球旁边，不是把用户弹到另一个页面。
	adminPages.POST("/ai/ask", builtin.CasbinMiddlewareForPath("/api/ai/chat"), sessionPage.FabAsk)
	// 流式版本：同一份权限点、同一个会话键。两条路径**并存**而不是替换 ——
	// 浏览器不支持流式读取（或 JS 被拦）时非流式那条仍能用。
	adminPages.POST("/ai/ask/stream", builtin.CasbinMiddlewareForPath("/api/ai/chat"), sessionPage.FabAskStream)
	// 历史回填：读的是会话事件，借会话事件查询的 casbin obj（不新增权限点）——
	// 能看到这条会话历史的，与能看会话日志的是同一批人。
	adminPages.GET("/ai/fab/history", builtin.CasbinMiddlewareForPathAs("/api/ai/session/events", http.MethodGet), sessionPage.FabHistory)
}
