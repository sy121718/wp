package webhookservice

// ssrf_test.go — SSRF 防护测试：内网 IP / 私有 DNS / 环回地址全部拒绝（SEC-015）。

import (
	"context"
	"errors"
	"net"
	"testing"
)

// fakeLookup 构造一个固定返回的 lookupIP 替身。
func fakeLookup(ips []net.IPAddr, err error) func(*net.Resolver, context.Context, string) ([]net.IPAddr, error) {
	return func(_ *net.Resolver, _ context.Context, _ string) ([]net.IPAddr, error) {
		return ips, err
	}
}

// TestValidateWebhookURL_RejectsForbiddenHostnames 环回与内网 hostname 直连一律拒绝。
func TestValidateWebhookURL_RejectsForbiddenHostnames(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"环回IPv4", "http://127.0.0.1/hook"},
		{"环回变体", "http://127.9.9.9/hook"},
		{"IPv6环回", "http://[::1]/hook"},
		{"十网段", "http://10.1.2.3/hook"},
		{"172私有段", "http://172.16.0.1/hook"},
		{"172私有段上界内", "http://172.31.255.255/hook"},
		{"192私有段", "http://192.168.1.1/hook"},
		{"链路本地", "http://169.254.169.254/latest/meta-data/"},
		{"未指定地址", "http://0.0.0.0/hook"},
		{"IPv6链路本地", "http://[fe80::1]/hook"},
		{"IPv6ULA", "http://[fd00::1]/hook"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := validateWebhookURL(c.raw); err == nil {
				t.Fatalf("期望拒绝 %s，实际放行", c.raw)
			}
		})
	}
}

// TestValidateWebhookURL_RejectsPrivateDNS 私有 DNS：公网域名解析到内网 IP 必须拒绝。
func TestValidateWebhookURL_RejectsPrivateDNS(t *testing.T) {
	orig := lookupIP
	t.Cleanup(func() { lookupIP = orig })
	lookupIP = fakeLookup([]net.IPAddr{{IP: net.ParseIP("10.0.0.5")}}, nil)
	if err := validateWebhookURL("https://innocent.example.com/hook"); err == nil {
		t.Fatal("公网域名解析到内网 IP 期望拒绝，实际放行")
	}
}

// TestValidateWebhookURL_RejectsResolveFailure 解析失败（无记录 / DNS 错误）一律拒绝，不降级放行。
func TestValidateWebhookURL_RejectsResolveFailure(t *testing.T) {
	orig := lookupIP
	t.Cleanup(func() { lookupIP = orig })
	lookupIP = fakeLookup(nil, errors.New("dns: no such host"))
	if err := validateWebhookURL("https://nonexistent.example.com/hook"); err == nil {
		t.Fatal("解析失败期望拒绝，实际放行")
	}
	lookupIP = fakeLookup(nil, nil)
	if err := validateWebhookURL("https://empty.example.com/hook"); err == nil {
		t.Fatal("解析出 0 条记录期望拒绝，实际放行")
	}
}

// TestValidateWebhookURL_AcceptsPublicHost 公网 IP 正常放行。
func TestValidateWebhookURL_AcceptsPublicHost(t *testing.T) {
	orig := lookupIP
	t.Cleanup(func() { lookupIP = orig })
	lookupIP = fakeLookup([]net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil)
	if err := validateWebhookURL("https://example.com/hook"); err != nil {
		t.Fatalf("公网 IP 期望放行，实际拒绝: %v", err)
	}
}

// TestValidateWebhookURL_RejectsBadInput 协议与格式防线。
func TestValidateWebhookURL_RejectsBadInput(t *testing.T) {
	for _, raw := range []string{"", "ftp://example.com", "file:///etc/passwd", "http:///nohost"} {
		if err := validateWebhookURL(raw); err == nil {
			t.Fatalf("期望拒绝 %q，实际放行", raw)
		}
	}
}
