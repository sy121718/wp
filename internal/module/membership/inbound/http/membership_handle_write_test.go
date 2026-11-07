package membershiphttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	membershipdto "go_wp/internal/module/membership/dto"
	membershipenums "go_wp/internal/module/membership/enums"
	"go_wp/internal/templates"
	"go_wp/pkg/i18n"
	pageview "go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
)

type membershipWriteStub struct {
	stubMembershipService
	got any
}

func (s *membershipWriteStub) CreateTier(_ context.Context, req *membershipdto.CreateTierReq) (*membershipdto.TierResp, error) {
	s.got = req
	return &membershipdto.TierResp{}, nil
}

func (s *membershipWriteStub) UpdateTier(_ context.Context, req *membershipdto.UpdateTierReq) (*membershipdto.TierResp, error) {
	s.got = req
	return &membershipdto.TierResp{}, nil
}

func (s *membershipWriteStub) DeleteTier(_ context.Context, req *membershipdto.DeleteTierReq) error {
	s.got = req
	return nil
}

func (s *membershipWriteStub) SaveEntitlements(_ context.Context, req *membershipdto.SaveEntitlementsReq) (*membershipdto.SaveEntitlementsResp, error) {
	s.got = req
	return &membershipdto.SaveEntitlementsResp{}, nil
}

func (s *membershipWriteStub) AssignManual(_ context.Context, req *membershipdto.AssignManualReq) (*membershipdto.AssignmentResp, error) {
	s.got = req
	return &membershipdto.AssignmentResp{}, nil
}

func (s *membershipWriteStub) UnlockManual(_ context.Context, req *membershipdto.UnlockManualReq) error {
	s.got = req
	return nil
}

// membershipWriteEngine 复刻生产接线：失败中间件、请求取词与 HTML 渲染都必须在场。
func membershipWriteEngine(h *Handle) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	r.Use(pageview.FailureMiddleware(pageview.RenderFailure))
	r.Use(pageview.UseSuccess(pageview.NewSuccessWriter()))
	r.Use(func(c *gin.Context) {
		text.AttachScope(c)
		c.Next()
	})
	r.POST("/write", pageview.HTMLFragment, h.CreateTier)
	r.POST("/update", pageview.HTMLFragment, h.UpdateTier)
	r.POST("/delete", pageview.HTMLFragment, h.DeleteTier)
	r.POST("/entitlement", pageview.HTMLFragment, h.SaveEntitlements)
	r.POST("/assign", pageview.HTMLFragment, h.AssignSet)
	r.POST("/unlock", pageview.HTMLFragment, h.AssignUnlock)
	return r
}

