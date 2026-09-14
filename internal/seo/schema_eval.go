package seo

import "strings"

// EvaluateSchemaPresence 判断构建期是否会输出有意义的 JSON-LD（与 builder.buildJSONLD 口径对齐）。
func EvaluateSchemaPresence(title, description, schemaType string) bool {
	if strings.TrimSpace(title) == "" && strings.TrimSpace(description) == "" {
		return false
	}
	return strings.TrimSpace(schemaType) != "" || strings.TrimSpace(title) != ""
}
