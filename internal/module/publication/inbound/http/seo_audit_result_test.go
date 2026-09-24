package pubhttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	pubservice "go_wp/internal/module/publication/service"
	"go_wp/internal/templates"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

func TestSEOAuditHandlerResponseModes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const raw = `<img src=x onerror=alert(1)> & "quote" 'tick' ` + "`backtick`"
	cases := []struct {
		name string
		hx   string
		fail bool
	}{
		{name: "htmx issues", hx: "true"},
		{name: "htmx clean", hx: "true"},
		{name: "htmx error", hx: "true", fail: true},
		{name: "json issues"},
		{name: "json clean"},
		{name: "json error", fail: true},
		{name: "non-htmx truthy header", hx: "false"},
		{name: "json with html accept"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var projectID string
			run := func(_ context.Context, id string) ([]pubservice.AuditIssue, int, error) {
				projectID = id
				if tc.fail {
					return nil, 0, errors.New("private-path:" + raw)
				}
				if tc.name == "htmx clean" || tc.name == "json clean" {
					return nil, 2, nil
				}
				return []pubservice.AuditIssue{{Level: raw, Path: raw, Message: raw}}, 2, nil
			}
			r := gin.New()
			r.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
			r.POST("/api/publication/seo-audit", seoAuditHandler(run))
			form := url.Values{"project": {" p1 "}}
			req := httptest.NewRequest(http.MethodPost, "/api/publication/seo-audit", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if tc.hx != "" {
				req.Header.Set("HX-Request", tc.hx)
			}
			if tc.name == "json with html accept" {
				req.Header.Set("Accept", "text/html")
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if projectID != "p1" {
				t.Fatalf("project = %q", projectID)
			}
			if tc.hx == "true" {
				if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
					t.Fatalf("htmx response: %d %s: %s", w.Code, w.Header().Get("Content-Type"), w.Body.String())
				}
				body := w.Body.String()
				if strings.Contains(body, raw) || strings.Contains(body, "private-path:") || strings.Contains(body, "<html") {
					t.Fatalf("HTML 片段泄漏原文或整页：%s", body)
				}
				switch tc.name {
				case "htmx issues":
					for _, want := range []string{"<table", "&lt;img", "&#39;", "&amp;", "已扫描 2", "发现 1"} {
						if !strings.Contains(body, want) {
							t.Errorf("片段缺 %q：%s", want, body)
						}
					}
				case "htmx clean":
					if !strings.Contains(body, "badge-success") || strings.Contains(body, "<table") {
						t.Errorf("无问题片段异常：%s", body)
					}
				case "htmx error":
					if !strings.Contains(body, `role="alert"`) || !strings.Contains(body, "badge-warning") {
						t.Errorf("失败片段未给出可见提示：%s", body)
					}
				}
				return
			}
			if !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
				t.Fatalf("JSON 响应类型改变：%s", w.Header().Get("Content-Type"))
			}
			var got response.Response
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if tc.fail {
				if w.Code != http.StatusBadRequest || got.Code != http.StatusBadRequest || got.Message == "" || got.Data != nil || strings.Contains(got.Message, raw) {
					t.Fatalf("JSON 错误契约改变：%+v", got)
				}
				return
			}
			if w.Code != http.StatusOK || got.Code != http.StatusOK {
				t.Fatalf("JSON 状态改变：%+v", got)
			}
			data, ok := got.Data.(map[string]any)
			if !ok || data["scanned"] != float64(2) {
				t.Fatalf("JSON 数据形状改变：%+v", got.Data)
			}
			if tc.name == "json clean" {
				if data["count"] != float64(0) || data["issues"] != nil {
					t.Fatalf("无结论 JSON 应保持 issues:null：%+v", data)
				}
				return
			}
			issues, ok := data["issues"].([]any)
			if !ok || data["count"] != float64(1) || len(issues) != 1 || issues[0].(map[string]any)["Message"] != raw {
				t.Fatalf("JSON issue 字段改变：%+v", data["issues"])
			}
		})
	}
}
