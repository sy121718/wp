package feature

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	projecthttp "go_wp/internal/module/project/inbound/http"
	"go_wp/internal/templates"
	"go_wp/pkg/casbin"
	mw "go_wp/pkg/middleware"
	"go_wp/public/test/support"
)

// 三块工作台面板的守卫必须来自**生产注册函数**。
//
// 模块内的用例手工写了「守卫 + 渲染接线 + handler」三条路由 —— 那只能证明
// 手工写的那一份有守卫；把 project_page_router.go 里的 Casbin 中间件删掉时，
// 它照样绿。这里直接调用 projecthttp.SetupProjectPages 注册（与生产同一段代码），
// 再从真实 HTTP 请求观察：
//
//  1. 鉴权器不可用 → 非 200（守卫拒绝，请求到不了 handler）；
//  2. Casbin 有策略 → 200 且响应是服务端渲染的面板片段。
//
// 服务依赖传 nil：守卫在 handler 之前执行，请求不会走到业务层。
func TestWorkbenchPanelGuardComesFromProductionRouter(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	_ = casbin.Close()
	t.Cleanup(func() { _ = casbin.Close() })

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	// 与生产组同形：会话/CSRF/权限上下文在组上，守卫逐条挂在操作上。
	// 这里用测试身份注入替代会话中间件，只为让请求走到守卫。
	workbench := engine.Group("", func(c *gin.Context) {
		id, _ := strconv.ParseInt(c.GetHeader("X-Test-User"), 10, 64)
		c.Set("user_id", id)
		c.Next()
	})
	projecthttp.SetupProjectPages(mw.NewRegistry(), nil, workbench, nil, nil, nil, nil)

	post := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"document":{}}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Test-User", "810")
		engine.ServeHTTP(rec, req)
		return rec
	}

	panels := []string{"/workbench/settings", "/workbench/seo-score-panel", "/workbench/global"}

	// 情形一：Casbin 未初始化 —— 守卫必须拒绝，不能落到 handler 返回 200。
	for _, path := range panels {
		rec := post(path)
		if rec.Code == http.StatusOK {
			t.Fatalf("%s 在鉴权器不可用时返回 200 —— 生产注册处没有实际守卫\n%s", path, rec.Body.String())
		}
	}

	// 情形二：Casbin 可用且有策略 —— 三条都进入 handler 并渲染片段。
	if err := casbin.InitCasbin(db); err != nil {
		t.Fatalf("初始化 Casbin 失败：%v", err)
	}
	if _, err := casbin.GetEnforcer().AddPolicy("810", "/api/project/update", "POST", "project:update"); err != nil {
		t.Fatalf("写入测试策略失败：%v", err)
	}
	for _, path := range panels {
		rec := post(path)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s 在策略允许时应进入 handler 并返回片段，实际 %d：%s", path, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "<") {
			t.Fatalf("%s 应返回 HTML 片段，实际正文：%s", path, rec.Body.String())
		}
	}
}
