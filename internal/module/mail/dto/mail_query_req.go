package maildto

// PageQuery 是后台邮件页通用分页查询：键名与页面链接一致。
type PageQuery struct {
	Page string `form:"page"`
}

// AutomationRunQuery 是排障列表的查询：分页 + 活动 + 状态。
type AutomationRunQuery struct {
	Page         string `form:"page"`
	AutomationID string `form:"automationId"`
	RunStatus    string `form:"runStatus"`
}

// CampaignReportQuery 是活动报表查询。
type CampaignReportQuery struct {
	ID   string `form:"id"`
	Page string `form:"page"`
}

// ContactListQuery 是联系人列表的筛选。
type ContactListQuery struct {
	Page    string `form:"page"`
	Keyword string `form:"keyword"`
	Status  string `form:"status"`
	Tags    string `form:"tags"`
}

// MailTemplateQuery 是模板列表与模板文案查询。
type MailTemplateQuery struct {
	Page        string `form:"page"`
	TemplateKey string `form:"templateKey"`
	Locale      string `form:"locale"`
}

// MailAccountQuery 是账户列表的分页。
type MailAccountQuery struct {
	AccountPage string `form:"account_page"`
}

// 邮件 API 的只读查询串入口：删除 / 默认 / 定时这几条写端点只从 URL query
// 取标识与上限，body 不参与。声明成 DTO 后写端点契约能给出确定协议与字段表，
// 不再逐字段手工读查询串。
type MailIDQuery struct {
	ID string `form:"id"`
}

type MailTemplateKeyQuery struct {
	TemplateKey string `form:"templateKey"`
	Locale      string `form:"locale"`
}

type MailTickQuery struct {
	Limit int `form:"limit"`
}
