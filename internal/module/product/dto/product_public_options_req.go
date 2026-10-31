package productdto

type PublicProductOptionsReq struct {
	ProjectID string `form:"projectId" binding:"required,uuid"`
	ProductID string `form:"productId" binding:"required,uuid"`
	Lang      string `form:"lang" binding:"max=35"`
}
