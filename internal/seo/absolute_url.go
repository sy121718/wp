package seo

import (
	"os"
	"strings"
)

const SiteBaseURLEnv = "WP_SITE_BASE_URL"

func SiteBaseURL() string {
	return strings.TrimRight(strings.TrimSpace(os.Getenv(SiteBaseURLEnv)), "/")
}

func AbsoluteSiteURL(pathOrURL string) string {
	v := strings.TrimSpace(pathOrURL)
	if v == "" {
		return pathOrURL
	}
	if !strings.HasPrefix(v, "/") || strings.HasPrefix(v, "//") {
		return pathOrURL
	}
	base := SiteBaseURL()
	if base == "" {
		return pathOrURL
	}
	return JoinURL(base, v)
}
