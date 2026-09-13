package pageservice

// page_site_analytics.go — 站点级统计代码（GA4）的构建期上下文注入（BIZ-8）。
//
// 站点级设置的真源是 project 模块的 SiteSettings（projects.settings 这一列 JSON）。
// 构建期把它读成**本次编译输入的一部分**（与导航、系统页面槽位同一路子）：
// builder 侧只认传进去的值，不读进程级全局变量 —— 同一 Page Document + BuildContext
// → 同一字节（确定性构建不变量）。
//
// 预览与发布都经 compileDocument，两条路径因此自动同源：预览里看到的 head
// 与发布会产出的字节一致（预览字节 == 发布字节）。

import (
	"context"
	"strings"

	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/logger"
)

// siteGA4MeasurementID 读取站点 GA4 测量 ID。
//
// 三种情况都返回空串（= 不注入，产物零字节）：工程未指定、工程契约未注入、
// 设置里没配。读取失败额外记日志但不阻断构建 —— 统计代码是增强能力，
// 缺了它站点照常发布与访问（把统计故障升级成发布失败是本末倒置）。
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
