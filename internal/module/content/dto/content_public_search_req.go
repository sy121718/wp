package contentdto

type PublicSearchReq struct {
	ProjectID string `form:"projectId" binding:"required,uuid"`
	Lang      string `form:"lang" binding:"max=35"`
	Query     string `form:"q" binding:"max=64"`
	Limit     int    `form:"limit" binding:"min=0,max=20"`
}
