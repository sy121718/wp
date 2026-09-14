package pipeline

// compile_analytics.go — 站点统计代码（GA4）构建期装配（BIZ-8，page / presentation 共用）。

import (
	"context"
	"strings"

	"go_wp/internal/builder"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/logger"
)

// SiteGA4MeasurementID 读取站点 GA4 测量 ID；未配置 / 读失败返回空串（零字节注入）。
func SiteGA4MeasurementID(ctx context.Context, project projectcontract.ProjectService, projectID string) string {
	projectID = strings.TrimSpace(projectID)
	if project == nil || projectID == "" {
		return ""
	}
	p, err := project.Detail(ctx, &projectcontract.DetailReq{ID: projectID})
	if err != nil || p == nil {
		logger.Scene("build").With("project", projectID).
			Warn("读取站点设置失败（本次构建不注入统计代码）")
		return ""
	}
	return projectcontract.ParseSiteSettings(p.Settings).GA4MeasurementID
}

// AnalyticsCompileOptions 非空 GA4 测量 ID 时注入 builder.WithGA4MeasurementID。
func AnalyticsCompileOptions(ctx context.Context, project projectcontract.ProjectService, projectID string) []builder.CompileOption {
	if id := SiteGA4MeasurementID(ctx, project, projectID); id != "" {
		return []builder.CompileOption{builder.WithGA4MeasurementID(id)}
	}
	return nil
}
