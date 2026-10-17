package pagedto

// SEOPatrolReq 站点 SEO 合规巡检请求（按工程）。
type SEOPatrolReq struct {
	ProjectID string `form:"projectId" json:"projectId" binding:"required"`
}
