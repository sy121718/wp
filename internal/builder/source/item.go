package source

import (
	"strings"
	"time"
)

const (
	ItemFieldID = "id"

	ItemFieldSlug = "slug"

	ItemFieldCreatedAt = "createdAt"

	ItemFieldMinPrice = "minPrice"

	ItemFieldRatingValue = "ratingValue"
)

func ItemFloat(item map[string]any, key string) (v float64, ok bool) {
	raw, exists := item[key]
	if !exists {
		return 0, false
	}
	switch n := raw.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

func ItemString(item map[string]any, key string) string {
	v, _ := item[key].(string)
	return v
}

func ItemTime(item map[string]any, key string) (t time.Time, ok bool) {
	if v, ok := item[key]; ok {
		switch raw := v.(type) {
		case int64:
			return time.UnixMilli(raw), true
		case float64:
			return time.UnixMilli(int64(raw)), true
		case int:
			return time.UnixMilli(int64(raw)), true
		}
	}
	s := strings.TrimSpace(ItemString(item, key))
	if s == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}
