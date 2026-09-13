package presentationservice

// presentation_site_analytics.go — 站点级统计代码（GA4）的构建期上下文注入（BIZ-8）。
//
// 自动发布实例与手工 Page 走同一条发布管线，站点级设置（SiteSettings）也就必须
// 从同一处读取：只给手工页面注入统计代码，会让商品/文章详情页（自动发布）没有统计 ——
// 而转化页往往正是这些页面。读取方式与手工路径逐条对齐（构建输入、空值零字节）。

import (
	"context"
	"strings"

	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/logger"
)

// siteGA4MeasurementID 读取站点 GA4 测量 ID；未配置 / 读不到返回空串（= 不注入）。
func (s *Service) siteGA4MeasurementID(ctx context.Context, projectID string) string {
	projectID = strings.TrimSpace(projectID)
	if s.project == nil || projectID == "" {
		return ""
	}
	p, err := s.project.Detail(ctx, &projectcontract.DetailReq{ID: projectID})
	if err != nil || p == nil {
		logger.Scene("build").With("project", projectID).
			Warn("读取站点设置失败（本次构建不注入统计代码）")
		return ""
	}
	return projectcontract.ParseSiteSettings(p.Settings).GA4MeasurementID
}
