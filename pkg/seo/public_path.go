package seo

import "strings"

func CanonicalPublicPath(path string) string {
	p := strings.TrimSpace(path)
	if p == "" {
		return ""
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	if p == "/index" || p == "/index.html" {
		return "/"
	}
	if strings.HasSuffix(p, "/index") {
		parent := strings.TrimSuffix(p, "/index")
		if parent == "" {
			return "/"
		}
		return parent
	}
	if strings.HasSuffix(p, "/index.html") {
		parent := strings.TrimSuffix(p, "/index.html")
		if parent == "" {
			return "/"
		}
		return parent
	}
	return p
}

func IsPublicIndexAlias(path string) bool {
	p := strings.TrimSpace(path)
	if p == "" {
		return false
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p == "/index" || p == "/index.html" || strings.HasSuffix(p, "/index") || strings.HasSuffix(p, "/index.html")
}
