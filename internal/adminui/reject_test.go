package adminui_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/render"
	"go_wp/internal/adminui"
	"go_wp/pkg/enums"
	"go_wp/pkg/i18n"
	json "go_wp/pkg/jsonx"
)

type rejectRenderer struct{ t *testing.T }

func (r rejectRenderer) Instance(string, any) render.Render {
	r.t.Fatal("鉴权拒绝不得调用 HTML 渲染器")
	return nil
}

// rejectProbe 的返回值就是拒绝出口的完整观测面：状态码、JSON 体与响应头。
func rejectProbe(t *testing.T, status int, key, hx string) (*httptest.ResponseRecorder, struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}) {
	t.Helper()
	engine := gin.New()
	engine.HTMLRender = rejectRenderer{t}
	engine.GET("/reject", func(c *gin.Context) { adminui.Reject()(c, status, key) })

	req := httptest.NewRequest(http.MethodGet, "/reject", nil)
	req.Header.Set("Accept", "text/html")
	req.Header.Set("HX-Request", hx)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	var body struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    any    `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("拒绝不是JSON：%v %s", err, rec.Body.String())
	}
	return rec, body
}

// 拒绝出口的稳定契约与文案无关：401/403/500 取自错误声明，
// 恒为不可缓存 JSON、不输出导航头、不泄漏内部原文。
// 文案断言另见下面两条（有词 / 缺词），这里只锚定形态。
func TestRejectAlwaysReturnsDeclaredJSONFailure(t *testing.T) {
	for _, tc := range []struct {
		name, key string
		status    int
	}{
		{"未登录", enums.ErrUserInfoMissing, 401},
		{"无权限", enums.ErrPermissionDenied, 403},
		{"权限能力缺失", enums.ErrAuthzNotReady, 500},
		{"鉴权故障", enums.ErrAuthzFailed, 500},
		{"未知内部错误", "private SQLSTATE access failure", 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, hx := range []string{"", "true", "TRUE", " true ", "false", "1"} {
				rec, body := rejectProbe(t, tc.status, tc.key, hx)

				if rec.Code != tc.status || body.Code != rec.Code {
					t.Fatalf("HX=%q 状态错误：%d code=%d", hx, rec.Code, body.Code)
				}
				if strings.TrimSpace(body.Message) == "" {
					t.Fatalf("HX=%q message 为空：拒绝必须给一句可显示的话", hx)
				}
				// 归口文案与词库词条都必须不是 key 字面量。
				if body.Message == tc.key {
					t.Fatalf("HX=%q message 是裸 key %q", hx, tc.key)
				}
				if rec.Header().Get("Cache-Control") != "no-store" ||
					!strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
					t.Fatalf("拒绝应为不可缓存JSON：%v", rec.Header())
				}
				for _, header := range []string{"Location", "HX-Redirect", "HX-Location"} {
					if rec.Header().Get(header) != "" {
						t.Fatalf("拒绝不得输出导航头 %s", header)
					}
				}
				if strings.Contains(rec.Body.String(), "private") || strings.Contains(rec.Body.String(), "SQLSTATE") {
					t.Fatalf("内部信息泄漏：%s", rec.Body.String())
				}
			}
		})
	}
}

// 有词时取词库里的精确文案：这正是「key + 状态码声明、文案归词库」这条链的落点。
func TestRejectUsesWordLibraryText(t *testing.T) {
	i18n.InjectForTest(map[string]map[string]string{
		enums.ErrUserInfoMissing:  {"zh-CN": "未获取到用户信息"},
		enums.ErrPermissionDenied: {"zh-CN": "无权限访问"},
		enums.ErrAuthzNotReady:    {"zh-CN": "权限系统未初始化"},
		enums.ErrAuthzFailed:      {"zh-CN": "权限验证失败"},
	}, nil)
	defer i18n.InjectForTest(nil, nil)

	for _, tc := range []struct {
		name, key, want string
		status          int
	}{
		{"未登录", enums.ErrUserInfoMissing, "未获取到用户信息", 401},
		{"无权限", enums.ErrPermissionDenied, "无权限访问", 403},
		{"权限能力缺失", enums.ErrAuthzNotReady, "权限系统未初始化", 500},
		{"鉴权故障", enums.ErrAuthzFailed, "权限验证失败", 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec, body := rejectProbe(t, tc.status, tc.key, "")
			if rec.Code != tc.status {
				t.Fatalf("状态码 %d，want %d", rec.Code, tc.status)
			}
			if body.Message != tc.want {
				t.Fatalf("message = %q，want 词库里的 %q", body.Message, tc.want)
			}
		})
	}
}

// 缺词时退归口文案：既不是裸 key，也不是某条业务词。
// 这条与上面那条分开，是因为「测试没注入词库」不等于「出口可以放任意非空字符串」。
func TestRejectFallsBackToSharedTextWhenWordMissing(t *testing.T) {
	i18n.InjectForTest(map[string]map[string]string{}, nil)
	defer i18n.InjectForTest(nil, nil)

	const want = "系统内部错误，请稍后重试"
	for _, tc := range []struct {
		name, key string
		status    int
	}{
		{"未登录", enums.ErrUserInfoMissing, 401},
		{"无权限", enums.ErrPermissionDenied, 403},
		{"未知内部错误", "private SQLSTATE access failure", 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec, body := rejectProbe(t, tc.status, tc.key, "")
			if rec.Code != tc.status {
				t.Fatalf("状态码 %d，want %d（缺词不改变状态）", rec.Code, tc.status)
			}
			// 非业务错误本身就是 500；业务错误缺词时状态码仍按声明给。
			if body.Message != want {
				t.Fatalf("message = %q，want 归口文案 %q", body.Message, want)
			}
		})
	}
}

func TestRejectStatusComesFromDeclaredFact(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/reject", nil)
	adminui.Reject()(c, http.StatusOK, enums.ErrPermissionDenied)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("错误事实必须保持403，不能被调用方改为200：%d", rec.Code)
	}
}
