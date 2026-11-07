package productdto

import productenums "go_wp/internal/module/product/enums"

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

// Validate 是这张表单的业务校验：Score 未出现时绑定本身会失败，
// 但零分是合法输入，因此这里只判「没拿到值」。
func (r *AddRatingReq) Validate() error {
	if r.Score == nil {
		return productenums.Err(productenums.ErrInvalidParam)
	}
	return nil
}
