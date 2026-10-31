package productdto

import (
	"net/url"
	"strings"

	json "go_wp/pkg/jsonx"
)

type CreateReq struct {
	ProjectID string `json:"projectId" form:"projectId"`
	Name      string `json:"name" form:"name" binding:"required"`
	Subtitle  string `json:"subtitle" form:"subtitle"`

	Description json.RawMessage `json:"description" form:"-"`
	Slug        string          `json:"slug" form:"slug"`

	SKUCode string `json:"sku" form:"sku"`

	SKUSource string `json:"skuSource" form:"skuSource"`

	WarehouseSKU string `json:"warehouseSku" form:"warehouseSku"`

	ExternalSKU    string   `json:"externalSku" form:"externalSku"`
	Unit           string   `json:"unit" form:"unit"`
	Weight         *float64 `json:"weight" form:"weight"`
	SEOTitle       string   `json:"seoTitle" form:"seoTitle"`
	SEODescription string   `json:"seoDescription" form:"seoDescription"`
	Images         []string `json:"images" form:"images"`

	ImageAlts []string `json:"imageAlts" form:"imageAlts"`

	AttributeIDs []string `json:"attributeIds" form:"attributeIds"`
	CategoryIDs  []string `json:"categoryIds" form:"categoryIds"`

	PrimaryCategoryID string   `json:"primaryCategoryId" form:"primaryCategoryId"`
	TagIDs            []string `json:"tagIds" form:"tagIds"`
	RelatedIDs        []string `json:"relatedIds" form:"relatedIds"`

	BrandID      string          `json:"brandId" form:"brandId"`
	DefaultPrice *float64        `json:"defaultPrice" form:"defaultPrice"`
	DefaultImage string          `json:"defaultImage" form:"defaultImage"`
	Metadata     json.RawMessage `json:"metadata" form:"metadata"`

	WarehouseID string `json:"warehouseId" form:"warehouseId"`

	WarehouseIDs []string `json:"warehouseIds" form:"warehouseIds"`

	Quantity *int `json:"quantity" form:"quantity"`

	TrackQuantity bool `json:"trackQuantity" form:"trackQuantity"`

	OperatorID string `json:"-" form:"-"`
}

type UpdateReq struct {
	ID string `json:"id" form:"id" binding:"required"`

	ProjectID string  `json:"projectId" form:"projectId"`
	Name      *string `json:"name" form:"name"`
	Subtitle  *string `json:"subtitle" form:"subtitle"`

	Description json.RawMessage `json:"description" form:"-"`
	Slug        *string         `json:"slug" form:"slug"`

	SKUCode        *string  `json:"sku" form:"sku"`
	Status         *string  `json:"status" form:"status"`
	Unit           *string  `json:"unit" form:"unit"`
	Weight         *float64 `json:"weight" form:"weight"`
	SEOTitle       *string  `json:"seoTitle" form:"seoTitle"`
	SEODescription *string  `json:"seoDescription" form:"seoDescription"`
	Images         []string `json:"images" form:"images"`

	ImageAlts []string `json:"imageAlts" form:"imageAlts"`

	AttributeIDs []string `json:"attributeIds" form:"attributeIds"`
	CategoryIDs  []string `json:"categoryIds" form:"categoryIds"`

	PrimaryCategoryID *string         `json:"primaryCategoryId" form:"primaryCategoryId"`
	TagIDs            []string        `json:"tagIds" form:"tagIds"`
	RelatedIDs        []string        `json:"relatedIds" form:"relatedIds"`
	BrandID           *string         `json:"brandId" form:"brandId"`
	DefaultPrice      *float64        `json:"defaultPrice" form:"defaultPrice"`
	DefaultImage      *string         `json:"defaultImage" form:"defaultImage"`
	Metadata          json.RawMessage `json:"metadata" form:"-"`

	OperatorID string `json:"-" form:"-"`
}

type GetReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	ID        string `form:"id" binding:"required"`
}

