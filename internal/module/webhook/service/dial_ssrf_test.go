package webhookservice

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
)

// 校验阶段解析成公网、连接阶段变成环回；必须在拨号前再次拒绝。
func TestWebhookTransportRejectsDNSRebinding(t *testing.T) {
	original := lookupIP
	t.Cleanup(func() { lookupIP = original })
	calls := 0
	lookupIP = func(_ *net.Resolver, _ context.Context, _ string) ([]net.IPAddr, error) {
		calls++
		ip := "93.184.216.34"
		if calls > 1 {
			ip = "127.0.0.1"
		}
		return []net.IPAddr{{IP: net.ParseIP(ip)}}, nil
	}
	if err := validateWebhookURL("http://rebind.invalid/hook"); err != nil {
		t.Fatal(err)
	}
	client := newWebhookClient()
	defer client.CloseIdleConnections()
	_, err := client.Transport.(*http.Transport).DialContext(context.Background(), "tcp", "rebind.invalid:80")
	if !errors.Is(err, ErrWebhookURLDenied) {
		t.Fatalf("DNS 变化后必须在连接前拒绝，得到 %v（解析次数 %d）", err, calls)
	}
}
