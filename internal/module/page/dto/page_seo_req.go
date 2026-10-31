package pagedto

type SEOPatrolReq struct {
	ProjectID string `form:"projectId" json:"projectId" binding:"required"`
}
