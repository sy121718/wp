package text

import "go_wp/pkg/builder/core"

func sanitizeRichHTML(src string) string { return core.SanitizeRichHTML(src) }

func stripRichTags(src string) string { return core.StripRichTags(src) }
