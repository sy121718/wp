package membershiphttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	membershipcontract "go_wp/internal/module/membership/contract"
	membershipdto "go_wp/internal/module/membership/dto"
	membershipenums "go_wp/internal/module/membership/enums"
	"go_wp/pkg/i18n"
)

type membershipWriteStub struct {
	membershipcontract.MembershipService
	got any
}

func (s *membershipWriteStub) CreateTier(_ context.Context, req *membershipdto.CreateTierReq) (*membershipdto.TierResp, error) {
	s.got = req
	return &membershipdto.TierResp{ID: 2}, nil
}

func (s *membershipWriteStub) UpdateTier(_ context.Context, req *membershipdto.UpdateTierReq) (*membershipdto.TierResp, error) {
	s.got = req
	return &membershipdto.TierResp{ID: req.TierID}, nil
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

func TestMembershipPageWritesUseExistingJSONContracts(t *testing.T) {
	gin.SetMode(gin.TestMode)
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
		name    string
		body    string
		message string
		handle  func(*Handle) gin.HandlerFunc
		check   func(*testing.T, any)
	}{
		{"create", `{"projectId":"p-1","name":"Silver","sortOrder":-1,"thresholdAmount":100005,"isDefault":false,"remark":"memo","entitlements":[{"kind":"discount","valueInt":20}]}`, "EN-CREATED", func(h *Handle) gin.HandlerFunc { return h.CreateTier }, func(t *testing.T, got any) {
			r := got.(*membershipdto.CreateTierReq)
			if r.ThresholdAmount != 100005 || r.SortOrder != -1 || len(r.Entitlements) != 1 || r.Entitlements[0].ValueInt != 20 {
				t.Fatalf("create contract: %+v", r)
			}
		}},
		{"update", `{"projectId":"p-1","tierId":2,"name":"Silver","sortOrder":-1,"thresholdAmount":0,"remark":""}`, "EN-UPDATED", func(h *Handle) gin.HandlerFunc { return h.UpdateTier }, func(t *testing.T, got any) {
			r := got.(*membershipdto.UpdateTierReq)
			if r.IsDefault != nil || r.ThresholdAmount == nil || *r.ThresholdAmount != 0 || r.Remark == nil || *r.Remark != "" || r.SortOrder == nil || *r.SortOrder != -1 {
				t.Fatalf("update must preserve explicit zero and omitted default: %+v", r)
			}
		}},
		{"default", `{"projectId":"p-1","tierId":2,"isDefault":true}`, "EN-UPDATED", func(h *Handle) gin.HandlerFunc { return h.UpdateTier }, func(t *testing.T, got any) {
			r := got.(*membershipdto.UpdateTierReq)
			if r.IsDefault == nil || !*r.IsDefault || r.Name != nil || r.ThresholdAmount != nil || r.Remark != nil || r.SortOrder != nil {
				t.Fatalf("default must only change default: %+v", r)
			}
		}},
		{"delete", `{"projectId":"p-1","tierId":2}`, "EN-DELETED", func(h *Handle) gin.HandlerFunc { return h.DeleteTier }, func(t *testing.T, got any) {
			r := got.(*membershipdto.DeleteTierReq)
			if r.ProjectID != "p-1" || r.TierID != 2 {
				t.Fatalf("delete contract: %+v", r)
			}
		}},
		{"clear-entitlements", `{"projectId":"p-1","tierId":2,"items":[]}`, "EN-SAVED", func(h *Handle) gin.HandlerFunc { return h.SaveEntitlements }, func(t *testing.T, got any) {
			r := got.(*membershipdto.SaveEntitlementsReq)
			if r.Items == nil || len(r.Items) != 0 || r.TierID != 2 {
				t.Fatalf("clear must pass an explicit empty final list: %+v", r)
			}
		}},
		{"assign", `{"projectId":"p-1","userId":1024,"tierId":2}`, "EN-ASSIGNED", func(h *Handle) gin.HandlerFunc { return h.AssignSet }, func(t *testing.T, got any) {
			r := got.(*membershipdto.AssignManualReq)
			if r.UserID != 1024 || r.TierID != 2 || r.ProjectID != "p-1" {
				t.Fatalf("assign contract: %+v", r)
			}
		}},
		{"unlock", `{"projectId":"p-1","userId":1024}`, "EN-UNLOCKED", func(h *Handle) gin.HandlerFunc { return h.AssignUnlock }, func(t *testing.T, got any) {
			r := got.(*membershipdto.UnlockManualReq)
			if r.UserID != 1024 || r.ProjectID != "p-1" {
				t.Fatalf("unlock contract: %+v", r)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &membershipWriteStub{}
			r := gin.New()
			r.POST("/write", tc.handle(NewHandle(svc)))
			req := httptest.NewRequest(http.MethodPost, "/write?lang=en-US", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
			var body struct {
				Message string `json:"message"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Message != tc.message {
				t.Fatalf("request-language message = %q, want %q", body.Message, tc.message)
			}
			tc.check(t, svc.got)
		})
	}
}
