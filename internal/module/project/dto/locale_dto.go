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
	// ConfirmRetire 确认「禁用语言会下掉该语言的已激活路由」（审计 I18N-017）。
	// 未确认时 SaveLocales 拒绝执行并把受影响路径数回报给运营 —— 这是一次不可逆的
	// 站点可见变更，不该在运营不知道代价的情况下发生。
	ConfirmRetire bool `json:"confirmRetire"`
}

// LocaleResp 语言清单条目（查询用）。
type LocaleResp struct {
	Lang      string `json:"lang"`
	SortOrder int    `json:"sortOrder"`
	IsDefault bool   `json:"isDefault"`
	Enabled   bool   `json:"enabled"`
}
