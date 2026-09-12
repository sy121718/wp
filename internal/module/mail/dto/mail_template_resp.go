package maildto

// mail_template_resp.go — 模板与发送相关响应。

// TemplateItem 模板条目。
type TemplateItem struct {
	// ID 供「新建活动选模板」用：活动存的是 template_id 而不是 key。
	ID          uint64   `json:"id"`
	TemplateKey string   `json:"templateKey"`
	Locale      string   `json:"locale"`
	Name        string   `json:"name"`
	Subject     string   `json:"subject"`
	BodyHTML    string   `json:"bodyHtml"`
	BodyText    string   `json:"bodyText"`
	Variables   []string `json:"variables"`
	Status      int      `json:"status"`
	UpdateTime  string   `json:"updateTime"`
}

// SendResult 发送受理结果。
//
// Suppressed=true 表示地址在抑制名单里，**没有入队**（留了一条 suppressed 日志说明原因）；
// Queued=true 表示已入队，真正的投递结果看 mail_logs。
type SendResult struct {
	LogID      uint64 `json:"logId"`
	To         string `json:"to"`
	Queued     bool   `json:"queued"`
	Suppressed bool   `json:"suppressed"`
}
