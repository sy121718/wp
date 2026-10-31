package productdto

import (
	"net/url"
	"strings"

	json "go_wp/pkg/jsonx"
)

type ListVariantOptionReq struct {
	ProjectID string `form:"projectId"`
	Keyword   string `form:"keyword"`
}

type PreviewVariantReq struct {
	ProductID  string                `json:"productId" form:"productId" binding:"required"`
	ProjectID  string                `json:"projectId" form:"projectId"`
	Selections []VariantSelectionReq `json:"selections" form:"selections"`

	WarehouseID string `json:"warehouseId" form:"warehouseId"`

	ExistingOptionValues []json.RawMessage `json:"existingOptionValues" form:"existingOptionValues"`
}

type SaveVariantListReq struct {
	ProductID string `json:"productId" form:"productId" binding:"required"`

	ProjectID string `json:"projectId" form:"projectId"`

	WarehouseID string `json:"warehouseId" form:"warehouseId"`

	Rows []VariantListResp `json:"rows" form:"-"`

	OperatorID string `json:"-" form:"-"`
}

func (r *SaveVariantListReq) BindForm(vals url.Values) error {
	raw := strings.TrimSpace(vals.Get("rows"))
	if raw == "" {
		return nil
	}
	if !json.Valid([]byte(raw)) {
		return &FieldValueError{Field: "rows", Kind: FieldKindJSON}
	}
	if err := json.Unmarshal([]byte(raw), &r.Rows); err != nil {
		return &FieldValueError{Field: "rows", Kind: FieldKindJSON}
	}
	return nil
}
