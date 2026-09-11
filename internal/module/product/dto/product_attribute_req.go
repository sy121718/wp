// product_attribute_req.go — 商品属性组入参（inbound 绑定用，issue #7）。
//
// 属性值是「稳定标识 + 排序」的数组，故单独一个 set-values 动作整体保存：
// 一个表单一次提交全部值，避免逐行接口在部分失败时留下半截状态。
package productdto

// CreateAttributeReq 新建属性组。
//
// Key 为稳定标识（工程内唯一）；留空则由 Name 派生，派生不出（纯中文名）
// 由 service 兜底生成。IsVariation 决定该组是否参与变体笛卡尔积（issue #8）。
type CreateAttributeReq struct {
	ProjectID   string              `json:"projectId"`
	Key         string              `json:"key"`
	Name        string              `json:"name" binding:"required"`
	IsVariation *bool               `json:"isVariation"`
	Sort        int                 `json:"sort"`
	Values      []AttributeValueReq `json:"values"`
}

// UpdateAttributeReq 修改属性组（只改组本身；属性值走 SetAttributeValuesReq）。
type UpdateAttributeReq struct {
	ID          string  `json:"id" binding:"required"`
	Key         *string `json:"key"`
	Name        *string `json:"name"`
	IsVariation *bool   `json:"isVariation"`
	Sort        *int    `json:"sort"`
}

// SetAttributeValuesReq 整体保存某属性组的属性值。
//
// 语义是「全量替换」：请求里没有的值被删除，已有的按 id 更新、无 id 的新建。
// 这样上移/下移/改名/增删都在一次提交里完成，不会出现「值序号错乱」。
type SetAttributeValuesReq struct {
	ID     string              `json:"id" binding:"required"`
	Values []AttributeValueReq `json:"values"`
}

// AttributeValueReq 单个属性值。
type AttributeValueReq struct {
	ID      string `json:"id"`
	Key     string `json:"key"`
	Label   string `json:"label" binding:"required"`
	Sort    int    `json:"sort"`
	Enabled *bool  `json:"enabled"`
}

// GetAttributeReq 按 ID 查询属性组。
type GetAttributeReq struct {
	ID string `form:"id" binding:"required"`
}

// ListAttributeReq 属性组列表（分页 + 可选过滤）。
//
// Variation 取值为 ""（全部）/ "1"（参与变体）/ "0"（不参与），与表单下拉一致。
type ListAttributeReq struct {
	ProjectID string `form:"projectId"`
	Keyword   string `form:"keyword"`
	Variation string `form:"variation"`
	Page      int    `form:"page"`
	Size      int    `form:"size"`
}

// DeleteAttributeReq 删除属性组。
type DeleteAttributeReq struct {
	ID string `json:"id" binding:"required"`
}
