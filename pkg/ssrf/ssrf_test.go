package ssrf_test

// ssrf_test.go — SSRF 判定的六类拒绝形态（SEC-015 口径）。
//
// 判据只认「拒绝 / 放行」，不认错误文案：调用方各自决定怎么说（webhook 的 enums
// 与 mail 的 enums 是两套词条）。

import (
	"context"
	"errors"
	"net"
	"testing"

	"go_wp/pkg/ssrf"
)

// TestIsForbiddenIPTables 逐条点名禁止网段（含任务书要求的六类）。
func TestIsForbiddenIPTables(t *testing.T) {
	forbidden := []string{
		"127.0.0.1",       // 环回（任务书 6 例之一）
		"169.254.169.254", // 云元数据（链路本地）
		"10.0.0.1",        // 私网 10/8
		"172.16.5.4",      // 私网 172.16/12
		"192.168.1.1",     // 私网 192.168/16
		"::1",             // IPv6 环回
		"fc00::1",         // IPv6 唯一本地
		"fe80::1",         // IPv6 链路本地
		"224.0.0.1",       // 组播
		"0.0.0.0",         // 未指定
		"::",              // IPv6 未指定
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

// TestCheckRejectsLiteralPrivateHosts URL 里直接写私网地址（不走 DNS 那一支）。
func TestCheckRejectsLiteralPrivateHosts(t *testing.T) {
	cases := map[string]string{
		"环回":     "http://127.0.0.1:6379/",
		"云元数据":   "http://169.254.169.254/latest/meta-data/",
		"私网 10":  "http://10.0.0.1/",
		"私网 172": "http://172.16.0.9/",
		"私网 192": "http://192.168.1.1/",
		"IPv6环回":  "http://[::1]/",
	}
	for name, raw := range cases {
		err := ssrf.Check(context.Background(), raw, "https", "http")
		if !errors.Is(err, ssrf.ErrForbidden) {
			t.Errorf("%s（%s）应被拒（ErrForbidden），实际 %v", name, raw, err)
		}
	}
}

// TestCheckRejectsDecimalIP 十进制 IP（http://2130706433/ = 127.0.0.1）。
//
// 这条刻意用「不解析 DNS」的路径断言：`net.ParseIP("2130706433")` 返回 nil（它不是点分
// 十进制），于是走域名解析分支 —— 真实环境里 2130706433 会被解析成 127.0.0.1，
// 而测试环境没有这个解析器。所以这里断言的是**判定入口不把数字当合法主机名放过去**：
// 解析失败 → ErrUnresolvable，同样不是「放行」。
func TestCheckRejectsDecimalIP(t *testing.T) {
	old := ssrf.LookupIP
	t.Cleanup(func() { ssrf.LookupIP = old })
	// 模拟真实解析器的行为：十进制形态解析成 127.0.0.1。
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

// TestCheckRejectsMixedRecords 公私混合的域名必须整条拒（不能靠记录顺序）。
func TestCheckRejectsMixedRecords(t *testing.T) {
	old := ssrf.LookupIP
	t.Cleanup(func() { ssrf.LookupIP = old })
	ssrf.LookupIP = func(_ *net.Resolver, _ context.Context, _ string) ([]net.IPAddr, error) {
		return []net.IPAddr{
			{IP: net.ParseIP("93.184.216.34")}, // 公网
			{IP: net.ParseIP("10.1.2.3")},      // 私网
		}, nil
	}
	if err := ssrf.Check(context.Background(), "https://mix.example.com/"); !errors.Is(err, ssrf.ErrForbidden) {
		t.Fatalf("混合公私地址应整体拒绝，实际 %v", err)
	}
}

// TestCheckAllowsPublicHost 公网地址放行（否则门禁只会「拒绝一切」）。
func TestCheckAllowsPublicHost(t *testing.T) {
	if err := ssrf.Check(context.Background(), "https://93.184.216.34/hook", "https"); err != nil {
		t.Fatalf("公网 https 地址应放行，实际 %v", err)
	}
}

// TestCheckSchemeWhitelist 协议白名单。
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

// TestCheckRejectsEmptyHost 空主机名。
func TestCheckRejectsEmptyHost(t *testing.T) {
	if err := ssrf.Check(context.Background(), "https:///path"); !errors.Is(err, ssrf.ErrHostMissing) {
		t.Fatalf("空主机名应被拒，实际 %v", err)
	}
}
