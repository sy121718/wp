package projectdto

// LocaleItem 语言清单条目（保存用）。
type LocaleItem struct {
	Lang      string `json:"lang" binding:"required"`
	SortOrder int    `json:"sortOrder"`
	IsDefault bool   `json:"isDefault"`
	Enabled   *bool  `json:"enabled"`
}

// LocaleResp 语言清单条目（查询用）。
type LocaleResp struct {
	Lang      string `json:"lang"`
	SortOrder int    `json:"sortOrder"`
	IsDefault bool   `json:"isDefault"`
	Enabled   bool   `json:"enabled"`
}
