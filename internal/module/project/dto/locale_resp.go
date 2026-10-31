package projectdto

type LocaleItem struct {
	Lang      string `json:"lang" binding:"required"`
	SortOrder int    `json:"sortOrder"`
	IsDefault bool   `json:"isDefault"`
	Enabled   *bool  `json:"enabled"`
}

type LocaleResp struct {
	Lang      string `json:"lang"`
	SortOrder int    `json:"sortOrder"`
	IsDefault bool   `json:"isDefault"`
	Enabled   bool   `json:"enabled"`
}
