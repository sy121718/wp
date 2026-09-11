// product_attribute_resp.go — 商品属性组出参（service 返回，issue #7）。
package productdto

// AttributeValueResp 单个属性值。
//
// ID 与 Key 都是稳定标识：ID 供关联/引用，Key 供构建期渲染与规格组合（issue #8）
// 使用，二者一经创建就保持不变，改名（Label）不影响它们。
type AttributeValueResp struct {
	ID      string `json:"id"`
	Key     string `json:"key"`
	Label   string `json:"label"`
	Sort    int    `json:"sort"`
	Enabled bool   `json:"enabled"`
}

// AttributeResp 属性组（含全部属性值）。
type AttributeResp struct {
	ID          string               `json:"id"`
	ProjectID   string               `json:"projectId"`
	Key         string               `json:"key"`
	Name        string               `json:"name"`
	IsVariation bool                 `json:"isVariation"`
	Sort        int                  `json:"sort"`
	Values      []AttributeValueResp `json:"values"`
	// ValueCount / VariationValueCount 是列表展示用的只读派生值。
	ValueCount          int    `json:"valueCount"`
	VariationValueCount int    `json:"variationValueCount"`
	CreatedAt           string `json:"createdAt"`
	UpdatedAt           string `json:"updatedAt"`
}
