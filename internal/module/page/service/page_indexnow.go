package pageservice

import (
	"context"
	"strings"

	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/seo"
)

// notifyIndexNow 发布成功后异步 ping IndexNow（SEO-022）；失败不影响发布。
func (s *Service) notifyIndexNow(ctx context.Context, projectID string, paths ...string) {
	if s.project == nil || strings.TrimSpace(projectID) == "" || len(paths) == 0 {
		return
	}
	project, err := s.project.Detail(ctx, &projectcontract.DetailReq{ID: projectID})
	if err != nil || project == nil {
		return
	}
	settings := projectcontract.ParseSiteSettings(project.Settings)
	seo.NotifyIndexNowAsync(siteBaseURL(), settings.IndexNowKey, paths)
}
