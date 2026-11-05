package view

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/facing"
	"go_wp/pkg/response"
)

// 页面失败只有一条出口：统一 JSON 失败，且状态码取自错误声明。
// 这条围栏盯的是「FailScene 别退回 ErrorAuto + 手算状态」——
// 两条路一旦分叉，页面失败的 400/404 与接口失败就会不一致。
func TestFailSceneGoesThroughSingleJSONExit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name string
		err  error
		code int
	}{
		{"声明了 404", facing.WithStatus(facing.New("product.err.notFound"), http.StatusNotFound), http.StatusNotFound},
		{"声明了 409", facing.WithStatus(facing.New("product.err.versionConflict"), http.StatusConflict), http.StatusConflict},
		{"未声明状态", facing.New("product.err.invalidParam"), http.StatusBadRequest},
		{"非业务错误", errors.New("dial tcp 127.0.0.1:5432: connect: connection refused"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			engine := gin.New()
			engine.GET("/", UseRenderer(func(c *gin.Context, tpl string, data any) {
				t.Fatal("失败路径不得进入模板渲染器（没有失败页）")
			}), func(c *gin.Context) { FailScene(c, "product", tc.err) })

			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

			if rec.Code != tc.code {
				t.Fatalf("状态码 %d，want %d", rec.Code, tc.code)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
				t.Fatalf("页面失败必须是 JSON，实际 Content-Type=%q", ct)
			}
			body := rec.Body.String()
			if strings.Contains(body, tc.err.Error()) {
				t.Fatalf("响应体泄漏了错误原文：%s", body)
			}
		})
	}
}

// 字节流已写 body 后返回 ErrHandled：出口不得再写一次 JSON 覆盖它。
func TestFailSceneIgnoresErrHandled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, "字节流正文")
		FailScene(c, "presentation", response.ErrHandled)
	})

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK || rec.Body.String() != "字节流正文" {
		t.Fatalf("ErrHandled 之后不该再写响应，实际 %d %q", rec.Code, rec.Body.String())
	}
}
