package productdto

type PublicProductOptionValueResp struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

type PublicProductOptionResp struct {
	Key    string                         `json:"key"`
	Name   string                         `json:"name"`
	Values []PublicProductOptionValueResp `json:"values"`
}

type PublicProductVariantOptionResp struct {
	ID           string            `json:"id"`
	SKU          string            `json:"sku"`
	Options      map[string]string `json:"options"`
	Price        int64             `json:"price"`
	ComparePrice *int64            `json:"comparePrice"`
}

type PublicProductOptionsResp struct {
	ID       string                           `json:"id"`
	Name     string                           `json:"name"`
	Options  []PublicProductOptionResp        `json:"options"`
	Variants []PublicProductVariantOptionResp `json:"variants"`
}
