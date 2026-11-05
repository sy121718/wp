package pathkit

import (
	"fmt"
	"strings"
)

const MaxRoutePathLen = 500

func NormalizeRoutePath(raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("路径不能为空")
	}
	if !strings.HasPrefix(raw, "/") {
		return "", fmt.Errorf("路径必须以 / 开头: %q", raw)
	}
	for _, r := range raw {
		switch {
		case r == '\\':
			return "", fmt.Errorf("路径含非法字符: %q", raw)
		case r < 0x20 || r == 0x7f:
			return "", fmt.Errorf("路径含控制字符: %q", raw)
		case r == ' ':
			return "", fmt.Errorf("路径含空格: %q", raw)
		case r == '?' || r == '#':
			return "", fmt.Errorf("路径含查询串或锚点: %q", raw)
		case r == '"' || r == '\'':
			return "", fmt.Errorf("路径含引号: %q", raw)
		}
	}

	if raw == "/" {
		return "/", nil
	}
	p := strings.TrimSuffix(raw, "/")
	if p == "" {
		return "", fmt.Errorf("路径含重复分隔符: %q", raw)
	}
	if len(p) > MaxRoutePathLen {
		return "", fmt.Errorf("路径过长（最大 %d 字符）: %q", MaxRoutePathLen, raw)
	}

	segs := strings.Split(strings.TrimPrefix(p, "/"), "/")
	for _, seg := range segs {
		if seg == "" {
			return "", fmt.Errorf("路径含重复分隔符: %q", raw)
		}
		if seg == "." || seg == ".." || strings.Contains(strings.ToLower(seg), "%2e") {
			return "", fmt.Errorf("路径含非法段 %q（拒绝路径穿越）: %q", seg, raw)
		}
	}

	if p == "/index" || p == "/index.html" {
		return "/", nil
	}
	return p, nil
}

func MatchKey(p string) string {
	return strings.TrimRight(strings.TrimSpace(p), "/")
}

// NormalizeSitePath 把站点 URL 路径归一成「以 / 开头、无重复斜杠、无尾斜杠」。
//
// 纯字符串处理：不判站点语义、不读工程配置。空串原样返回空串，
// 调用方可据此判断「未填写」与「已填写但归一后为空」的区别。
func NormalizeSitePath(raw string) string {
	p := strings.TrimSpace(raw)
	if p == "" {
		return ""
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	return strings.TrimRight(p, "/")
}
