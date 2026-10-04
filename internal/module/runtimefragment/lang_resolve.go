package runtimefragment

// lang_resolve.go — 片段请求语言解析（I18N-011）。

import (
	"context"
	"strings"

	"go_wp/pkg/i18n"
)

// fragmentLangParam 语言参数名（GET 走 query、POST 走表单，同名）。
//
// **片段语言只能从它来**：端点不读 Accept-Language、不读任何语言 cookie
// （后台那套「Cookie lang → query lang → Accept-Language」是管理面链路，
// 见 pkg/response；访问面片段刻意不复用，理由见 docs/06-D §11 结论）。
// 这条约束同时是缓存正确性的前提 —— 语言进 URL 就进了缓存键。
const fragmentLangParam = "lang"

// resolveRequestLang 解析并校验请求语言：?lang= 优先，非法值回落工程默认语言。
func resolveRequestLang(ctx context.Context, projectID, rawLang string) string {
	lang := strings.TrimSpace(rawLang)
	if deps.FragmentProject != nil && strings.TrimSpace(projectID) != "" {
		if lang != "" {
			if enabled, err := deps.FragmentProject.EnabledLangs(ctx, projectID); err == nil {
				for _, e := range enabled {
					if e == lang {
						return lang
					}
				}
			}
		}
		if def, err := deps.FragmentProject.DefaultLocale(ctx, projectID); err == nil && def != "" {
			return def
		}
	}
	if lang != "" {
		return lang
	}
	return i18n.GetDefaultLang()
}
