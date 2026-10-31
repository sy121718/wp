package i18n

import (
	"context"
	"strings"
)

func TranslateValues(ctx context.Context, lang string, store ContentStore, values map[string]string) {
	if len(values) == 0 || store == nil || strings.TrimSpace(lang) == "" {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	fields := make([]string, 0, len(values))
	hashes := make([]string, 0, len(values))
	for field, text := range values {
		if !ShouldTranslateContent(text) {
			continue
		}
		fields = append(fields, field)
		hashes = append(hashes, ContentHash(text))
	}
	if len(hashes) == 0 {
		return
	}

	t := NewContentTranslatorWith(ctx, store, lang, hashes)
	for _, field := range fields {
		values[field] = t.TranslateContent(values[field], field)
	}
}
