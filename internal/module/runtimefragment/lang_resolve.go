package runtimefragment

// lang_resolve.go — 片段请求语言解析（I18N-011）。

import (
	"context"
	"strings"

	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/i18n"
)

var fragmentProject projectcontract.ProjectService

// SetFragmentProject 注入工程契约（装配期调用；用于校验 lang 与回落默认语言）。
func SetFragmentProject(p projectcontract.ProjectService) { fragmentProject = p }

// resolveRequestLang 解析并校验请求语言：?lang= 优先，非法值回落工程默认语言。
func resolveRequestLang(ctx context.Context, projectID, rawLang string) string {
	lang := strings.TrimSpace(rawLang)
	if fragmentProject != nil && strings.TrimSpace(projectID) != "" {
		if lang != "" {
			if enabled, err := fragmentProject.EnabledLangs(ctx, projectID); err == nil {
				for _, e := range enabled {
					if e == lang {
						return lang
					}
				}
			}
		}
		if def, err := fragmentProject.DefaultLocale(ctx, projectID); err == nil && def != "" {
			return def
		}
	}
	if lang != "" {
		return lang
	}
	return i18n.GetDefaultLang()
}
