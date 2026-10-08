package maildto

// mail_report_resp.go — 活动报表（issue #38 P1）。

// LinkStat 链接点击排行的一行。
type LinkStat struct {
	URL      string `json:"url"`
	Total    int64  `json:"total"`
	Contacts int64  `json:"contacts"`
}

// RecipientItem 收件人的投递与互动状态。
type RecipientItem struct {
	ContactID uint64 `json:"contactId"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	SentAt    string `json:"sentAt"`
	ErrorKind string `json:"errorKind"`
	Opened    int64  `json:"opened"`
	Clicked   int64  `json:"clicked"`

	// StatusTone 投递状态徽标分档（ok / warn / danger / mute / info），**只给服务端模板用**。
	// json:"-" 是刻意的：分档是「怎么显示」不是数据，接口契约（含保留的 ReportJSON）
	// 不该因为这个字段变样 —— 同一份数据给外部客户端时，人家自己决定怎么上色。
	StatusTone string `json:"-"`
}

// CampaignReport 活动报表。
//
// 打开 / 点击 / 退订都是**去重人数**（同一人多次只算一个）；
// OpenRate 是**估算值** —— 多数邮件客户端默认不加载图片（漏报），
// 而 Apple Mail 的隐私保护会代理预取所有图片（虚高）。界面上必须如实这么标注，
// 别让运营拿它做硬判断。
type CampaignReport struct {
	Campaign CampaignItem `json:"campaign"`

	Sent   int `json:"sent"`
	Failed int `json:"failed"`
	Target int `json:"target"`

	// 去重人数
	Opened       int64 `json:"opened"`
	Clicked      int64 `json:"clicked"`
	Unsubscribed int64 `json:"unsubscribed"`
	Complained   int64 `json:"complained"`
	Bounced      int64 `json:"bounced"`

	// 事件次数（用于「人均打开次数」这类指标）
	OpenEvents  int64 `json:"openEvents"`
	ClickEvents int64 `json:"clickEvents"`

	// OpenRate 打开率（百分比，保留一位小数）。**估算**，见类型说明。
	OpenRate  float64 `json:"openRate"`
	ClickRate float64 `json:"clickRate"`

	Links      []LinkStat      `json:"links"`
	Recipients []RecipientItem `json:"recipients"`
	Total      int64           `json:"total"`
}
