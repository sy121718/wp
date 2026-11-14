package projecthttp

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	projectenums "go_wp/internal/module/project/enums"
	"go_wp/internal/templates"
	pageview "go_wp/pkg/view"
)

// themeSettingsFailPage 是保存失败的唯一出口，两个发起方各有一条受控响应。
//
// 以前这里无条件重渲整页、也不给状态码：
//   - 后台表单拿到 200 的「失败页」，语义上等于保存成功；
//   - 工作台 HTMX 拿到整页 HTML，被写基座判为非法响应；
//   - 而且模板读 .ThemeSettings，结构体字段却叫 ThemeSettingsJSON 且没有
//     view tag —— Jet 在缺键处中断，整页重渲直接退化成 500。
//
// 这条用例同时钉住三件事：视图键名、状态码、两条出口的形态。
func TestThemeSettingsFailPageIsControlled4xx(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
	engine.Use(pageview.FailureMiddleware(pageview.RenderFailure))
	// 与生产注册同形：同一条路由既服务后台普通表单，也服务工作台 HTMX。
	engine.POST("/admin/themes/settings/save",
		pageview.HTMLFragment, pageview.Slot(ThemeSettingsPageTemplate, pageview.RenderPage),
		func(c *gin.Context) {
			// 与 SaveThemeSettings 的失败分支同形：用表单回显数据构造视图。
			data := &themeSettingsData{
				Title: "MsgThemeSettingsTitle", Menu: "themes",
				ThemeID: "t-1", ThemeName: "默认主题",
				Groups: buildThemeGroups(map[string]any{
					"colors": map[string]any{"primary": "not-a-color"},
				}),
				// 绑定事实（面板不渲染的那几个）也由真实回显路径填回来。
				HeaderBlockID: "blk-header",
				HeaderBlocks:  []blockOption{{ID: "blk-header", NameFallback: "页眉块"}},
			}
			themeSettingsFailPage(c, data, http.StatusBadRequest,
				projectMsgOf(projectenums.MsgThemeSettingsInvalid, "主题设置不合法"),
				errSample("colors.primary", "not-a-color"), nil)
		})

	t.Run("普通表单：受控 4xx + 就地重渲 + 保留输入", func(t *testing.T) {
		form := url.Values{
			"id":             {"t-1"},
			"headerBlockId":  {"blk-header"},
			"colors.primary": {"not-a-color"},
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/themes/settings/save", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		engine.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("校验失败必须是受控 4xx，实际 %d（500 = 模板缺键退化，200 = 把失败报成成功）\n%s",
				rec.Code, firstN(rec.Body.String(), 400))
		}
		body := rec.Body.String()
		for _, want := range []string{
			`id="theme-settings-json"`, // 模板读 .ThemeSettings：缺 view tag 时整页中断
			`value="blk-header"`,       // 面板不渲染的绑定事实原样回显
			`aria-invalid="true"`,      // 字段级错误定位
			`role="alert"`,             // 失败提示
		} {
			if !strings.Contains(body, want) {
				t.Errorf("失败页缺少 %q —— 用户拿不回输入或看不到原因\n%s", want, firstN(body, 600))
			}
		}
	})

	t.Run("HTMX：受控 4xx + 共享失败片段，不重渲整页", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/themes/settings/save",
			strings.NewReader(url.Values{"id": {"t-1"}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")
		engine.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("HTMX 失败必须是受控 4xx，实际 %d", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, `data-ui-error`) || !strings.Contains(body, `role="alert"`) {
			t.Fatalf("HTMX 失败必须是共享错误片段，实际正文：%s", firstN(body, 400))
		}
		if strings.Contains(body, "<!DOCTYPE") || strings.Contains(body, "theme-settings-form") {
			t.Fatalf("HTMX 请求不能拿到整页 HTML（写基座会判为非法响应）")
		}
	})
}

// errSample 构造一条带引号值的校验错误，与 builder 的真实报错形状一致
// （themeSettingsInvalidField 从 `: "值"` 里取回显值）。
func errSample(path, value string) error {
	return &quotedError{msg: "主题设置字段不合法: " + path + `: "` + value + `"`}
}

type quotedError struct{ msg string }

func (e *quotedError) Error() string { return e.msg }

func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
