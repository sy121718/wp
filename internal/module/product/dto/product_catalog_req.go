package productdto

// ProductProjectReq 绑定工程范围，不保存页面或表单回显状态。
type ProductProjectReq struct {
	ProjectID string `form:"project" json:"projectId"`
}