type ListReq struct {
	ProjectID string `form:"projectId"`
	Keyword   string `form:"keyword"`
	Status    string `form:"status"`
	Page      int    `form:"page"`
	Size      int    `form:"size"`
}

type DeleteReq struct {
	ID string `json:"id" form:"id" binding:"required"`

	ProjectID string `json:"projectId" form:"projectId"`

	OperatorID string `json:"-" form:"-"`
}

type CreateVariantReq struct {
	ProductID string `json:"productId" form:"productId" binding:"required"`

	ProjectID    string   `json:"projectId" form:"projectId"`
	SKUCode      string   `json:"skuCode" form:"skuCode"`
	Barcode      string   `json:"barcode" form:"barcode"`
	Price        *float64 `json:"price" form:"price"`
	ComparePrice *float64 `json:"comparePrice" form:"comparePrice"`
	CostPrice    *float64 `json:"costPrice" form:"costPrice"`
	Image        string   `json:"image" form:"image"`

	OptionValues json.RawMessage `json:"optionValues" form:"-"`
	Enabled      *bool           `json:"enabled" form:"enabled"`
	Sort         int             `json:"sort" form:"sort"`

	WarehouseID string `json:"warehouseId" form:"warehouseId"`

	Quantity *int `json:"quantity" form:"quantity"`

	OperatorID string `json:"-" form:"-"`
}

type UpdateVariantReq struct {
	ID string `json:"id" form:"id" binding:"required"`

	ProjectID    string   `json:"projectId" form:"projectId"`
	SKUCode      *string  `json:"skuCode" form:"skuCode"`
	Barcode      *string  `json:"barcode" form:"barcode"`
	Price        *float64 `json:"price" form:"price"`
	ComparePrice *float64 `json:"comparePrice" form:"comparePrice"`
	CostPrice    *float64 `json:"costPrice" form:"costPrice"`
	Image        *string  `json:"image" form:"image"`

	OptionValues json.RawMessage `json:"optionValues" form:"-"`
	Enabled      *bool           `json:"enabled" form:"enabled"`
	Sort         *int            `json:"sort" form:"sort"`

	OperatorID string `json:"-" form:"-"`
}

type DeleteVariantReq struct {
	ID string `json:"id" form:"id" binding:"required"`

	ProjectID string `json:"projectId" form:"projectId"`

	OperatorID string `json:"-" form:"-"`
}

type VariantSelectionReq struct {
	AttributeID string   `json:"attributeId" form:"attributeId"`
	ValueIDs    []string `json:"valueIds" form:"valueIds"`
}

type GenerateVariantsReq struct {
	ProductID string `json:"productId" form:"productId" binding:"required"`

	ProjectID  string                `json:"projectId" form:"projectId"`
	Selections []VariantSelectionReq `json:"selections" form:"selections"`

	WarehouseID string `json:"warehouseId" form:"warehouseId"`

	OperatorID string `json:"-" form:"-"`
}

func (r *UpdateReq) BindForm(vals url.Values) error {
	for _, f := range []struct {
		name string
		dst  *json.RawMessage
	}{
		{"description", &r.Description},
		{"metadata", &r.Metadata},
	} {
		raw := strings.TrimSpace(vals.Get(f.name))
		if raw == "" {
			continue
		}
		if !json.Valid([]byte(raw)) {
			return &FieldValueError{Field: f.name, Kind: FieldKindJSON}
		}
		*f.dst = json.RawMessage(raw)
	}
	return nil
}

func (r *CreateReq) BindForm(vals url.Values) error {
	return bindRawJSONField(vals, "description", &r.Description)
}

func (r *CreateVariantReq) BindForm(vals url.Values) error {
	return bindRawJSONField(vals, "optionValues", &r.OptionValues)
}

func (r *UpdateVariantReq) BindForm(vals url.Values) error {
	return bindRawJSONField(vals, "optionValues", &r.OptionValues)
}
