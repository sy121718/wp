package templates_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CloudyKit/jet/v6"
	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/internal/templates"
	"go_wp/pkg/i18n"
	"go_wp/pkg/response"
	"go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
)

// 本文件的断言口径：
//   - 状态必须是 500（模板渲染不是业务声明，没有 key、没有状态码）；
//   - message 必须是**精确的归口文案**，不是「任意非空」，更不是裸 key；
//   - data 形状：模板失败不给 data（JSON 里要么没有该字段，要么是 null）；
//   - 内部信息（模板名、Jet 报错原文）一律不上屏。

// badRenderCases 是三种典型的模板失败：加载期语法错、执行期变量缺失、执行期函数缺失。
var badRenderCases = []struct {
	name   string
	source string
}{
	{"语法错误", `{{if}}`},
	{"变量不存在", `{{.Missing.Field.Deep}}`},
	{"函数不存在", `{{noSuchFunc 1}}`},
}

// newBadEngine 复刻生产接线：渲染器 + 失败出口 + 取词上下文，一笔不少。
func newBadEngine(t *testing.T, source string, viaAdminUI bool) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	loader := jet.NewInMemLoader()
	loader.Set("bad.html", source)
	set := jet.NewSet(loader, jet.WithTemplateNameExtensions([]string{"", ".html"}))
	templates.InjectGlobalsForTest(set)

	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRenderWithSet(set)
	engine.Use(templates.FailedRenderMiddleware(func(c *gin.Context, err error) {
		response.ErrorStatusAuto(c, "template", err)
	}))
	engine.Use(func(c *gin.Context) {
		text.AttachScope(c)
		c.Next()
	})

	if viaAdminUI {
		// adminui 路径：真实 PageSlot → view.Slot → RenderPage → c.Render → jetInstance.Render。
		engine.GET("/admin/page", view.Slot("bad.html", adminui.RenderPage), func(c *gin.Context) {
			view.Render(c, gin.H{"Title": "页面"})
		})
		return engine
	}
	// 直接 c.HTML 路径。
	engine.GET("/page", func(c *gin.Context) {
		c.HTML(http.StatusOK, "bad.html", map[string]any{"Title": "页面"})
	})
	return engine
}

func assertJSONFailure(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("状态码 %d，want 500", rec.Code)
	}
	if ctype := rec.Header().Get("Content-Type"); !strings.HasPrefix(ctype, "application/json") {
		t.Fatalf("Content-Type %q，want application/json（页面失败走 JSON）", ctype)
	}

	body := rec.Body.String()

	// message 精确等于归口文案：既不是裸 key，也不是任意非空串。
	wantMessage := response.InternalFallback
	if !strings.Contains(body, `"message":"`+wantMessage+`"`) {
		t.Fatalf("message 不是归口文案 %q：%s", wantMessage, body)
	}
	if strings.Contains(body, response.InternalKey) {
		t.Fatalf("裸 InternalKey 泄漏到前端：%s", body)
	}

	// data 形状：模板失败不带业务数据。omitempty 会让字段整体消失，
	// 因此「没有 data 字段」与 "data":null 都算合格，出现对象/数组则不合格。
	for _, bad := range []string{`"data":{`, `"data":[`} {
		if strings.Contains(body, bad) {
			t.Fatalf("模板失败不得带业务 data：%s", body)
		}
	}

	// 内部信息不上屏。
	for _, leak := range []string{"bad.html", "template", "template render failed", "Jet", "Missing", "noSuchFunc"} {
		if strings.Contains(body, leak) {
			t.Fatalf("响应体泄漏内部信息 %q：%s", leak, body)
		}
	}
	if strings.Contains(body, "<html") || strings.Contains(body, "<pre>") || strings.Contains(body, "页面暂时无法显示") {
		t.Fatalf("失败不得回 HTML 页：%s", body)
	}
}

