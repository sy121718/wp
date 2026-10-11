package siteurl

import (
	"context"

	projectcontract "go_wp/internal/module/project/contract"
)

// ProjectDetailReader provides only the project configuration read needed for URL derivation.
type ProjectDetailReader interface {
	Detail(ctx context.Context, req *projectcontract.DetailReq) (res *projectcontract.ProjectResp, err error)
}

// URLPatternsOf reads project URL patterns; unavailable settings use the default patterns.
func URLPatternsOf(ctx context.Context, projects ProjectDetailReader, projectID string) map[string]string {
	if projects == nil || projectID == "" {
		return nil
	}
	project, err := projects.Detail(ctx, &projectcontract.DetailReq{ID: projectID})
	if err != nil || project == nil {
		return nil
	}
	return projectcontract.ParseSiteSettings(project.Settings).URLPatterns
}

// DetailPathForProject derives a form default without changing an explicitly published URL.
func DetailPathForProject(ctx context.Context, projects ProjectDetailReader, projectID, kind, slug, id string) string {
	return DetailPath(kind, slug, id, URLPatternsOf(ctx, projects, projectID))
}
