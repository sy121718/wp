package maildto

type SaveCampaignReq struct {
	ID         uint64         `form:"id" json:"id"`
	Name       string         `form:"name" json:"name"`
	AccountID  uint64         `form:"accountId" json:"accountId"`
	TemplateID uint64         `form:"templateId" json:"templateId"`
	TargetTags []string       `form:"targetTags" json:"targetTags"`
	Subject    string         `form:"subject" json:"subject"`
	Variables  map[string]any `form:"-" json:"variables"`
	OperatorID uint64         `form:"-" json:"-"`
}

type StartCampaignReq struct {
	CampaignID uint64 `form:"campaignId" json:"campaignId"`
	OperatorID uint64 `form:"-" json:"-"`
}

type CampaignListReq struct {
	Status   string
	Page     int
	PageSize int
}
