package feature

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"go_wp/internal/adminui"
	aidto "go_wp/internal/module/ai/dto"
	aihttp "go_wp/internal/module/ai/inbound/http"
	aimodel "go_wp/internal/module/ai/model"
	aiservice "go_wp/internal/module/ai/service"
	"go_wp/internal/templates"
	pageview "go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
	"go_wp/public/test/support"
)

// 令牌的生成与撤销是普通后台表单：按新写契约回 Jet HTML 片段 + 真实状态码。
// 这两条曾经回 JSON 信封，由前端按 data.item 拼行；现在行与一次性明文都由服务端渲染，
// 这里从 HTTP 层锁住新协议：状态码、Content-Type、片段内容、无整页壳。

const tokenPanelTestUserID int64 = 7

// 这两个写动作只有 /api 一条入口：生成与撤销都回 JSON 事实，页面侧的 POST 双轨
// 已经清退（见 ai_page_router.go）。测试因此直接打真实 API 路径与真实 handler。
func newTokenPanelEnv(t *testing.T) (*aihttp.TokenHandle, *aiservice.AccessTokenService, *gorm.DB) {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	applyProductionSchema(t, db)

	tokens := aiservice.NewAccessTokenService(aimodel.NewAccessTokenModel(db))
	tokens.SetScopeValidator(func(_ context.Context, p string) bool { return p != "" })
	// handler 从 gin.Context 上取当前用户；测试直接把它固定成同一个账号。
	th := aihttp.NewTokenHandle(tokens)
	return th, tokens, db
}

