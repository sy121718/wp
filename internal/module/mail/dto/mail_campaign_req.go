package maildto

// mail_campaign_req.go — 群发活动相关请求。

// SaveCampaignReq 新建 / 更新群发活动。
//
// TargetTags 是**投递目标**：空数组表示全部「已订阅」联系人。
// 注意它不是「附加筛选」—— 活动只会发给 status=subscribed 的人，
// pending（未确认同意）的人一律不发，这是合规底线而不是可选项。
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

// StartCampaignReq 启动群发。
type StartCampaignReq struct {
	CampaignID uint64
	OperatorID uint64
}

// CampaignListReq 活动列表筛选。
type CampaignListReq struct {
	Status   string
	Page     int
	PageSize int
}
