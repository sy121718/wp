package dashboardhttp

// site_url.go — 后台各处派生「详情页路径」时的统一入口。
//
// 为什么要有这个文件：派生逻辑此前散在各页面 handler 里（商品侧 productDetailPath 写死
// /products/、文章侧写死 /blog/），既不能按站点改，也没法一眼看出"这个站的详情页 URL 长什么样"。
// 现在规则在 internal/siteurl（纯函数），站点配置在 SiteSettings.urlPatterns，
// 两处硬编码都收口到这里。

import (
	"context"

	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/siteurl"
)

// siteURLPatternsOf 读某工程的 URL 模式配置。
//
// 读不到（工程不存在 / settings 为空 / 字段缺失）一律返回 nil，由 siteurl 回落到默认模式 ——
// 派生路径是"帮用户把表单填好"，任何一步失败都不该让页面报错。
func siteURLPatternsOf(ctx context.Context, projects projectcontract.ProjectService, projectID string) map[string]string {
	if projects == nil || projectID == "" {
		return nil
	}
	project, err := projects.Detail(ctx, &projectcontract.DetailReq{ID: projectID})
	if err != nil || project == nil {
		return nil
	}
	return projectcontract.ParseSiteSettings(project.Settings).URLPatterns
}

// siteDetailPath 派生某实体在某工程下的详情页路径（无模式可依时返回空串）。
func siteDetailPath(ctx context.Context, projects projectcontract.ProjectService,
	projectID, kind, slug, id string) string {
	return siteurl.DetailPath(kind, slug, id, siteURLPatternsOf(ctx, projects, projectID))
}
