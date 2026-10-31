package productdto

type ProductPublishRecordPageReq struct {
	ProjectID string `form:"project"`

	Keyword string `form:"keyword"`

	Status string `form:"status"`
	Page   int    `form:"page"`
}
