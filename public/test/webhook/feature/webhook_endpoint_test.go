package feature

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	webhookenums "go_wp/internal/module/webhook/enums"
	webhookhttp "go_wp/internal/module/webhook/inbound/http"
	webhookmodel "go_wp/internal/module/webhook/model"
	webhookservice "go_wp/internal/module/webhook/service"
	"go_wp/pkg/response"
	"go_wp/public/test/support"
)

func newWebhookEngine(t *testing.T) *gin.Engine {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	svc := webhookservice.NewService(webhookmodel.NewWebhookModel(db))
	svc.SetCipherSecret("test-webhook-secret")

	gin.SetMode(gin.TestMode)
	e := gin.New()
	h := webhookhttp.NewHandle(svc)
	e.POST("/api/webhook/endpoint/save", h.EndpointSave)
	e.POST("/api/webhook/endpoint/status", h.EndpointStatus)
	e.POST("/api/webhook/delivery/retry", h.DeliveryRetry)
	e.GET("/api/webhook/endpoint/list", h.EndpointList)
	return e
}

func postJSON(t *testing.T, e *gin.Engine, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	return w
}

func TestWebhookInvalidInputReturns400(t *testing.T) {
	e := newWebhookEngine(t)

	cases := []struct {
		name string
		path string
		body string
	}{

		{"创建端点缺事件类型", "/api/webhook/endpoint/save", `{"targetUrl":"https://example.com/hook","secret":"s3cret"}`},
		{"创建端点缺目标地址", "/api/webhook/endpoint/save", `{"eventType":"order.paid","secret":"s3cret"}`},

		{"目标地址是内网", "/api/webhook/endpoint/save", `{"eventType":"order.paid","targetUrl":"http://127.0.0.1/hook","secret":"s3cret"}`},
		{"启停用非法状态值", "/api/webhook/endpoint/status", `{"id":1,"status":7}`},
		{"启停用缺 id", "/api/webhook/endpoint/status", `{"status":1}`},
		{"重投不存在的投递", "/api/webhook/delivery/retry?id=999999", ``},
	}
	for _, c := range cases {
		w := postJSON(t, e, c.path, c.body)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s：业务错误应返回 400，实际 %d（body=%s）—— 变成 500 说明 ErrorAuto "+
				"把它判成了内部错误（enums 常量值不再是 i18n key 形态）", c.name, w.Code, w.Body.String())
		}
	}
}

func TestWebhookEnumsAreBusinessErrors(t *testing.T) {
	consts := map[string]string{
		"ErrInvalidParam":       webhookenums.ErrInvalidParam,
		"ErrEndpointNotFound":   webhookenums.ErrEndpointNotFound,
		"ErrEventTypeRequired":  webhookenums.ErrEventTypeRequired,
		"ErrTargetURLRequired":  webhookenums.ErrTargetURLRequired,
		"ErrSecretRequired":     webhookenums.ErrSecretRequired,
		"ErrDeliveryNotFound":   webhookenums.ErrDeliveryNotFound,
		"ErrDeliveryNotFailed":  webhookenums.ErrDeliveryNotFailed,
		"ErrDeliveryNotPending": webhookenums.ErrDeliveryNotPending,
	}
	for name, v := range consts {
		if !response.IsBusinessError(errors.New(v)) {
			t.Fatalf("webhookenums.%s = %q 不被认作业务错误：经 ErrorAuto 会变成 500 + 通用文案", name, v)
		}
	}
}

func TestWebhookDispatchWithoutEndpointIsNoop(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	svc := webhookservice.NewService(webhookmodel.NewWebhookModel(db))
	svc.SetCipherSecret("test-webhook-secret")

	n, err := svc.DispatchEvent(context.Background(), "order.paid", map[string]any{"orderNo": "SO-1"})
	if err != nil {
		t.Fatalf("无订阅端点时不应报错: %v", err)
	}
	if n != 0 {
		t.Fatalf("无订阅端点时应入队 0 条，实际 %d", n)
	}
}
