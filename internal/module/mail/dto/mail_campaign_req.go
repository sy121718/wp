package maildto

type SaveCampaignReq struct {
	ID         uint64
	Name       string
	AccountID  uint64
	TemplateID uint64
	TargetTags []string
	Subject    string
	Variables  map[string]any
	OperatorID uint64
}

type StartCampaignReq struct {
	CampaignID uint64
	OperatorID uint64
}

type CampaignListReq struct {
	Status   string
	Page     int
	PageSize int
}
