package pagedto

type PageLangStateResp struct {
	Lang string `json:"lang"`

	IsDefault bool `json:"isDefault"`

	Excluded bool `json:"excluded"`

	Published bool `json:"published"`
}
