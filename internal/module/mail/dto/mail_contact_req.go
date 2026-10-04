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

// SaveContactReq 新建 / 编辑联系人（ID = 0 新建，否则编辑）。
//
// 新建与编辑共用一条请求：两者填的是同一组字段，拆成两个结构只会让字段清单出现两份，
// 一处加了字段另一处漏掉，表现就是「编辑页保存后某个字段被清空」。
type SaveContactReq struct {
	// ID 联系人主键；0 = 新建（语义见 service.SaveContact）。
	ID uint64
	// Email 邮箱。唯一键是 lower(email) 表达式索引，写入前一律走 normalizeEmail。
	Email string
	Name  string
	// Tags 完整标签列表（编辑抽屉提交的是全量，不是差量）——
	// 差量增删走 TagContacts，「保存」这一条路径的语义是「保存后就是这些」。
	Tags []string
	// Source 来源。新建为空时落 manual；编辑为空时保持原值
	// （来源是事实记录，不该因为没在表单里填就被抹掉）。
	Source string
	// ConsentSource 同意来源（留痕：谁在什么时候声明过什么）。
	ConsentSource string
	// Status 同意状态。为空时新建落 pending —— 没有同意证据的人不进入可发名单。
	Status     string
	OperatorID uint64
}

// DeleteContactsReq 删除联系人（单条与批量共用）。
//
// **只删 mail_contacts 行**：抑制名单（mail_suppressions）是地址级的事实记录，
// 删联系人时一起删掉，下次导入同一个地址就会把退订者复活 —— 见 service.DeleteContacts。
type DeleteContactsReq struct {
	IDs        []uint64
	OperatorID uint64
}

// TagContactsReq 批量打标签：给这批人加上 Add 里的标签、去掉 Remove 里的标签。
//
// 增与减放在同一条请求里（而不是两个端点）：一次「打标签」在运营眼里是一个动作，
// 分成两次提交就必然出现「加成功、减失败」的半截状态。
type TagContactsReq struct {
	IDs        []uint64
	Add        []string
	Remove     []string
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
