// Package ssrf 是出站目标的 SSRF 判定（解析后逐 IP 检查）。
//
// ## 为什么在 pkg 而不是某个模块的 service 里
//
// 判定逻辑只有一份，而消费者有两个：webhook 模块的投递（SEC-015）与 mail 自动化流程的
// 回调节点。此前它藏在 webhook/service 里作为未导出函数 —— mail 要么跨模块 import 别人的
// service（违反分层），要么抄一份判定（两份真源，下一次补一个网段必然只改一边）。
// 提到 pkg 之后两边调同一个函数，网段清单只维护这一处。
//
// ## 判定口径
//
// 「禁止」比「允许」好写也安全：默认拒绝一切非全局单播地址，再逐条点名环回 / 私有 /
// 链路本地 / 组播 / 未指定。IPv4 的十进制与八进制变体（`http://2130706433/`）由
// `net.ParseIP` 拒绝解析，因此调用方应当**先解析域名再判定**（见 Check），而不是自己
// 拿字符串做前缀匹配 —— 字符串匹配永远漏一种写法。
package ssrf

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"
	"time"
)

// 判定结论（不依赖具体模块的错误文案：调用方自己决定怎么说）。
var (
	// ErrMalformed 地址不是合法 URL。
	ErrMalformed = errors.New("ssrf: url malformed")
	// ErrSchemeUnsupported 协议不在白名单里（只允许 http / https）。
	ErrSchemeUnsupported = errors.New("ssrf: scheme unsupported")
	// ErrHostMissing 没有主机名。
	ErrHostMissing = errors.New("ssrf: host missing")
	// ErrUnresolvable 域名解析不出任何地址。
	ErrUnresolvable = errors.New("ssrf: host unresolvable")
	// ErrForbidden 目标落在禁止网段里（内网 / 环回 / 链路本地 / 组播 / 未指定）。
	ErrForbidden = errors.New("ssrf: target address forbidden")
)

// LookupIP 解析域名（测试可替换，避免依赖真实 DNS）。
var LookupIP = (*net.Resolver).LookupIPAddr

// DefaultTimeout 单次解析的超时。
const DefaultTimeout = 5 * time.Second

// IsForbiddenIP 判断 IP 是否落在禁止网段：
//
//	IPv4  10/8、172.16/12、192.168/16（IsPrivate）、127/8（IsLoopback）、
//	      169.254/16（IsLinkLocalUnicast）、224/4（IsMulticast）、0.0.0.0（IsUnspecified）
//	IPv6  ::1（IsLoopback）、fc00::/7（IsPrivate）、fe80::/10（IsLinkLocalUnicast）、
//	      ff00::/8（IsMulticast）、::（IsUnspecified）
//
// 用 `!IsGlobalUnicast()` 做第一道：它同时覆盖了「不是单播」的所有形态（含未来新增的
// 保留段），逐条点名则保证语义明确的几类不会被标准库的实现变化带走。
func IsForbiddenIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	return !ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified()
}

// Check 校验一个出站 URL：协议白名单 + 解析后逐 IP 判定。
//
// 三条要点：
//  1. **先解析再判定**：字符串形态的判定（是否像内网 IP）永远漏一种写法，
//     而解析之后拿到的是 net.IP，判定只看位模式；
//  2. **全部结果都要过**：混合公私地址的域名不能因为记录顺序不同而时通时不通；
//  3. 带 zone 的地址（fe80::1%eth0）一律拒绝 —— 那是本机链路，不可能是合法出站目标。
func Check(ctx context.Context, rawURL string, schemes ...string) error {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ErrMalformed
	}
	if !schemeAllowed(u.Scheme, schemes) {
		return ErrSchemeUnsupported
	}
	host := u.Hostname()
	if strings.TrimSpace(host) == "" {
		return ErrHostMissing
	}
	// 直接是 IP 的地址不必走 DNS（也避免某些解析器对 IP 字面量的奇怪行为）。
	if ip := net.ParseIP(host); ip != nil {
		if IsForbiddenIP(ip) {
			return ErrForbidden
		}
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()
	addrs, lerr := LookupIP(net.DefaultResolver, ctx, host)
	if lerr != nil || len(addrs) == 0 {
		return ErrUnresolvable
	}
	for _, addr := range addrs {
		if addr.Zone != "" || IsForbiddenIP(addr.IP) {
			return ErrForbidden
		}
	}
	return nil
}

// schemeAllowed 协议白名单（空清单时默认 http / https）。
func schemeAllowed(scheme string, schemes []string) bool {
	if len(schemes) == 0 {
		return scheme == "http" || scheme == "https"
	}
	for _, s := range schemes {
		if scheme == s {
			return true
		}
	}
	return false
}
