package inventoryhttp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"

	inventorycontract "go_wp/internal/module/inventory/contract"
	inventorydto "go_wp/internal/module/inventory/dto"
	bulkids "go_wp/pkg/bulkids"
	"go_wp/pkg/response"
)

type reasonUpdateSpy struct {
	inventorycontract.InventoryService
	calls  []*inventorydto.UpdateReasonReq
	failID string
}

func (s *reasonUpdateSpy) UpdateReason(_ context.Context, req *inventorydto.UpdateReasonReq) (*inventorydto.ReasonResp, error) {
	s.calls = append(s.calls, req)
	if req.ID == s.failID {
		return nil, errors.New("failure")
	}
	return &inventorydto.ReasonResp{ID: req.ID}, nil
}

func TestInventoryReasonsBulkStatus(t *testing.T) {
	for _, tc := range []struct {
		name      string
		values    url.Values
		failID    string
		wantCalls int
		wantTitle string
	}{
		// 部分失败仍是 400：失败片段给状态码档的通用文案（缺词时不露裸 key）。
		{"partial", url.Values{"projectId": {"p1"}, "status": {"disabled"}, "ids": {"1", "2", "1"}}, "1", 2, "data-ui-error"},

		// 没注入词库：业务错误落到共享归口文案，不泄漏裸 key。
		{"invalid status", url.Values{"projectId": {"p1"}, "status": {"unknown"}, "ids": {"1"}}, "", 0, "参数有误"},
		{"empty selection", url.Values{"projectId": {"p1"}, "status": {"active"}}, "", 0, "data-ui-error"},
		{"limit", func() url.Values {
			v := url.Values{"projectId": {"p1"}, "status": {"active"}}
			for i := 0; i <= bulkids.MaxIDs; i++ {
				v.Add("ids", fmt.Sprint(i))
			}
			return v
		}(), "", 0, response.InternalFallback},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spy := &reasonUpdateSpy{failID: tc.failID}
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/admin/inventory/reasons/bulk-status",
				strings.NewReader(tc.values.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			engine := inventoryFormEngine((&inventoryPageHandle{inventory: spy}).InventoryReasonsBulkStatus)
			engine.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("状态码 %d，want 400", rec.Code)
			}
			body := rec.Body.String()
			if !strings.Contains(body, tc.wantTitle) {
				t.Errorf("响应文案 %q 缺少 %q", body, tc.wantTitle)
			}
			if len(spy.calls) != tc.wantCalls {
				t.Fatalf("calls = %d, want %d", len(spy.calls), tc.wantCalls)
			}
			for _, req := range spy.calls {
				if req.ProjectID != "p1" || req.Status == nil || req.Name != nil || req.Sort != nil {
					t.Errorf("update changed non-target fields: %+v", req)
				}
			}
		})
	}
}

func TestInventoryReasonBulkUsesUpdatePermission(t *testing.T) {
	body, err := os.ReadFile("inventory_page_router.go")
	if err != nil {
		t.Fatal(err)
	}

	route := regexp.MustCompile(`pages\.POST\("/inventory/reasons/bulk-status",\s*builtin\.CasbinMiddlewareForPath\("/api/inventory/reason/update"\),\s*(?:pageview\.HTMLFragment,\s*)?[\w.]+\.InventoryReasonsBulkStatus\)`)
	if !route.Match(body) {
		t.Fatal("bulk route must share reason_update enforcement")
	}
}
