package productdto

import (
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/utils"
)

type TagResp struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`

	Kind string `json:"kind"`

	RuleType string `json:"ruleType"`

	RuleParams json.RawMessage `json:"ruleParams"`

	RuleValues RuleValuesResp `json:"ruleValues"`

	RecalcAt utils.JSONTime `json:"recalcAt"`

	ProductCount int `json:"productCount"`
	Sort         int `json:"sort"`

	Products  []*TagProductResp `json:"products,omitempty"`
	CreatedAt utils.JSONTime    `json:"createdAt"`
	UpdatedAt utils.JSONTime    `json:"updatedAt"`
}

type TagProductResp struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Slug   string `json:"slug"`
	Status string `json:"status"`
}

type TagProductsListResp struct {
	TagID   string `json:"tagId"`
	TagName string `json:"tagName"`

	Total int `json:"total"`

	Page     int `json:"page"`
	PageSize int `json:"pageSize"`

	Items []*TagProductResp `json:"items"`
}

type TagRuleTypeResp struct {
	Type string `json:"type"`

	ParamKeys []string `json:"paramKeys"`
}

type RecalcTagsResp struct {
	Recalculated int `json:"recalculated"`

	Products int `json:"products"`

	Tags []*TagResp `json:"tags"`
}
