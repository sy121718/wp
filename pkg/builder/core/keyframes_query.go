package core

import (
	"strings"

	"go_wp/pkg/logger"
)

func KeyframeCSS(names []string) string {
	if len(names) == 0 {
		return ""
	}
	seen := make(map[string]bool, len(names))
	parts := make([]string, 0, len(names))
	for _, n := range names {
		name := strings.TrimSpace(n)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		css, ok := keyframeIndex[name]
		if !ok {
			logger.Scene("admin").With("keyframe", name).
				Warn("后台页面请求了不存在的动效词汇，已跳过（检查拼写，见 core/keyframes/*.css）")
			continue
		}
		parts = append(parts, css)
	}
	return strings.Join(parts, "\n")
}

func KeyframeNames() []string {
	names := make([]string, 0, len(keyframesCatalog))
	for _, k := range keyframesCatalog {
		names = append(names, k.Name)
	}
	return names
}
