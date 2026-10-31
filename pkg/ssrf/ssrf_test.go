package ssrf_test

import (
	"context"
	"errors"
	"net"
	"testing"

	"go_wp/pkg/ssrf"
)

func TestIsForbiddenIPTables(t *testing.T) {
	forbidden := []string{
		"127.0.0.1",
		"169.254.169.254",
		"10.0.0.1",
		"172.16.5.4",
		"192.168.1.1",
		"::1",
		"fc00::1",
		"fe80::1",
		"224.0.0.1",
		"0.0.0.0",
		"::",
	}
	for _, raw := range forbidden {
		ip := net.ParseIP(raw)
		if ip == nil {
			t.Fatalf("测试数据写错了：%q 不是 IP", raw)
		}
		if !ssrf.IsForbiddenIP(ip) {
			t.Errorf("%s 应被判为禁止目标", raw)
		}
	}

	allowed := []string{"93.184.216.34", "8.8.8.8", "2606:2800:220:1:248:1893:25c8:1946"}
	for _, raw := range allowed {
		ip := net.ParseIP(raw)
		if ip == nil {
			t.Fatalf("测试数据写错了：%q 不是 IP", raw)
		}
		if ssrf.IsForbiddenIP(ip) {
			t.Errorf("%s 是公网单播，不该被拒", raw)
		}
	}
}

func TestCheckRejectsLiteralPrivateHosts(t *testing.T) {
	cases := map[string]string{
		"环回":     "http://127.0.0.1:6379/",
		"云元数据":   "http://169.254.169.254/latest/meta-data/",
		"私网 10":  "http://10.0.0.1/",
		"私网 172": "http://172.16.0.9/",
		"私网 192": "http://192.168.1.1/",
		"IPv6环回": "http://[::1]/",
	}
	for name, raw := range cases {
		err := ssrf.Check(context.Background(), raw, "https", "http")
		if !errors.Is(err, ssrf.ErrForbidden) {
			t.Errorf("%s（%s）应被拒（ErrForbidden），实际 %v", name, raw, err)
		}
	}
}

func TestCheckRejectsDecimalIP(t *testing.T) {
	old := ssrf.LookupIP
	t.Cleanup(func() { ssrf.LookupIP = old })

	ssrf.LookupIP = func(_ *net.Resolver, _ context.Context, host string) ([]net.IPAddr, error) {
		if host == "2130706433" {
			return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
		}
		return nil, errors.New("no such host")
	}
	err := ssrf.Check(context.Background(), "http://2130706433/", "http", "https")
	if !errors.Is(err, ssrf.ErrForbidden) {
		t.Fatalf("十进制 IP 应被拒（解析后是环回），实际 %v", err)
	}
}

func TestCheckRejectsMixedRecords(t *testing.T) {
	old := ssrf.LookupIP
	t.Cleanup(func() { ssrf.LookupIP = old })
	ssrf.LookupIP = func(_ *net.Resolver, _ context.Context, _ string) ([]net.IPAddr, error) {
		return []net.IPAddr{
			{IP: net.ParseIP("93.184.216.34")},
			{IP: net.ParseIP("10.1.2.3")},
		}, nil
	}
	if err := ssrf.Check(context.Background(), "https://mix.example.com/"); !errors.Is(err, ssrf.ErrForbidden) {
		t.Fatalf("混合公私地址应整体拒绝，实际 %v", err)
	}
}

func TestCheckAllowsPublicHost(t *testing.T) {
	if err := ssrf.Check(context.Background(), "https://93.184.216.34/hook", "https"); err != nil {
		t.Fatalf("公网 https 地址应放行，实际 %v", err)
	}
}

func TestCheckSchemeWhitelist(t *testing.T) {
	if err := ssrf.Check(context.Background(), "https://93.184.216.34/", "https"); err != nil {
		t.Fatalf("https 在白名单内，应放行: %v", err)
	}
	if err := ssrf.Check(context.Background(), "http://93.184.216.34/", "https"); !errors.Is(err, ssrf.ErrSchemeUnsupported) {
		t.Fatalf("只允许 https 时 http 应被拒，实际 %v", err)
	}
	if err := ssrf.Check(context.Background(), "ftp://93.184.216.34/"); !errors.Is(err, ssrf.ErrSchemeUnsupported) {
		t.Fatalf("ftp 应被拒，实际 %v", err)
	}
}

func TestCheckRejectsEmptyHost(t *testing.T) {
	if err := ssrf.Check(context.Background(), "https:///path"); !errors.Is(err, ssrf.ErrHostMissing) {
		t.Fatalf("空主机名应被拒，实际 %v", err)
	}
}
