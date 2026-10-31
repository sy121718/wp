package builder

import (
	"regexp"
	"strings"

	"go_wp/pkg/logger"
)

const maxSiteScriptBytes = 16 * 1024

var siteScriptStructuralTagPattern = regexp.MustCompile(`(?i)<\s*/?\s*(!doctype|html|head|body)\b`)

func NormalizeHeadScripts(raw string) (script string, ok bool) {
	return normalizeSiteScript(raw)
}

func NormalizeBodyScripts(raw string) (script string, ok bool) {
	return normalizeSiteScript(raw)
}

func normalizeSiteScript(raw string) (script string, ok bool) {
	script = strings.TrimSpace(raw)
	if script == "" {
		return "", false
	}
	if len(script) > maxSiteScriptBytes {
		return "", false
	}
	if siteScriptStructuralTagPattern.MatchString(script) {
		return "", false
	}
	return script, true
}

func buildHeadScripts(raw string) string {
	return buildSiteScript(raw, "Head")
}

func buildBodyScripts(raw string) string {
	return buildSiteScript(raw, "Body")
}

func buildSiteScript(raw, kind string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	script, ok := normalizeSiteScript(trimmed)
	if !ok {
		logger.Scene("build").With("kind", kind).With("value", siteScriptLogPreview(trimmed)).
			Warn("自定义注入代码形状不合法，已跳过注入（不得含 <!DOCTYPE> / <html> / <head> / <body> 结构性标签，且不超过 16 KiB）")
		return ""
	}
	return script
}

func siteScriptLogPreview(s string) string {
	const maxRunes = 120
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "…"
}

func WithHeadScripts(script string) CompileOption {
	return func(c *compileConfig) { c.headScripts = strings.TrimSpace(script) }
}

func WithBodyScripts(script string) CompileOption {
	return func(c *compileConfig) { c.bodyScripts = strings.TrimSpace(script) }
}
