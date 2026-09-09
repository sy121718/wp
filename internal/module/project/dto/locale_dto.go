package projectdto

// locale_dto.go — 站点语言清单请求/响应（多语言 P3，docs/06-D §14 D10）。

// LocaleItem 语言清单条目（保存用）。
type LocaleItem struct {
	Lang      string `json:"lang" binding:"required"`
	SortOrder int    `json:"sortOrder"`
	IsDefault bool   `json:"isDefault"`
	Enabled   *bool  `json:"enabled"`
}

// LocalesSaveReq 全量保存站点语言清单。
type LocalesSaveReq struct {
	ProjectID string       `json:"projectId" binding:"required"`
	Locales   []LocaleItem `json:"locales" binding:"required"`
}

// LocaleResp 语言清单条目（查询用）。
type LocaleResp struct {
	Lang      string `json:"lang"`
	SortOrder int    `json:"sortOrder"`
	IsDefault bool   `json:"isDefault"`
	Enabled   bool   `json:"enabled"`
}
