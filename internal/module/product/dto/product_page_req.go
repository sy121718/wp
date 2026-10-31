package productdto

import (
	"net/url"
	"sort"
	"strings"

	json "go_wp/pkg/jsonx"
)

const variantSelectionPrefix = "attr:"

func SelectionsFromForm(vals url.Values) []VariantSelectionReq {
	keys := make([]string, 0, len(vals))
	for key := range vals {
		if strings.HasPrefix(key, variantSelectionPrefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	out := make([]VariantSelectionReq, 0, len(keys))
	for _, key := range keys {
		attrID := strings.TrimPrefix(key, variantSelectionPrefix)
		if attrID == "" {
			continue
		}
		out = append(out, VariantSelectionReq{AttributeID: attrID, ValueIDs: vals[key]})
	}
	return out
}

type ScorePanelReq struct {
	ProjectID string `form:"projectId" json:"projectId" binding:"required"`
	ID        string `form:"id" json:"id" binding:"required"`
}

type VariantPreviewFormReq struct {
	ProductID   string `json:"productId" form:"productId"`
	ProjectID   string `json:"projectId" form:"projectId"`
	WarehouseID string `json:"warehouseId" form:"warehouseId"`

	OptionValues []string `json:"optionValues" form:"optionValues"`

	Selections []VariantSelectionReq `json:"selections" form:"-"`
}

func (r *VariantPreviewFormReq) BindForm(vals url.Values) error {
	r.Selections = SelectionsFromForm(vals)
	return nil
}

type DetailTemplatePreviewReq struct {
	ProjectID  string `form:"projectId"`
	ProductID  string `form:"productId"`
	TemplateID string `form:"templateId"`
}

type ProductNewPageReq struct {
	ProjectID string `form:"project"`
}

type ProductListPageReq struct {
	ProjectID string `form:"project"`
	Keyword   string `form:"keyword"`
	Status    string `form:"status"`
	Page      int    `form:"page"`
}

type ProductDetailPageReq struct {
	ProjectID string `form:"project"`

	ProductID string `form:"product"`
}

type ProductEditPageReq struct {
	ProjectID string `form:"project"`

	ProductID string `form:"product"`
}

type ProductDetailTemplatePageReq struct {
	ProjectID string `form:"project"`

	ProductID string `form:"product" binding:"required"`
}

type ProductTranslationsPageReq struct {
	ProjectID string `form:"project"`
	ProductID string `form:"product"`
	Lang      string `form:"lang"`
}

type SaveProductTranslationsReq struct {
	ProjectID string   `json:"projectId" form:"projectId"`
	ProductID string   `json:"productId" form:"productId" binding:"required"`
	Lang      string   `json:"lang" form:"lang"`
	Targets   []string `json:"rowTarget" form:"rowTarget"`
	Contexts  []string `json:"rowContext" form:"rowContext"`
	Hashes    []string `json:"rowHash" form:"rowHash"`
}

type ProductPreviewFrameReq struct {
	ProjectID string `form:"project"`
	ProductID string `form:"productId"`
}

func (r *VariantPreviewFormReq) ServiceReq() *PreviewVariantReq {
	req := &PreviewVariantReq{
		ProductID:   r.ProductID,
		ProjectID:   r.ProjectID,
		WarehouseID: r.WarehouseID,
		Selections:  r.Selections,
	}
	for _, raw := range r.OptionValues {
		if trimmed := strings.TrimSpace(raw); trimmed != "" {
			req.ExistingOptionValues = append(req.ExistingOptionValues, json.RawMessage(trimmed))
		}
	}
	return req
}
