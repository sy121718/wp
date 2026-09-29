package pipeline

// compile_analytics.go — 站点级 <head> / <body> 注入的构建期装配
//（BIZ-8 GA4 + SEO-009 GSC 验证 meta + PIPE-8 自定义 Head/Body 代码）。
//
// 三条注入共用同一条链路、同一份 SiteSettings 快照：page 与 presentation 两条构建路径
// 都只调 AnalyticsCompileOptions 一行，读一次设置、装配多项注入 —— 各写一份读取
// 逻辑迟早分叉，而分叉的表现是「后台存了、产物里没有」这种最难排查的静默失效。
//
// PIPE-8 的自定义代码与另外两条的差别只在**内容形态**：GA4/GSC 由服务端按白名单
// 字符集拼装，自定义代码是管理员原文（第三方片段无法穷举形状）—— 因此它的安全论证
// 不在「字符集」而在「信任边界 + 受控注入点」，见 internal/builder/site_scripts.go。

import (
	"context"
	"strings"

	"go_wp/internal/builder"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/logger"
)

// siteSettingsSnapshot 读取站点设置快照；未配置 / 读失败返回零值（零字节注入）。
func siteSettingsSnapshot(ctx context.Context, project projectcontract.ProjectService, projectID string) projectcontract.SiteSettings {
	projectID = strings.TrimSpace(projectID)
	if project == nil || projectID == "" {
		return projectcontract.SiteSettings{}
	}
	p, err := project.Detail(ctx, &projectcontract.DetailReq{ID: projectID})
	if err != nil || p == nil {
		logger.Scene("build").With("project", projectID).
			Warn("读取站点设置失败（本次构建不注入统计代码与验证 meta）")
		return projectcontract.SiteSettings{}
	}
	return projectcontract.ParseSiteSettings(p.Settings)
}

// SiteGA4MeasurementID 读取站点 GA4 测量 ID；未配置 / 读失败返回空串（零字节注入）。
func SiteGA4MeasurementID(ctx context.Context, project projectcontract.ProjectService, projectID string) string {
	return siteSettingsSnapshot(ctx, project, projectID).GA4MeasurementID
}

// SiteSearchConsoleVerification 读取 GSC 验证 token；未配置 / 读失败返回空串（零字节注入）。
func SiteSearchConsoleVerification(ctx context.Context, project projectcontract.ProjectService, projectID string) string {
	return siteSettingsSnapshot(ctx, project, projectID).SearchConsoleVerification
}

// AnalyticsCompileOptions 站点级注入选项（GA4 统计代码 + GSC 验证 meta + 自定义代码段）。
//
// 空值不产生任何选项：builder 侧空值即零字节注入，非法值在那里被丢弃并记日志，
// 这里提前判空只是省掉一个无意义的选项。
func AnalyticsCompileOptions(ctx context.Context, project projectcontract.ProjectService, projectID string) []builder.CompileOption {
	settings := siteSettingsSnapshot(ctx, project, projectID)
	var opts []builder.CompileOption
	if strings.TrimSpace(settings.GA4MeasurementID) != "" {
		opts = append(opts, builder.WithGA4MeasurementID(settings.GA4MeasurementID))
	}
	if strings.TrimSpace(settings.SearchConsoleVerification) != "" {
		opts = append(opts, builder.WithSearchConsoleVerification(settings.SearchConsoleVerification))
	}
	// 自定义注入代码（PIPE-8）：这里只判空，形状校验的唯一出口是
	// builder.NormalizeHeadScripts / NormalizeBodyScripts（保存与注入共用同一判据），
	// 非法值在构建期被丢弃并记告警。
	if strings.TrimSpace(settings.HeadScripts) != "" {
		opts = append(opts, builder.WithHeadScripts(settings.HeadScripts))
	}
	if strings.TrimSpace(settings.BodyScripts) != "" {
		opts = append(opts, builder.WithBodyScripts(settings.BodyScripts))
	}
	return opts
}
