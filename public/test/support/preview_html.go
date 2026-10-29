package support

import (
	"regexp"
	"strings"
	"testing"

	"go_wp/internal/templates"
)

var previewOnlyLines = regexp.MustCompile(`(?m)^(?:<meta name="site-preview-(?:mode|state)"[^\n]*>|\.sky-preview-notice\{[^\n]*\}|<aside class="sky-preview-notice" role="status">[^\n]*</aside>)\n`)
var trackingConfigLine = regexp.MustCompile(`(?m)window\.__skyTrack=\{[^\n]*\};\n`)

// ComparableSiteHTML 仅去掉明确的模拟预览外壳和正式采集源码，保留业务结构与增强脚本。
// 调用者还须分别断言模拟标识、采集是否出现；它们不属于两种用途的公共字节。
func ComparableSiteHTML(t *testing.T, html string) string {
	t.Helper()
	track, err := templates.StaticJS("track.js")
	if err != nil {
		t.Fatal(err)
	}
	html = previewOnlyLines.ReplaceAllString(html, "")
	html = trackingConfigLine.ReplaceAllString(html, "")
	html = strings.Replace(html, track, "", 1)
	html = strings.Replace(html, "<script></script>\n", "", 1)
	return html
}
