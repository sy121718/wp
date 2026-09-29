package maildto

// mail_account_req.go — 发信账号相关请求。

// SaveAccountReq 新建 / 更新发信账号。
//
// 更新时 Password 留空表示**不改密码** —— 编辑页面无法回显密码（我们从不回显），
// 若把空串当成新密码保存，运营每次改名字都会把 SMTP 密码清掉。
type SaveAccountReq struct {
	ID          uint64
	Name        string
	Purpose     string // transactional / marketing（分类标记，不是使用限制）
	FromName    string
	FromEmail   string
	ReplyTo     string
	Provider    string
	Host        string
	Port        int
	Username    string
	Password    string
	Encryption  string
	RatePerHour int
	OperatorID  uint64
}

// TestSendReq 用指定账号发测试邮件。
//
// Lang 是**请求语言**（response.RequestLanguage 的取值）：测试邮件的主题与正文
// 按它取词 —— 英文界面上点「测试发送」收到的该是英文邮件，而不是恒中文。
type TestSendReq struct {
	AccountID  uint64
	ToEmail    string
	OperatorID uint64
	Lang       string
}
