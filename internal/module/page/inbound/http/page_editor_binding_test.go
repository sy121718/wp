package pagehttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
)

type editorBindingPages struct {
	pagecontract.PageService
	called string
	saved  *pagedto.SaveDraftReq
	built  *pagedto.BuildReq
	pub    *pagedto.PublishReq
}

func (s *editorBindingPages) SaveDraft(_ context.Context, req *pagedto.SaveDraftReq) (*pagedto.PageResp, error) {
	s.called, s.saved = "draft", req
	return &pagedto.PageResp{}, nil
}

func (s *editorBindingPages) Build(_ context.Context, req *pagedto.BuildReq) (*pagedto.PublishResp, error) {
	s.called, s.built = "build", req
	return &pagedto.PublishResp{}, nil
}

func (s *editorBindingPages) Publish(_ context.Context, req *pagedto.PublishReq) (*pagedto.PublishResp, error) {
	s.called, s.pub = "publish", req
	return &pagedto.PublishResp{}, nil
}

func (s *editorBindingPages) PublishAllLanguages(_ context.Context, req *pagedto.PublishReq) (*pagedto.PublishAllResp, error) {
	s.called, s.pub = "all", req
	return &pagedto.PublishAllResp{}, nil
}

// TestPageEditorJSONBinding 只核原生绑定边界，不替代真实保存、构建和发布链路。
func TestPageEditorJSONBinding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, path, body, called string
	}{
		{"draft", "/draft", `{"id":"p1","expectedVersion":0,"draftPath":"/","draftDocument":{"root":[],"settings":{"enabled":false}}}`, "draft"},
		{"build", "/build", `{"id":"p1","expectedVersion":7,"lang":"zh-CN"}`, "build"},
		{"publish", "/publish", `{"id":"p1","lang":"en-US","allLangs":false}`, "publish"},
		{"all", "/publish", `{"id":"p1","lang":"zh-CN","allLangs":true}`, "all"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &editorBindingPages{}
			h := NewHandle(svc)
			router := gin.New()
			router.POST("/draft", h.SaveDraft)
			router.POST("/build", h.Build)
			router.POST("/publish", h.Publish)
			req := httptest.NewRequest(http.MethodPost, tc.path+"?id=query&expectedVersion=99&lang=query&allLangs=true", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK || svc.called != tc.called {
				t.Fatalf("status=%d called=%q body=%s", rec.Code, svc.called, rec.Body.String())
			}
			switch tc.called {
			case "draft":
				if svc.saved.ID != "p1" || svc.saved.ExpectedVersion != 0 || svc.saved.DraftPath != "/" || string(svc.saved.DraftDocument) != `{"root":[],"settings":{"enabled":false}}` {
					t.Fatalf("草稿绑定失真：%+v", svc.saved)
				}
			case "build":
				if svc.built.ID != "p1" || svc.built.ExpectedVersion != 7 || svc.built.Lang != "zh-CN" {
					t.Fatalf("构建绑定失真：%+v", svc.built)
				}
			default:
				wantLang := "en-US"
				if tc.called == "all" {
					wantLang = "zh-CN"
				}
				if svc.pub.ID != "p1" || svc.pub.Lang != wantLang || svc.pub.AllLangs != (tc.called == "all") {
					t.Fatalf("发布绑定失真：%+v", svc.pub)
				}
			}
		})
	}
	for _, path := range []string{"/draft", "/build", "/publish"} {
		for _, tc := range []struct{ name, contentType, body string }{
			{"form", "application/x-www-form-urlencoded", "id=p1&draftPath=%2F&draftDocument=%7B%7D"},
			{"queryCannotFillBody", "application/json", `{}`},
			{"malformed", "application/json", `{"id":`},
		} {
			t.Run(path+"/"+tc.name, func(t *testing.T) {
				svc := &editorBindingPages{}
				h := NewHandle(svc)
				router := gin.New()
				router.POST("/draft", h.SaveDraft)
				router.POST("/build", h.Build)
				router.POST("/publish", h.Publish)
				req := httptest.NewRequest(http.MethodPost, path+"?id=p1&draftPath=%2F&draftDocument=%7B%7D", strings.NewReader(tc.body))
				req.Header.Set("Content-Type", tc.contentType)
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)
				if rec.Code != http.StatusBadRequest || svc.called != "" || !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
					t.Fatalf("非法输入必须在绑定层拒绝：status=%d called=%q body=%s", rec.Code, svc.called, rec.Body.String())
				}
			})
		}
	}
}
