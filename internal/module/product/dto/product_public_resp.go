package productdto

type PublicVariantResp struct {
	ID        string `json:"id"`
	Known     bool   `json:"known"`
	Enabled   bool   `json:"enabled"`
	Price     *int64 `json:"price"`
	Available *int   `json:"available"`
}
