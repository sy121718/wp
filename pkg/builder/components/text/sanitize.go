package text

import "go_wp/pkg/richtext"

func sanitizeRichHTML(src string) string { return richtext.SanitizeRichHTML(src) }

func stripRichTags(src string) string { return richtext.StripRichTags(src) }
