package inventoryhttp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	inventorycontract "go_wp/internal/module/inventory/contract"
	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	bulkids "go_wp/pkg/bulkids"
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
		{"partial", url.Values{"projectId": {"p1"}, "status": {"disabled"}, "ids": {"1", "2", "1"}}, "2", 2, "已更新"},
		// 无效状态给出的是声明处的中文兜底（原先兜底表不在 enums 里，页面上直接是裸 key）。
		{"invalid status", url.Values{"projectId": {"p1"}, "status": {"unknown"}, "ids": {"1"}}, "", 0, inventoryenums.FallbackFor(inventoryenums.ErrReasonStatusInvalid)},
		{"empty selection", url.Values{"projectId": {"p1"}, "status": {"active"}}, "", 0, "请选择要操作的原因"},
		{"limit", func() url.Values {
			v := url.Values{"projectId": {"p1"}, "status": {"active"}}
			for i := 0; i <= bulkids.MaxIDs; i++ {
				v.Add("ids", fmt.Sprint(i))
			}
			return v
		}(), "", 0, "一次最多操作"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, rec, view := inventoryFormContext(t, tc.values, false)
			spy := &reasonUpdateSpy{failID: tc.failID}
			(&inventoryPageHandle{inventory: spy}).InventoryReasonsBulkStatus(c)
			c.Writer.WriteHeaderNow()
			// 结论走响应体：失败一律 400 + JSON，不再渲染提示页。
			if rec.Code != http.StatusBadRequest || view.name != "" {
				t.Fatalf("状态码 %d 模板 %q，want 400 且不渲染", rec.Code, view.name)
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
	route := `pages.POST("/inventory/reasons/bulk-status", builtin.CasbinMiddlewareForPath("/api/inventory/reason/update"), inventoryPages.InventoryReasonsBulkStatus)`
	if !strings.Contains(string(body), route) {
		t.Fatal("bulk route must share reason_update enforcement")
	}
}