// 直接 c.HTML 路径：三种坏模板都必须是 JSON + 500 + 归口文案。
func TestDirectHTMLRenderFailureBecomesJSON(t *testing.T) {
	for _, tc := range badRenderCases {
		t.Run(tc.name, func(t *testing.T) {
			engine := newBadEngine(t, tc.source, false)
			rec := getWithLang(engine, "/page", "zh-CN")
			assertJSONFailure(t, rec)
		})
	}
}

// adminui / pkg/view 路径：同样的失败结论。两条合法出口必须一致。
func TestAdminUIRenderFailureBecomesJSON(t *testing.T) {
	for _, tc := range badRenderCases {
		t.Run(tc.name, func(t *testing.T) {
			engine := newBadEngine(t, tc.source, true)
			rec := getWithLang(engine, "/admin/page", "zh-CN")
			assertJSONFailure(t, rec)
		})
	}
}

// 词库里有 MsgInternalError 时，出口取词库那条；词库没有时退回常量兜底。
// 两种情况都必须是「一句可显示的话」，且都不是裸 key。
func TestRenderFailureMessageFollowsWordLibrary(t *testing.T) {
	i18n.InjectForTest(map[string]map[string]string{
		response.InternalKey: {"zh-CN": "页面开小差了，请稍候"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	engine := newBadEngine(t, `{{if}}`, false)
	rec := getWithLang(engine, "/page", "zh-CN")

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("状态码 %d，want 500", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "页面开小差了，请稍候") {
		t.Fatalf("有词条时应取词条：%s", rec.Body.String())
	}
}

// 已写过响应时不接管：这里用的是**真实的** *TemplateRenderError，
// 而不是别的错误类型 —— 否则测不到「类型对、但已写」这条边界。
func TestWrittenResponseWithTemplateErrorIsNotRewritten(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(templates.FailedRenderMiddleware(func(c *gin.Context, err error) {
		t.Fatal("已写过响应时不得再写")
	}))
	engine.GET("/y", func(c *gin.Context) {
		_, _ = c.Writer.WriteString("already-written")
		_ = c.Error(&templates.TemplateRenderError{Template: "x.html", Err: errBoom{}})
	})

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/y", nil))

	if got := rec.Body.String(); got != "already-written" {
		t.Fatalf("响应体被改写：%q", got)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("已提交的状态被改成 %d", rec.Code)
	}
}

// 模板错误之后又 push 了普通 err：仍必须接管。
//
// 只看 c.Errors.Last() 的实现会在这里漏掉 —— 那是一个 500 的空白响应。
func TestTemplateErrorFollowedByPlainErrorStillClaimed(t *testing.T) {
	gin.SetMode(gin.TestMode)

	loader := jet.NewInMemLoader()
	loader.Set("bad.html", `{{if}}`)
	set := jet.NewSet(loader, jet.WithTemplateNameExtensions([]string{"", ".html"}))
	templates.InjectGlobalsForTest(set)

	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRenderWithSet(set)
	engine.Use(templates.FailedRenderMiddleware(func(c *gin.Context, err error) {
		response.ErrorStatusAuto(c, "template", err)
	}))
	engine.Use(func(c *gin.Context) {
		text.AttachScope(c)
		c.Next()
	})
	engine.GET("/page", func(c *gin.Context) {
		c.HTML(http.StatusOK, "bad.html", map[string]any{})
		// 渲染失败之后，链上又追加了一条普通错误（真实代码里常见：清理/回滚失败）。
		_ = c.Error(errBoom{})
	})

	rec := getWithLang(engine, "/page", "zh-CN")
	assertJSONFailure(t, rec)
}

// 非模板错误不被接管：出口由下游自己写，状态与正文都不得被改。
func TestPlainErrorIsNotClaimed(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(templates.FailedRenderMiddleware(func(c *gin.Context, err error) {
		t.Fatal("非模板错误不该被接管")
	}))
	engine.GET("/x", func(c *gin.Context) {
		_ = c.Error(errBoom{})
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误"})
	})

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("状态码被改成 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "参数错误") {
		t.Fatalf("正文被改写：%s", rec.Body.String())
	}
}

type errBoom struct{}

func (errBoom) Error() string { return "boom" }
