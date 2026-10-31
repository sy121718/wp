package webhookservice

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

func withLooseValidate(t *testing.T) {
	t.Helper()
	orig := validateURL
	t.Cleanup(func() { validateURL = orig })
	validateURL = func(string) error { return nil }
}

func localWebhookClient(t *testing.T) *http.Client {
	t.Helper()
	client := newWebhookClient()
	client.Transport.(*http.Transport).DialContext = (&net.Dialer{Timeout: DialTimeout}).DialContext
	t.Cleanup(client.CloseIdleConnections)
	return client
}

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

func TestDispatchEvent_PayloadLimit(t *testing.T) {

	svc := &Service{}
	big := strings.Repeat("a", MaxPayloadBytes+1)
	if _, err := svc.DispatchEvent(context.Background(), "e", big); err == nil {
		t.Fatal("超限负载期望整体拒绝")
	}
}
