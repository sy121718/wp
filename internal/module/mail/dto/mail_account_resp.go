package maildto

// mail_account_resp.go — 发信账号相关响应。

// AccountItem 账号条目。
//
// **没有密码字段**：密码只以密文落库、只在发信时解密，从不回显给任何调用方。
// HasPassword 只回答「有没有设过」，够界面显示「已配置 / 未配置」。
type AccountItem struct {
	ID             uint64 `json:"id"`
	Name           string `json:"name"`
	Purpose        string `json:"purpose"`
	IsDefault      bool   `json:"isDefault"`
	FromName       string `json:"fromName"`
	FromEmail      string `json:"fromEmail"`
	ReplyTo        string `json:"replyTo"`
	Provider       string `json:"provider"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	Username       string `json:"username"`
	Encryption     string `json:"encryption"`
	RatePerHour    int    `json:"ratePerHour"`
	Status         int    `json:"status"`
	HasPassword    bool   `json:"hasPassword"`
	Incomplete     bool   `json:"incomplete"`
	LastCheckAt    string `json:"lastCheckAt"`
	LastCheckError string `json:"lastCheckError"`
}

// TestSendResp 测试发送结果。
//
// 失败不返回 error 而是返回 OK=false + 分类 + 原因：这是「配置诊断」而不是「接口出错」，
// 界面要把 SMTP 的真实反馈显示给运营（认证失败 / 连不上 / 收件人被拒）。
type TestSendResp struct {
	OK        bool   `json:"ok"`
	To        string `json:"to"`
	ErrorKind string `json:"errorKind"`
	Error     string `json:"error"`
}