func serveTokenPanel(t *testing.T, th *aihttp.TokenHandle, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	// 生产装配在基础层挂失败出口与请求取词；缺了它们片段渲染为空。
	engine.Use(pageview.FailureMiddleware(adminui.RenderFailure))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	engine.Use(func(c *gin.Context) {
		c.Set("user_id", tokenPanelTestUserID)
		c.Next()
	})
	engine.POST("/api/ai/token/create", pageview.HTMLFragment, th.Create)
	engine.POST("/api/ai/token/revoke", pageview.HTMLFragment, th.Revoke)

	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func TestMcpTokenCreateReturnsHTMLFragment(t *testing.T) {
	ph, _, _ := newTokenPanelEnv(t)

	rec := serveTokenPanel(t, ph, "/api/ai/token/create", "name=外部集成&scopes=order:list")
	if rec.Code != http.StatusOK {
		t.Fatalf("生成成功应为 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if ctype := rec.Header().Get("Content-Type"); !strings.HasPrefix(ctype, "text/html") {
		t.Fatalf("生成应回 HTML 片段，实际 code=%d Content-Type=%q body=%q", rec.Code, ctype, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "<html") {
		t.Fatalf("生成响应不该带整页壳：%s", body)
	}
	if !strings.Contains(body, `id="mcp-token-plain"`) {
		t.Fatalf("生成响应缺少一次性明文区：%s", body)
	}
	if !strings.Contains(body, "外部集成") {
		t.Fatalf("生成响应缺少新令牌行：%s", body)
	}
	if !strings.Contains(body, `hx-swap-oob`) {
		t.Errorf("新行应以 OOB 方式插回列表：%s", body)
	}

	// 库里只存哈希：整个响应里明文只该出现一次。
	start := strings.Index(body, `id="mcp-token-plain">`)
	if start < 0 {
		t.Fatalf("找不到明文节点：%s", body)
	}
	rest := body[start+len(`id="mcp-token-plain">`):]
	end := strings.Index(rest, "</code>")
	if end < 0 {
		t.Fatalf("明文节点没有闭合：%s", body)
	}
	plain := strings.TrimSpace(rest[:end])
	if !strings.HasPrefix(plain, "wp_") {
		t.Fatalf("明文应以 wp_ 开头，实际 %q", plain)
	}
	if strings.Count(body, plain) != 1 {
		t.Errorf("明文只该出现一次（库里只存哈希、服务端不重放），实际出现 %d 次", strings.Count(body, plain))
	}
}

func TestMcpTokenRevokeReturnsPostRevokeFacts(t *testing.T) {
	ph, tokens, _ := newTokenPanelEnv(t)

	created, err := tokens.Create(t.Context(), &aidto.TokenCreateReq{
		Name: "要被撤销的", Scopes: []string{"order:list"}, UserID: tokenPanelTestUserID,
	})
	if err != nil {
		t.Fatalf("准备令牌失败：%v", err)
	}

	rec := serveTokenPanel(t, ph, "/api/ai/token/revoke", "id="+itoa(created.Item.ID))
	if rec.Code != http.StatusOK {
		t.Fatalf("撤销成功应为 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if ctype := rec.Header().Get("Content-Type"); !strings.HasPrefix(ctype, "text/html") {
		t.Fatalf("撤销应回 HTML 片段，实际 Content-Type=%q", ctype)
	}
	body := rec.Body.String()
	// 行还在（审计要能回答「什么时候撤的」），所以回的是同一行、状态换成已撤销。
	if !strings.Contains(body, `id="mcp-token-row-`+itoa(created.Item.ID)+`"`) {
		t.Fatalf("撤销应回同一行，实际：%s", body)
	}
	if !strings.Contains(body, "已撤销") {
		t.Errorf("撤销后的行应显示已撤销状态：%s", body)
	}
	if strings.Contains(body, `hx-post="/api/ai/token/revoke"`) {
		t.Errorf("已撤销的行不该再有撤销表单：%s", body)
	}
}

func TestMcpTokenRevokeUnknownIDIsNotFound(t *testing.T) {
	ph, _, _ := newTokenPanelEnv(t)

	rec := serveTokenPanel(t, ph, "/api/ai/token/revoke", "id=987654")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("不存在应为 404，实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-ui-error`) {
		t.Fatalf("失败应渲染 Jet 错误片段：%s", body)
	}
	if ctype := rec.Header().Get("Content-Type"); !strings.HasPrefix(ctype, "text/html") {
		t.Fatalf("失败也应回 HTML 片段，实际 Content-Type=%q", ctype)
	}
	// 声明 key 一律不上屏：片段里只能是译文或归口文案。
	if strings.Contains(body, "ai.err.") || strings.Contains(body, "ErrTokenNotFound") {
		t.Errorf("失败片段漏出了错误 key：%s", body)
	}
}

// TestMcpTokenRevokeFactsMatchRowTemplateKeys 锁「服务端事实」与「行模板取值」是同一组键。
//
// 页面按 data-field 从 data.item 里取值；键写错就是「行插进去了但每格空」，而页面
// 看着一切正常。这里把真实响应的顶层键与模板声明的 data-field / data-time / data-scopes
// 对齐，任何一侧改名都会红。
func TestMcpTokenRevokeFactsMatchRowTemplateKeys(t *testing.T) {
	ph, tokens, _ := newTokenPanelEnv(t)

	created, err := tokens.Create(t.Context(), &aidto.TokenCreateReq{
		Name: "键对齐", Scopes: []string{"order:list"}, UserID: tokenPanelTestUserID,
	})
	if err != nil {
		t.Fatalf("准备令牌失败：%v", err)
	}

	rec := serveTokenPanel(t, ph, "/api/ai/token/revoke", "id="+itoa(created.Item.ID))
	if rec.Code != http.StatusOK {
		t.Fatalf("撤销应成功，实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	// 行由服务端按 tokenRow 渲染：状态、权限点与时刻都在模板里取词，不再有前端取值指令。
	for _, want := range []string{
		`id="mcp-token-row-` + itoa(created.Item.ID) + `"`,
		"键对齐",
		"order:list",
		"已撤销",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("撤销片段缺少 %q：%s", want, body)
		}
	}
	for _, stale := range []string{`data-field=`, `data-when-field=`, `data-time=`, `data-scopes`} {
		if strings.Contains(body, stale) {
			t.Errorf("撤销片段不该再有前端取值指令 %q：%s", stale, body)
		}
	}
}

func TestMcpTokenRevokeRejectsMissingID(t *testing.T) {
	ph, _, _ := newTokenPanelEnv(t)

	rec := serveTokenPanel(t, ph, "/api/ai/token/revoke", "id=0")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("缺 id 应为 400，实际 %d：%s", rec.Code, rec.Body.String())
	}
}
