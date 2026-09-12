package maildto

// mail_template_req.go — 模板与发送相关请求。

// SaveTemplateReq 新建 / 覆盖模板（key + locale 唯一）。
type SaveTemplateReq struct {
	TemplateKey string
	Locale      string
	Name        string
	Subject     string
	BodyHTML    string
	BodyText    string
	// Variables 声明模板用到的变量名（仅用于提示与校验展示）。
	Variables  []string
	OperatorID uint64
}

// SendTemplateReq 发送一封（事务）邮件。
//
// Vars 是模板变量；缺失变量会**直接报错**（模板渲染配了 missingkey=error），
// 不会渲染成 <no value> 发出去。
type SendTemplateReq struct {
	TemplateKey string
	Locale      string
	To          string
	Vars        map[string]any
	// AccountID 为 0 时用「事务用途」的默认账号。
	AccountID uint64
	// CampaignID / ContactID 供群发链路填充（事务邮件留空）。
	CampaignID uint64
	ContactID  uint64
	OperatorID uint64
}
