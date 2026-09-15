package runtimefragment

// lang_resolve.go — 片段请求语言解析（I18N-011）。

import (
	"context"
	"strings"

	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/i18n"
)

// fragmentProject 工程契约（用于校验 lang 与回落默认语言）。
//
// 装配自检（审计 CQ-019）：判为 **optional-degraded** —— 未注入时语言一律回落
// 默认语言，而单语言站点本来就是这个形态（可见表现：多语言站点上片段渲染成默认语言，
// 用户能直接看出来）。这一条是「等价于功能未开启」那种正当降级，不做 fail-fast。
var fragmentProject projectcontract.ProjectService

// SetFragmentProject 注入工程契约（装配期调用；可选 —— 未注入即语言回落默认，见字段注释）。
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
