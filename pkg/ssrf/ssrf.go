package ssrf

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"
	"time"
)

var (
	ErrMalformed = errors.New("ssrf: url malformed")

	ErrSchemeUnsupported = errors.New("ssrf: scheme unsupported")

	ErrHostMissing = errors.New("ssrf: host missing")

	ErrUnresolvable = errors.New("ssrf: host unresolvable")

	ErrForbidden = errors.New("ssrf: target address forbidden")
)

var LookupIP = (*net.Resolver).LookupIPAddr

const DefaultTimeout = 5 * time.Second

func IsForbiddenIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	return !ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified()
}

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
