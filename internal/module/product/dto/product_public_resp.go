package productdto

// PublicVariantResp 只包含已发布商品的公开事实。未知与零库存严格区分。
type PublicVariantResp struct {
	ID        string `json:"id"`
	Known     bool   `json:"known"`
	Enabled   bool   `json:"enabled"`
	Price     *int64 `json:"price"`
	Available *int   `json:"available"`
}
