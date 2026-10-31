package maildto

type CampaignItem struct {
	ID          uint64   `json:"id"`
	Name        string   `json:"name"`
	Status      string   `json:"status"`
	AccountID   uint64   `json:"accountId"`
	TemplateID  uint64   `json:"templateId"`
	TargetTags  []string `json:"targetTags"`
	Subject     string   `json:"subject"`
	TotalCount  int      `json:"totalCount"`
	SentCount   int      `json:"sentCount"`
	FailedCount int      `json:"failedCount"`
	StartedAt   string   `json:"startedAt"`
	FinishedAt  string   `json:"finishedAt"`
	CreateTime  string   `json:"createTime"`
}

type CampaignListResp struct {
	Items []CampaignItem `json:"items"`
	Total int64          `json:"total"`
}

type StartCampaignResp struct {
	CampaignID uint64 `json:"campaignId"`
	Total      int64  `json:"total"`
	Queued     bool   `json:"queued"`
}
