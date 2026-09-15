package presentationservice

import (
	"context"
	"os"
	"strings"

	presentationmodel "go_wp/internal/module/presentation/model"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/seo"
)

func presentationSiteBaseURL() string {
	return strings.TrimSpace(os.Getenv("WP_SITE_BASE_URL"))
}

func (s *Service) notifyIndexNow(ctx context.Context, inst *presentationmodel.InstanceEntity, paths ...string) {
	if s.project == nil || inst == nil || len(paths) == 0 {
		return
	}
	project, err := s.project.Detail(ctx, &projectcontract.DetailReq{ID: inst.ProjectID})
	if err != nil || project == nil {
		return
	}
	settings := projectcontract.ParseSiteSettings(project.Settings)
	seo.NotifyIndexNowAsync(presentationSiteBaseURL(), settings.IndexNowKey, paths)
}
