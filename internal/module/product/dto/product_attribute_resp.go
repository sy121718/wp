package productdto

import "go_wp/pkg/utils"

type AttributeValueResp struct {
	ID      string `json:"id"`
	Key     string `json:"key"`
	Label   string `json:"label"`
	Sort    int    `json:"sort"`
	Enabled bool   `json:"enabled"`
}

type AttributeResp struct {
	ID          string               `json:"id"`
	ProjectID   string               `json:"projectId"`
	Key         string               `json:"key"`
	Name        string               `json:"name"`
	IsVariation bool                 `json:"isVariation"`
	Sort        int                  `json:"sort"`
	Values      []AttributeValueResp `json:"values"`

	ValueCount          int            `json:"valueCount"`
	VariationValueCount int            `json:"variationValueCount"`
	CreatedAt           utils.JSONTime `json:"createdAt"`
	UpdatedAt           utils.JSONTime `json:"updatedAt"`
}
