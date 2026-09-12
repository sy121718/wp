package mailenums

// mail_enums.go — 邮箱模块的响应消息与业务错误（模块内统一出口）。

const (
	MsgSendSuccess   = "发送成功"
	MsgSaveSuccess   = "保存成功"
	MsgDeleteSuccess = "删除成功"
	MsgImportSuccess = "导入完成"
	MsgTestSent      = "测试邮件已发送"
)

const (
	ErrInvalidParam        = "参数不合法"
	ErrAccountNotFound     = "发信账号不存在"
	ErrTemplateNotFound    = "邮件模板不存在"
	ErrContactNotFound     = "联系人不存在"
	ErrCampaignNotFound    = "活动不存在"
	ErrAccountDisabled     = "发信账号已停用"
	ErrAccountIncomplete   = "发信账号配置不完整（缺少主机 / 端口 / 发件人）"
	ErrSuppressed          = "该地址在抑制名单中，不允许发送"
	ErrEmailRequired       = "邮箱不能为空"
	ErrEmailInvalid        = "邮箱格式不正确"
	ErrImportEmpty         = "导入内容为空"
	ErrImportTooLarge      = "导入内容过大"
	ErrCampaignNotDraft    = "只有草稿状态的活动可以修改"
	ErrCampaignNoRecipient = "投递目标为 0 人，无法发送"
)
