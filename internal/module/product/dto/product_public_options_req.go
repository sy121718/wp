package productdto

// PublicProductOptionsReq 查询指定工程内已上架商品的可选规格事实。
type PublicProductOptionsReq struct {
	ProjectID string `form:"projectId" binding:"required,uuid"`
	ProductID string `form:"productId" binding:"required,uuid"`
	Lang      string `form:"lang" binding:"max=35"`
}
