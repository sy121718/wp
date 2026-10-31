package productdto

import (
	json "go_wp/pkg/jsonx"
	"net/url"
	"strconv"
	"strings"
)

type CreateAttributeReq struct {
	ProjectID   string              `json:"projectId" form:"projectId"`
	Key         string              `json:"key" form:"key"`
	Name        string              `json:"name" form:"name" binding:"required"`
	IsVariation *bool               `json:"isVariation" form:"isVariation"`
	Sort        int                 `json:"sort" form:"sort"`
	Values      []AttributeValueReq `json:"values" form:"-"`
}

type UpdateAttributeReq struct {
	ID          string  `json:"id" form:"id" binding:"required"`
	ProjectID   string  `json:"projectId" form:"projectId"`
	Key         *string `json:"key" form:"key"`
	Name        *string `json:"name" form:"name"`
	IsVariation *bool   `json:"isVariation" form:"isVariation"`
	Sort        *int    `json:"sort" form:"sort"`
}

type SetAttributeValuesReq struct {
	ID        string              `json:"id" form:"id" binding:"required"`
	ProjectID string              `json:"projectId" form:"projectId"`
	Values    []AttributeValueReq `json:"values" form:"-"`
}

type AttributeValueReq struct {
	ID      string `json:"id" form:"id"`
	Key     string `json:"key" form:"key"`
	Label   string `json:"label" form:"label" binding:"required"`
	Sort    int    `json:"sort" form:"sort"`
	Enabled *bool  `json:"enabled" form:"enabled"`
}

type GetAttributeReq struct {
	ID        string `form:"id" binding:"required"`
	ProjectID string `form:"projectId"`
}

type ListAttributeReq struct {
	ProjectID string `form:"projectId"`
	Keyword   string `form:"keyword"`
	Variation string `form:"variation"`
	Page      int    `form:"page"`
	Size      int    `form:"size"`
}

type DeleteAttributeReq struct {
	ID        string `json:"id" form:"id" binding:"required"`
	ProjectID string `json:"projectId" form:"projectId"`
}

func (r *CreateAttributeReq) BindForm(values url.Values) error {
	var err error
	r.IsVariation, err = attributeVariationFromForm(values)
	if err != nil {
		return err
	}
	r.Values, err = attributeValuesFromForm(values)
	return err
}

func (r *UpdateAttributeReq) BindForm(values url.Values) error {
	var err error
	r.IsVariation, err = attributeVariationFromForm(values)
	return err
}

func (r *SetAttributeValuesReq) BindForm(values url.Values) error {
	var err error
	r.Values, err = attributeValuesFromForm(values)
	return err
}

func attributeVariationFromForm(values url.Values) (*bool, error) {
	items := values["isVariation"]
	if len(items) == 0 {
		return nil, nil
	}
	value, err := strconv.ParseBool(items[len(items)-1])
	if err != nil {
		return nil, &FieldValueError{Field: "isVariation", Kind: FieldKindJSON}
	}
	return &value, nil
}

func attributeValuesFromForm(values url.Values) ([]AttributeValueReq, error) {
	raw := strings.TrimSpace(values.Get("values"))
	if raw == "" {
		return nil, nil
	}
	var rows []struct {
		ID      string `json:"id"`
		Key     string `json:"key"`
		Label   string `json:"label"`
		Sort    string `json:"sort"`
		Enabled string `json:"enabled"`
	}
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		return nil, &FieldValueError{Field: "values", Kind: FieldKindJSON}
	}
	result := make([]AttributeValueReq, 0, len(rows))
	for _, row := range rows {
		value := AttributeValueReq{ID: row.ID, Key: row.Key, Label: row.Label}
		if row.Sort != "" {
			parsed, err := strconv.Atoi(row.Sort)
			if err != nil {
				return nil, &FieldValueError{Field: "values.sort", Kind: FieldKindNumber}
			}
			value.Sort = parsed
		}
		if row.Enabled != "" {
			parsed, err := strconv.ParseBool(row.Enabled)
			if err != nil {
				return nil, &FieldValueError{Field: "values.enabled", Kind: FieldKindJSON}
			}
			value.Enabled = &parsed
		}
		result = append(result, value)
	}
	return result, nil
}
