package pipeline

// compile_analytics.go — 站点级 <head> 注入的构建期装配（BIZ-8 GA4 + SEO-009 GSC 验证 meta）。
//
// 两条注入共用同一条链路、同一份 SiteSettings 快照：page 与 presentation 两条构建路径
// 都只调 AnalyticsCompileOptions 一行，读一次设置、装配多项 head 注入 —— 各写一份读取
// 逻辑迟早分叉，而分叉的表现是「后台存了、产物里没有」这种最难排查的静默失效。

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

// AnalyticsCompileOptions 站点级 <head> 注入选项（GA4 统计代码 + GSC 验证 meta）。
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
	return opts
}
