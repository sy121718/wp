package seo

import "strings"

func EvaluateSchemaPresence(title, description, schemaType string) bool {
	if strings.TrimSpace(title) == "" && strings.TrimSpace(description) == "" {
		return false
	}
	return strings.TrimSpace(schemaType) != "" || strings.TrimSpace(title) != ""
}
