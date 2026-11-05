package builder

import (
	"fmt"
	json "go_wp/pkg/jsonx"
	"regexp"
	"strings"

	"go_wp/pkg/logger"
)

var ga4MeasurementIDPattern = regexp.MustCompile(`^G-[A-Z0-9]{4,20}$`)

func NormalizeGA4MeasurementID(raw string) (id string, ok bool) {
	id = strings.ToUpper(strings.TrimSpace(raw))
	if id == "" {
		return "", false
	}
	return id, ga4MeasurementIDPattern.MatchString(id)
}

func buildGA4Head(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	id, ok := NormalizeGA4MeasurementID(trimmed)
	if !ok {
		logger.Scene("build").With("value", trimmed).
			Warn("GA4 测量 ID 形状不合法，已跳过统计代码注入（形如 G-XXXXXXXXXX）")
		return ""
	}

	var sb strings.Builder
	sb.WriteString(`<script async src="https://www.googletagmanager.com/gtag/js?id=`)
	sb.WriteString(id)
	sb.WriteString(`"></script>` + "\n")
	sb.WriteString("<script>window.dataLayer=window.dataLayer||[];function gtag(){dataLayer.push(arguments);}" +
		"gtag('js',new Date());gtag('config','" + id + "');</script>")
	return sb.String()
}

func WithGA4MeasurementID(id string) CompileOption {
	return func(c *compileConfig) { c.ga4MeasurementID = strings.TrimSpace(id) }
}

func buildTrackConfig(projectID, lang string) string {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return ""
	}
	payload, err := json.Marshal(map[string]string{
		"projectId": projectID,
		"lang":      strings.TrimSpace(lang),
	})
	if err != nil {
		return ""
	}
	return fmt.Sprintf("window.__skyTrack=%s;\n", payload)
}
