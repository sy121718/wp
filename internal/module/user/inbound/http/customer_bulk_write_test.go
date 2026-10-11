package userhttp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	userdto "go_wp/internal/module/user/dto"
	"go_wp/pkg/bulkids"
)

func TestCustomerStatusRequiresExplicitTarget(t *testing.T) {
	fake := &fakeCustomerAdmin{}
	engine := gin.New()
	engine.POST("/api/customer/status", NewCustomerHandle(fake).SetCustomerStatus)
	for _, body := range []string{`{"customerId":42}`, `{"customerId":42,"status":null}`} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/customer/status", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		engine.ServeHTTP(rec, req)
		if rec.Code != 400 || fake.statusReq != nil {
			t.Fatalf("missing target wrote status: %d %+v", rec.Code, fake.statusReq)
		}
	}
}

func customerBulkJSON(t *testing.T, fake *fakeCustomerAdmin, path, body string) (int, map[string]any) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewCustomerHandle(fake)
	r.POST("/api/customer/bulk-status", h.BulkCustomerStatus)
	r.POST("/api/customer/bulk-unlock", h.BulkCustomerUnlock)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	var result map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("API 响应不是 JSON: %s: %v", rec.Body.String(), err)
	}
	return rec.Code, result
}

func TestCustomerBulkStatusReturnsEveryOutcome(t *testing.T) {
	fake := &fakeCustomerAdmin{failIDs: map[uint64]bool{7: true}}
	code, body := customerBulkJSON(t, fake, "/api/customer/bulk-status", `{"ids":[42,7,42,43],"status":0}`)
	if code != 200 {
		t.Fatalf("status=%d body=%v", code, body)
	}
	data := body["data"].(map[string]any)
	if data["updated"] != float64(2) || data["skipped"] != float64(1) || len(data["items"].([]any)) != 3 {
		t.Fatalf("计数/逐项结果失真: %v", data)
	}
	if got := fake.statusIDs; len(got) != 3 || got[0] != 42 || got[1] != 7 || got[2] != 43 {
		t.Fatalf("未逐条去重保序: %v", got)
	}
	failed := data["items"].([]any)[1].(map[string]any)
	if failed["updated"] != false || failed["message"] == "" {
		t.Fatalf("失败条目丢失原因: %v", failed)
	}
}

func TestCustomerBulkUnlockSeparatesChangedAndNoop(t *testing.T) {
	fake := &fakeCustomerAdmin{unlockByID: map[uint64]*userdto.CustomerUnlockResp{
		42: {CustomerID: 42, Unlocked: true}, 43: {CustomerID: 43, Cleared: true}, 44: {CustomerID: 44},
	}}
	code, body := customerBulkJSON(t, fake, "/api/customer/bulk-unlock", `{"ids":[42,43,44]}`)
	if code != 200 {
		t.Fatalf("status=%d body=%v", code, body)
	}
	data := body["data"].(map[string]any)
	if data["unlocked"] != float64(1) || data["cleared"] != float64(1) || data["noop"] != float64(1) || data["skipped"] != float64(0) {
		t.Fatalf("解锁结果三态失真: %v", data)
	}
}

func TestCustomerBulkRejectsInvalidTargetAndLimit(t *testing.T) {
	fake := &fakeCustomerAdmin{}
	for _, body := range []string{`{"ids":[42],"status":2}`, `{"ids":[42],"status":"0"}`, `{"ids":[42],"status":0,"extra":1}`} {
		code, _ := customerBulkJSON(t, fake, "/api/customer/bulk-status", body)
		if code != 400 {
			t.Fatalf("非法状态或形状 %s 应 400，实得 %d", body, code)
		}
	}
	ids := make([]uint64, bulkids.MaxIDs+1)
	for i := range ids {
		ids[i] = uint64(i + 1)
	}
	raw, _ := json.Marshal(map[string]any{"ids": ids, "status": 0})
	code, _ := customerBulkJSON(t, fake, "/api/customer/bulk-status", string(raw))
	if code != 400 || len(fake.statusIDs) != 0 {
		t.Fatalf("超限未整批拒绝: code=%d writes=%v", code, fake.statusIDs)
	}
}
