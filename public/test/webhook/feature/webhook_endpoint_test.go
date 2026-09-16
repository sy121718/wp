package feature

// webhook_endpoint_test.go — webhook 端点管理与投递排障的接口层链路（OSS-006 接线后）。
//
// 这一层此前完全没有测试：模块从 199 建表到 2026-09-16 一直是没有调用方的死代码，
// service 内部的 ssrf / sign / deliver 有单测，但**接口层的错误映射**从没被验证过。
//
// 重点断言的是**状态码**：pkg/response.ErrorAuto 按「enums 常量值是不是 i18n key 形态」
// 区分业务错误与内部错误 —— key 形态给调用方传的 400，否则一律 500 + 通用文案。
// 一旦 enums 退回中文原文，这里的用例就会变红（pkg/response 的覆盖率账本要求每个
// 用 ErrorAuto 的模块都有这条断言，webhook 不在基线里）。

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

// newWebhookEngine 只挂 webhook 的写入路由。
//
// 刻意不做完整装配：本用例验证的是「业务错误 → 状态码」这一条映射，
// 与会话 / CSRF / Casbin 无关（那三层链由路由组负责，不是 handle 的职责）。
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

// TestWebhookInvalidInputReturns400 参数类业务错误必须是 400，不能退化成 500。
func TestWebhookInvalidInputReturns400(t *testing.T) {
	e := newWebhookEngine(t)

	cases := []struct {
		name string
		path string
		body string
	}{
		// 前两个用例在 URL 校验**之前**就返回，因此不触发 DNS / SSRF 判定（离线可跑）。
		{"创建端点缺事件类型", "/api/webhook/endpoint/save", `{"targetUrl":"https://example.com/hook","secret":"s3cret"}`},
		{"创建端点缺目标地址", "/api/webhook/endpoint/save", `{"eventType":"order.paid","secret":"s3cret"}`},
		// 内网地址：SSRF 校验拒绝（写入与投递都查 DNS 解析后的内网 IP），同样是 400。
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

// TestWebhookEnumsAreBusinessErrors 逐个校验 webhook 的 Err* 常量命中业务错误判据。
//
// 与上面那条互补：接口用例只覆盖被触达的少数分支，这一条把**每一个**常量都钉住 ——
// 新增常量忘了用 key 形态时，即使没有任何接口用例走到它，这里也会红。
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

// TestWebhookDispatchWithoutEndpointIsNoop 没有端点订阅时派发是「0 条且无错误」。
//
// 这条语义很重要：事件发布方（订单）把返回值当 best-effort 通知，
// 若这里返回 error，支付落账会记一条永远存在的告警日志（实际只是没人订阅）。
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
