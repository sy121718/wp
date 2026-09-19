package webhookservice

// deliver_test.go — 投递客户端测试：超时上限、请求体上限、签名头与固定头集。

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// withLooseValidate 临时放行 URL 校验：httptest 目标本就是 127.0.0.1，
// 内网拒绝行为已由 ssrf_test.go 专测覆盖。
func withLooseValidate(t *testing.T) {
	t.Helper()
	orig := validateURL
	t.Cleanup(func() { validateURL = orig })
	validateURL = func(string) error { return nil }
}

// 仅协议测试放行本地 httptest 连接；生产客户端仍使用受限拨号器。
func localWebhookClient(t *testing.T) *http.Client {
	t.Helper()
	client := newWebhookClient()
	client.Transport.(*http.Transport).DialContext = (&net.Dialer{Timeout: DialTimeout}).DialContext
	t.Cleanup(client.CloseIdleConnections)
	return client
}

// TestPostWebhook_Timeout 远端慢响应时受 ClientTimeout 约束并返回错误。
func TestPostWebhook_Timeout(t *testing.T) {
	withLooseValidate(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	client := &http.Client{Timeout: 50 * time.Millisecond}
	start := time.Now()
	_, err := postWebhook(context.Background(), client, srv.URL, "s", "e", 1, []byte("{}"))
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("慢响应期望超时错误，实际成功")
	}
	if elapsed > 200*time.Millisecond {
		t.Fatalf("超时未生效：耗时 %v 超过客户端上限", elapsed)
	}
}

// TestPostWebhook_PayloadTooLarge 请求体超上限直接拒绝，不发出请求。
func TestPostWebhook_PayloadTooLarge(t *testing.T) {
	withLooseValidate(t)
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	big := make([]byte, MaxPayloadBytes+1)
	if _, err := postWebhook(context.Background(), newWebhookClient(), srv.URL, "s", "e", 1, big); err == nil {
		t.Fatal("超限请求体期望拒绝，实际发出")
	}
	if called {
		t.Fatal("超限请求不应到达远端")
	}
}

// TestPostWebhook_SignatureAndFixedHeaders 头是固定集合：签名可验、无多余透传头。
func TestPostWebhook_SignatureAndFixedHeaders(t *testing.T) {
	withLooseValidate(t)
	var got *http.Request
	var rawBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		rawBody = make([]byte, r.ContentLength)
		_, _ = r.Body.Read(rawBody)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	secret := "sec"
	if _, err := postWebhook(context.Background(), localWebhookClient(t), srv.URL, secret, "order.paid", 7, []byte("{}")); err != nil {
		t.Fatalf("投递失败: %v", err)
	}

	sig := got.Header.Get(HeaderSignature)
	ts := got.Header.Get(HeaderTimestamp)
	if sig == "" || ts == "" {
		t.Fatal("缺少签名或时间戳头")
	}
	tsVal, perr := strconv.ParseInt(ts, 10, 64)
	if perr != nil {
		t.Fatalf("时间戳头不是整数: %v", perr)
	}
	if !VerifyPayload(secret, tsVal, rawBody, sig) {
		t.Fatal("远端视角验签失败")
	}
	if got.Header.Get(HeaderEvent) != "order.paid" || got.Header.Get(HeaderDelivery) != "7" {
		t.Fatal("事件 / 投递 ID 头不正确")
	}

	// 固定头集：除 Content-Type + 四个 webhook 头之外不允许出现其它头
	// （内部凭据没有透传通道 —— 安全模型第 6 条）。
	allowed := map[string]bool{
		"Content-Type": true, HeaderSignature: true, HeaderTimestamp: true,
		HeaderEvent: true, "X-Webhook-Delivery-Id": true,
		"Host": true, "Content-Length": true, "Accept-Encoding": true, "User-Agent": true,
	}
	for name := range got.Header {
		if !allowed[name] {
			t.Fatalf("出现非白名单请求头: %s", name)
		}
	}
}

// TestPostWebhook_RedirectNotAllowed 重定向到内网目标不跟随（302 视为非 2xx）。
func TestPostWebhook_RedirectNotAllowed(t *testing.T) {
	withLooseValidate(t)
	loopback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(loopback.Close)

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, &http.Request{}, loopback.URL, http.StatusFound)
	}))
	t.Cleanup(redirector.Close)

	status, err := postWebhook(context.Background(), localWebhookClient(t), redirector.URL, "s", "e", 1, []byte("{}"))
	if err != nil || status != http.StatusFound {
		t.Fatalf("必须到达首个服务并停在 302，status=%d err=%v", status, err)
	}
}

// TestDispatchEvent_PayloadLimit service 层入口同样执行负载上限。
func TestDispatchEvent_PayloadLimit(t *testing.T) {
	// 不依赖数据库：序列化超限应在查库前整体拒绝。
	// 构造超限负载 —— model 为 nil 也不会被触达（先在 marshal 后的大小检查处返回）。
	svc := &Service{}
	big := strings.Repeat("a", MaxPayloadBytes+1)
	if _, err := svc.DispatchEvent(context.Background(), "e", big); err == nil {
		t.Fatal("超限负载期望整体拒绝")
	}
}
