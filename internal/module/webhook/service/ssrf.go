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
)

// lookupIP DNS 解析入口（测试可替换）。
var lookupIP = (*net.Resolver).LookupIPAddr

// validateURL 投递路径调用的校验入口（测试可替换；生产恒为 validateWebhookURL）。
var validateURL = validateWebhookURL

// SSRF 防护错误（enums 层不放技术细节文案，这里导出供 service 映射）。
var (
	ErrWebhookURLScheme = errors.New("webhook URL 仅支持 http/https")
	ErrWebhookURLHost   = errors.New("webhook URL 缺少主机名")
	ErrWebhookURLNoIP   = errors.New("webhook 主机无法解析出 IP 地址")
	ErrWebhookURLDenied = errors.New("webhook 目标解析到内网或保留地址，已拒绝")
)

// isForbiddenIP 判断 IP 是否落在禁止网段：
// 私有段（10/8、172.16/12、192.168/16、IPv6 fc00::/7）、环回（127/8、::1）、
// 链路本地（169.254/16、fe80::/10）、组播与未指定地址。
func isForbiddenIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified()
}

// validateWebhookURL 校验出站目标 URL：协议白名单 + DNS 解析后逐 IP 检查。
//
// 注意：本函数只做网络层防护；「域名白名单」（只允许向预注册端点发请求）
// 由 service 层保证 —— 投递永远使用 webhook_endpoints 里存的 URL，
// 事件发布方根本没有指定 URL 的入口。
func validateWebhookURL(raw string) (err error) {
	u, perr := url.Parse(raw)
	if perr != nil {
		return errors.New("webhook URL 格式非法: " + perr.Error())
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ErrWebhookURLScheme
	}
	host := u.Hostname()
	if strings.TrimSpace(host) == "" {
		return ErrWebhookURLHost
	}

	ips, lerr := lookupIP(net.DefaultResolver, context.Background(), host)
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
