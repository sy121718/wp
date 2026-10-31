package support

import (
	"regexp"
	"strings"
	"testing"

	"go_wp/internal/templates"
)

var previewOnlyLines = regexp.MustCompile(`(?m)^(?:<meta name="site-preview-(?:mode|state)"[^\n]*>|\.sky-preview-notice\{[^\n]*\}|<aside class="sky-preview-notice" role="status">[^\n]*</aside>)\n`)
var trackingConfigLine = regexp.MustCompile(`(?m)window\.__skyTrack=\{[^\n]*\};\n`)

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
