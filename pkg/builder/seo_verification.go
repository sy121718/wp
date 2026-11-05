package builder

import (
	"regexp"
	"strings"

	"go_wp/pkg/logger"
)

var searchConsoleVerificationPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)

func NormalizeSearchConsoleVerification(raw string) (token string, ok bool) {
	token = strings.TrimSpace(raw)
	if token == "" {
		return "", false
	}
	return token, searchConsoleVerificationPattern.MatchString(token)
}

func buildSearchConsoleHead(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	token, ok := NormalizeSearchConsoleVerification(trimmed)
	if !ok {
		logger.Scene("build").With("value", trimmed).
			Warn("Search Console 验证 token 形状不合法，已跳过验证 meta 注入（base64url 字符集，8~128 位）")
		return ""
	}

	return `<meta name="google-site-verification" content="` + token + `">` + "\n"
}

func WithSearchConsoleVerification(token string) CompileOption {
	return func(c *compileConfig) { c.searchConsoleVerification = strings.TrimSpace(token) }
}