func TestMembershipPageWritesUseFormAndHTMLEndpoints(t *testing.T) {
	i18n.InjectForTest(map[string]map[string]string{
		membershipenums.MsgTierCreated:      {"zh-CN": "已创建等级", "en-US": "EN-CREATED"},
		membershipenums.MsgTierUpdated:      {"zh-CN": "已更新等级", "en-US": "EN-UPDATED"},
		membershipenums.MsgTierDeleted:      {"zh-CN": "已删除等级", "en-US": "EN-DELETED"},
		membershipenums.MsgEntitlementSaved: {"zh-CN": "已保存权益", "en-US": "EN-SAVED"},
		membershipenums.MsgAssignSet:        {"zh-CN": "已指定等级", "en-US": "EN-ASSIGNED"},
		membershipenums.MsgAssignUnlocked:   {"zh-CN": "已解锁", "en-US": "EN-UNLOCKED"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	cases := []struct {
		name  string
		path  string
		form  url.Values
		check func(*testing.T, any)
	}{
		{
			name: "create", path: "/write",
			form: url.Values{
				"projectId": {"p-1"}, "name": {"Silver"}, "sortOrder": {"-1"},
				"thresholdYuan": {"1000.05"}, "remark": {"memo"}, "discountPercent": {"20"},
			},
			check: func(t *testing.T, got any) {
				r := got.(*membershipdto.CreateTierReq)
				if r.ThresholdAmount != 100005 || r.SortOrder != -1 || len(r.Entitlements) != 1 || r.Entitlements[0].ValueInt != 20 {
					t.Fatalf("create contract: %+v", r)
				}
			},
		},
		{
			name: "update", path: "/update",
			form: url.Values{
				"projectId": {"p-1"}, "tierId": {"2"}, "name": {"Silver"},
				"sortOrder": {"-1"}, "thresholdYuan": {"0"}, "remark": {""},
			},
			check: func(t *testing.T, got any) {
				r := got.(*membershipdto.UpdateTierReq)
				if r.IsDefault != nil || r.ThresholdAmount == nil || *r.ThresholdAmount != 0 || r.SortOrder == nil || *r.SortOrder != -1 {
					t.Fatalf("update must preserve explicit zero: %+v", r)
				}
			},
		},
		{
			name: "default", path: "/update",
			form: url.Values{"projectId": {"p-1"}, "tierId": {"2"}, "isDefault": {"true"}},
			check: func(t *testing.T, got any) {
				r := got.(*membershipdto.UpdateTierReq)
				if r.IsDefault == nil || !*r.IsDefault || r.Name != nil || r.ThresholdAmount != nil {
					t.Fatalf("default must only change default: %+v", r)
				}
			},
		},
		{
			name: "delete", path: "/delete",
			form: url.Values{"projectId": {"p-1"}, "tierId": {"2"}},
			check: func(t *testing.T, got any) {
				r := got.(*membershipdto.DeleteTierReq)
				if r.ProjectID != "p-1" || r.TierID != 2 {
					t.Fatalf("delete contract: %+v", r)
				}
			},
		},
		{
			name: "clear-entitlements", path: "/entitlement",
			form: url.Values{"projectId": {"p-1"}, "tierId": {"2"}},
			check: func(t *testing.T, got any) {
				r := got.(*membershipdto.SaveEntitlementsReq)
				if r.Items == nil || len(r.Items) != 0 || r.TierID != 2 {
					t.Fatalf("clear must pass an explicit empty final list: %+v", r)
				}
			},
		},
		{
			name: "assign", path: "/assign",
			form: url.Values{"projectId": {"p-1"}, "userId": {"1024"}, "tierId": {"2"}},
			check: func(t *testing.T, got any) {
				r := got.(*membershipdto.AssignManualReq)
				if r.UserID != 1024 || r.TierID != 2 || r.ProjectID != "p-1" {
					t.Fatalf("assign contract: %+v", r)
				}
			},
		},
		{
			name: "unlock", path: "/unlock",
			form: url.Values{"projectId": {"p-1"}, "userId": {"1024"}},
			check: func(t *testing.T, got any) {
				r := got.(*membershipdto.UnlockManualReq)
				if r.UserID != 1024 || r.ProjectID != "p-1" {
					t.Fatalf("unlock contract: %+v", r)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &membershipWriteStub{}
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, tc.path+"?lang=en-US", strings.NewReader(tc.form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			membershipWriteEngine(NewHandle(svc)).ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
				t.Fatalf("写响应必须是 HTML 片段，实际 %q", ct)
			}
			if body := rec.Body.String(); !strings.Contains(body, `role="status"`) {
				t.Fatalf("成功应回 Jet 结果片段，实际 %s", body)
			}
			tc.check(t, svc.got)
		})
	}
}

func TestMembershipPageWriteRejectsMalformedAmount(t *testing.T) {
	svc := &membershipWriteStub{}
	rec := httptest.NewRecorder()
	form := url.Values{"projectId": {"p-1"}, "name": {"Silver"}, "thresholdYuan": {"12.345"}}
	req := httptest.NewRequest(http.MethodPost, "/write", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	membershipWriteEngine(NewHandle(svc)).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || svc.got != nil {
		t.Fatalf("非法金额应 400 且不进 service：%d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "data-ui-error") {
		t.Fatalf("失败应渲染错误片段：%s", rec.Body.String())
	}
}
