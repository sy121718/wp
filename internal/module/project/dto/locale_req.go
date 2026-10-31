package projectdto

type LocalesSaveReq struct {
	ProjectID string       `json:"projectId" form:"projectId" binding:"required"`
	Locales   []LocaleItem `json:"locales" form:"locales" binding:"required"`

	ConfirmRetire bool `json:"confirmRetire" form:"confirmRetire"`
}
