package maildto

// mail_contact_req.go — 联系人相关请求。

// ImportContactsReq 导入联系人。
//
// 支持 CSV / 纯文本两种形态（自动识别）：
//
//	· CSV 首行是表头时按表头映射（email / name / tags 列名可识别）；
//	· 纯文本每行一个地址，忽略空行与 # 开头的注释行。
type ImportContactsReq struct {
	// Content 导入内容（原始字节，由 inbound 从上传或表单里取）。
	Content []byte
	// DefaultTags 导入时为每个人补上的标签。
	DefaultTags []string
	// ConsentDeclared 操作者是否声明「这批人已同意接收营销」。
	//
	// false → 导入的人一律 pending（不可发营销）；
	// true  → subscribed + 记录 ConsentSource。
	// 这个开关不是 UI 便利，是合规留痕：谁在什么时候声明过什么。
	ConsentDeclared bool
	ConsentSource   string
	// UpdateExisting 已存在的邮箱是否更新（否则跳过）。
	UpdateExisting bool
	OperatorID     uint64
}

// ContactFilterReq 联系人列表筛选。
type ContactFilterReq struct {
	Keyword  string
	Status   string
	Tags     []string
	Page     int
	PageSize int
}

// UpdateContactStatusReq 改联系人状态（后台手工订阅 / 退订）。
type UpdateContactStatusReq struct {
	ID         uint64
	Status     string
	Note       string
	OperatorID uint64
}

// PullSystemUsersReq 从系统用户拉联系人。
type PullSystemUsersReq struct {
	// OnlyEmailNotify 只拉用户侧明确勾选接收营销的人（默认 true）——
	// 未勾选的人拉进来也只是 pending，拉不拉由调用方决定。
	OnlyEmailNotify bool
	DefaultTags     []string
	OperatorID      uint64
}
