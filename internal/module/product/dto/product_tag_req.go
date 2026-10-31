package productdto

import (
	"net/url"
	"strings"

	json "go_wp/pkg/jsonx"
)

type CreateTagReq struct {
	ProjectID string `json:"projectId" form:"projectId"`
	Name      string `json:"name" form:"name" binding:"required"`
	Slug      string `json:"slug" form:"slug"`

	Kind string `json:"kind" form:"kind"`

	RuleType string `json:"ruleType" form:"ruleType"`

	RuleParams json.RawMessage `json:"ruleParams" form:"-"`
	Sort       int             `json:"sort" form:"sort"`
}

func (r *CreateTagReq) BindForm(vals url.Values) error {
	return bindRawJSONField(vals, "ruleParams", &r.RuleParams)
}

type UpdateTagReq struct {
	ID         string          `json:"id" form:"id" binding:"required"`
	ProjectID  string          `json:"projectId" form:"projectId"`
	Name       *string         `json:"name" form:"name"`
	Slug       *string         `json:"slug" form:"slug"`
	Kind       *string         `json:"kind" form:"kind"`
	RuleType   *string         `json:"ruleType" form:"ruleType"`
	RuleParams json.RawMessage `json:"ruleParams" form:"-"`
	Sort       *int            `json:"sort" form:"sort"`
}

func (r *UpdateTagReq) BindForm(vals url.Values) error {
	return bindRawJSONField(vals, "ruleParams", &r.RuleParams)
}

type GetTagReq struct {
	ID        string `form:"id" binding:"required"`
	ProjectID string `form:"projectId"`
}

type ListTagReq struct {
	ProjectID string `form:"projectId"`
	Kind      string `form:"kind"`
	Keyword   string `form:"keyword"`

	Page int `form:"page"`

	Size int `form:"size"`
}

type ListTagProductsReq struct {
	TagID     string `form:"id" binding:"required"`
	ProjectID string `form:"projectId"`
	Page      int    `form:"page"`
	Size      int    `form:"size"`
}

type DeleteTagReq struct {
	ID        string `json:"id" form:"id" binding:"required"`
	ProjectID string `json:"projectId" form:"projectId"`
}

type RecalcTagsReq struct {
	ProjectID string `json:"projectId" form:"projectId"`
	TagID     string `json:"tagId" form:"tagId"`
}

func bindRawJSONField(vals url.Values, name string, dst *json.RawMessage) error {
	raw := strings.TrimSpace(vals.Get(name))
	if raw == "" {
		return nil
	}
	if !json.Valid([]byte(raw)) {
		return &FieldValueError{Field: name, Kind: FieldKindJSON}
	}
	*dst = json.RawMessage(raw)
	return nil
}
