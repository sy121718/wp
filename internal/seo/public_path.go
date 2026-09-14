package seo

import "strings"

// CanonicalPublicPath 访问面路径的 SEO 规范形式。
//
// 语言根在文件系统层映射为 /index（或 /{code}/index），对外 canonical 与菜单
// 统一为 /（或 /{code}），避免 / 与 /index 重复内容（I18N-022）。
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

// IsPublicIndexAlias 是否为语言根别名路径（应对外 301 到 CanonicalPublicPath）。
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
