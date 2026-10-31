package productdto

type AddRatingReq struct {
	ProductID string `json:"productId" form:"productId" binding:"required"`
	ProjectID string `json:"projectId" form:"projectId"`

	Score *float64 `json:"score" form:"score" binding:"required"`

	Source string `json:"source" form:"source"`

	OperatorID string `json:"-" form:"-"`
}

type ListRatingsReq struct {
	ProductID string `json:"productId" form:"productId" binding:"required"`
	ProjectID string `json:"projectId" form:"projectId"`
}

type DeleteRatingReq struct {
	ID        string `json:"id" form:"id" binding:"required"`
	ProjectID string `json:"projectId" form:"projectId"`
}
