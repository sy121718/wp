package projectdto

import "encoding/json"

// CreateReq 创建站点工程请求。
type CreateReq struct {
	Name     string          `json:"name" binding:"required,max=200"`
	Settings json.RawMessage `json:"settings"`
}

// UpdateReq 更新站点工程请求。
type UpdateReq struct {
	ID       string          `json:"id" binding:"required"`
	Name     string          `json:"name" binding:"required,max=200"`
	Settings json.RawMessage `json:"settings"`
}

// DetailReq 查询站点工程请求。
type DetailReq struct {
	ID string `form:"id" json:"id" binding:"required"`
}
