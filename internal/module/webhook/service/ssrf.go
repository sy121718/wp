package webhookservice

// ssrf.go — 出站 webhook 的 SSRF 防护（SEC-015）。
//
// 防线是「DNS 解析后的 IP」而不是 hostname 字符串：攻击者可以把内网地址
// 绑到公网域名上（DNS rebinding / 域名指内网），只看字符串拦不住。
// 因此这里强制解析出全部 A/AAAA 记录，任何一个 IP 落在禁止网段即整体拒绝。
//
// lookupIP 做成包级变量是为了测试可注入（私有 DNS / 环回场景不必真起解析）。

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"

	webhookenums "go_wp/internal/module/webhook/enums"
)

// lookupIP DNS 解析入口（测试可替换）。
var lookupIP = (*net.Resolver).LookupIPAddr

// validateURL 投递路径调用的校验入口（测试可替换；生产恒为 validateWebhookURL）。
var validateURL = validateWebhookURL

// SSRF 防护错误。
//
// 错误值取自 enums 的 **i18n key**（不是中文文案）：这几个错误会经 service 直接
// 返回给 HTTP 层，而 pkg/response.ErrorAuto 按「值是不是 key 形态」区分业务错误 ——
// 用中文原文会让「你填了个内网地址」变成「服务器内部错误，请稍后重试」（500），
// 用户看不出该改哪里。文案在迁移 216 的词条里。
//
// 导出这些变量而不是让调用方比较 key 字符串：调用方（含测试）比较的是变量本身。
var (
	ErrWebhookURLScheme = errors.New(webhookenums.ErrURLSchemeUnsupported)
	ErrWebhookURLHost   = errors.New(webhookenums.ErrURLHostMissing)
	ErrWebhookURLNoIP   = errors.New(webhookenums.ErrURLUnresolvable)
	ErrWebhookURLDenied = errors.New(webhookenums.ErrURLDenied)
)

// isForbiddenIP 判断 IP 是否落在禁止网段：
// 私有段（10/8、172.16/12、192.168/16、IPv6 fc00::/7）、环回（127/8、::1）、
// 链路本地（169.254/16、fe80::/10）、组播与未指定地址。
func isForbiddenIP(ip net.IP) bool {
	return !ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified()
}

// dialWebhookContext 把校验绑定到真正建立连接的 IP，避免校验与拨号各解析一次域名。
// URL 中的 hostname 保持原样，HTTP Host 与 TLS 证书校验仍针对原始主机。
func dialWebhookContext(ctx context.Context, network, address string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, DialTimeout)
	defer cancel()
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	ips, err := lookupIP(net.DefaultResolver, ctx, host)
	if err != nil || len(ips) == 0 {
		return nil, ErrWebhookURLNoIP
	}
	// 先检查全部结果，再尝试连接；混合公私地址不得因记录顺序不同而放行。
	for _, addr := range ips {
		if isForbiddenIP(addr.IP) || addr.Zone != "" {
			return nil, ErrWebhookURLDenied
		}
	}
	dialer := net.Dialer{Timeout: DialTimeout}
	for _, addr := range ips {
		var conn net.Conn
		conn, err = dialer.DialContext(ctx, network, net.JoinHostPort(addr.IP.String(), port))
		if err == nil {
			return conn, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, err
}

// validateWebhookURL 校验出站目标 URL：协议白名单 + DNS 解析后逐 IP 检查。
//
// 注意：本函数只做网络层防护；「域名白名单」（只允许向预注册端点发请求）
// 由 service 层保证 —— 投递永远使用 webhook_endpoints 里存的 URL，
// 事件发布方根本没有指定 URL 的入口。
func validateWebhookURL(raw string) (err error) {
	u, perr := url.Parse(raw)
	if perr != nil {
		// 丢掉 url.Parse 的技术细节：用户要的是「这个地址填得不对」，
		// 而不是 Go 标准库的错误串（后者会一起被渲染到界面上）。
		return errors.New(webhookenums.ErrURLMalformed)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ErrWebhookURLScheme
	}
	host := u.Hostname()
	if strings.TrimSpace(host) == "" {
		return ErrWebhookURLHost
	}

	ctx, cancel := context.WithTimeout(context.Background(), DialTimeout)
	defer cancel()
	ips, lerr := lookupIP(net.DefaultResolver, ctx, host)
	if lerr != nil || len(ips) == 0 {
		return ErrWebhookURLNoIP
	}
	for _, addr := range ips {
		if isForbiddenIP(addr.IP) {
			return ErrWebhookURLDenied
		}
	}
	return nil
}
