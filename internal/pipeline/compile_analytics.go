package pipeline

import (
	"context"
	"strings"

	"go_wp/pkg/builder"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/logger"
)

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

func SiteGA4MeasurementID(ctx context.Context, project projectcontract.ProjectService, projectID string) string {
	return siteSettingsSnapshot(ctx, project, projectID).GA4MeasurementID
}

func SiteSearchConsoleVerification(ctx context.Context, project projectcontract.ProjectService, projectID string) string {
	return siteSettingsSnapshot(ctx, project, projectID).SearchConsoleVerification
}

func AnalyticsCompileOptions(ctx context.Context, project projectcontract.ProjectService, projectID string) []builder.CompileOption {
	settings := siteSettingsSnapshot(ctx, project, projectID)
	var opts []builder.CompileOption
	if strings.TrimSpace(settings.GA4MeasurementID) != "" {
		opts = append(opts, builder.WithGA4MeasurementID(settings.GA4MeasurementID))
	}
	if strings.TrimSpace(settings.SearchConsoleVerification) != "" {
		opts = append(opts, builder.WithSearchConsoleVerification(settings.SearchConsoleVerification))
	}

	if strings.TrimSpace(settings.HeadScripts) != "" {
		opts = append(opts, builder.WithHeadScripts(settings.HeadScripts))
	}
	if strings.TrimSpace(settings.BodyScripts) != "" {
		opts = append(opts, builder.WithBodyScripts(settings.BodyScripts))
	}
	return opts
}
