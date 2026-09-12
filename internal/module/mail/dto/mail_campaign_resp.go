package maildto

// mail_campaign_resp.go — 群发活动相关响应。

// CampaignItem 活动条目。
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

// CampaignListResp 活动列表。
type CampaignListResp struct {
	Items []CampaignItem `json:"items"`
	Total int64          `json:"total"`
}

// StartCampaignResp 启动结果。
//
// Total 是启动时统计到的目标人数（快照）。真正的投递在后台分批展开，
// 所以这里只表示「已受理」，进度看 totalCount / sentCount。
type StartCampaignResp struct {
	CampaignID uint64 `json:"campaignId"`
	Total      int64  `json:"total"`
	Queued     bool   `json:"queued"`
}
