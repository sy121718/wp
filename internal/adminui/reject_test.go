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
	json "go_wp/pkg/jsonx"
)

type rejectRenderer struct{ t *testing.T }

func (r rejectRenderer) Instance(string, any) render.Render {
	r.t.Fatal("鉴权拒绝不得调用 HTML 渲染器")
	return nil
}

func TestRejectAlwaysReturnsDeclaredJSONFailure(t *testing.T) {
	for _, tc := range []struct {
		name, key, message string
		status             int
	}{
		{"未登录", enums.ErrUserInfoMissing, "未获取到用户信息", 401},
		{"无权限", enums.ErrPermissionDenied, "无权限访问", 403},
		{"权限能力缺失", enums.ErrAuthzNotReady, "权限系统未初始化", 500},
		{"鉴权故障", enums.ErrAuthzFailed, "权限验证失败", 500},
		{"未知内部错误", "private SQLSTATE access failure", "内部错误", 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, hx := range []string{"", "true", "TRUE", " true ", "false", "1"} {
				engine := gin.New()
				engine.HTMLRender = rejectRenderer{t}
				engine.GET("/reject", func(c *gin.Context) { adminui.Reject()(c, tc.status, tc.key) })
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
				if rec.Code != tc.status || body.Code != rec.Code || !strings.Contains(body.Message, tc.message) {
					t.Fatalf("HX=%q 状态或文案错误：%d %+v", hx, rec.Code, body)
				}
				if rec.Header().Get("Cache-Control") != "no-store" || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
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

func TestRejectStatusComesFromDeclaredFact(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/reject", nil)
	adminui.Reject()(c, http.StatusOK, enums.ErrPermissionDenied)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("错误事实必须保持403，不能被调用方改为200：%d", rec.Code)
	}
}
