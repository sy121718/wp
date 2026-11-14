package contenttemplatehttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
)

type editorBindingTemplates struct {
	contenttemplatecontract.ContentTemplateService
	created *contenttemplatedto.CreateReq
	updated *contenttemplatedto.UpdateReq
}

func (s *editorBindingTemplates) Create(_ context.Context, req *contenttemplatedto.CreateReq) (*contenttemplatedto.TemplateResp, error) {
	s.created = req
	return &contenttemplatedto.TemplateResp{}, nil
}

func (s *editorBindingTemplates) Update(_ context.Context, req *contenttemplatedto.UpdateReq) (*contenttemplatedto.TemplateResp, error) {
	s.updated = req
	return &contenttemplatedto.TemplateResp{}, nil
}

// TestContentTemplateEditorJSONBinding 保留复杂文档原文，并拒绝旧表单和 query 补参。
func TestContentTemplateEditorJSONBinding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, path := range []string{"/create", "/update"} {
		for _, tc := range []struct {
			name, contentType, body string
			status                  int
		}{
			{"json", "application/json", `{"id":"t1","entityType":"product","name":"模板","projectId":"p1","templateRole":"detail","draftDocument":{"root":[],"settings":{"enabled":false}}}`, http.StatusOK},
			{"form", "application/x-www-form-urlencoded", "id=t1&entityType=product&name=template&draftDocument=%7B%7D", http.StatusBadRequest},
			{"queryCannotFillBody", "application/json", `{}`, http.StatusBadRequest},
			{"documentRequired", "application/json", `{"id":"t1","entityType":"product","name":"模板"}`, http.StatusBadRequest},
			{"malformed", "application/json", `{"id":`, http.StatusBadRequest},
		} {
			t.Run(path+"/"+tc.name, func(t *testing.T) {
				svc := &editorBindingTemplates{}
				h := NewHandle(svc)
				router := gin.New()
				router.POST("/create", h.Create)
				router.POST("/update", h.Update)
				req := httptest.NewRequest(http.MethodPost, path+"?id=query&entityType=query&name=query&draftDocument=%7B%7D", strings.NewReader(tc.body))
				req.Header.Set("Content-Type", tc.contentType)
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)
				if rec.Code != tc.status {
					t.Fatalf("status=%d want=%d body=%s", rec.Code, tc.status, rec.Body.String())
				}
				if tc.status != http.StatusOK {
					if svc.created != nil || svc.updated != nil {
						t.Fatal("非法输入不应进入 service")
					}
					return
				}
				const document = `{"root":[],"settings":{"enabled":false}}`
				if path == "/create" {
					if svc.created == nil || svc.created.EntityType != "product" || svc.created.Name != "模板" || svc.created.ProjectID != "p1" || svc.created.TemplateRole != "detail" || string(svc.created.DraftDocument) != document {
						t.Fatalf("创建绑定失真：%+v", svc.created)
					}
				} else if svc.updated == nil || svc.updated.ID != "t1" || string(svc.updated.DraftDocument) != document {
					t.Fatalf("更新绑定失真：%+v", svc.updated)
				}
			})
		}
	}
}
