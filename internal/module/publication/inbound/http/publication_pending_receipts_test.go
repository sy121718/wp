package pubhttp_test

// publication_pending_receipts_test.go — 待处理回执端点（FIX-24）。
//
// 直调导出 handler（路由带 Session + Casbin）：证「服务层的 ListPendingReceipts 真的接到了
// HTTP 出口」—— 此前它是个 501 占位 + 一句「尚未实现」的错注释。

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	pubhttp "go_wp/internal/module/publication/inbound/http"
	pubmodel "go_wp/internal/module/publication/model"
	pubservice "go_wp/internal/module/publication/service"
	"go_wp/public/test/support"
)

func TestPendingReceiptsEndpointReturnsRealData(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		t.Skip("本地 PostgreSQL 不可用，跳过")
	}
	svc := pubservice.NewService(pubmodel.NewPublicationModel(db))
	h := pubhttp.PublicationPendingReceipts(svc)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/api/publication/receipts/pending", h)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/publication/receipts/pending", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("待处理回执查询应 200，实际 %d（body=%s）", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{`"items"`, `"total"`} {
		if !contains(body, want) {
			t.Fatalf("响应缺少 %s：%s", want, body)
		}
	}
	t.Logf("GET /api/publication/receipts/pending → %d %s", rec.Code, body)
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
