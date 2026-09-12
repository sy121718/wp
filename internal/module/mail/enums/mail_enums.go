package mailenums

// mail_enums.go — 邮箱模块的响应消息与业务错误（模块内统一出口）。

const (
	MsgSendSuccess       = "发送成功"
	MsgSaveSuccess       = "保存成功"
	MsgDeleteSuccess     = "删除成功"
	MsgImportSuccess     = "导入完成"
	MsgTestSent          = "测试邮件已发送"
	MsgCampaignStarted   = "活动已开始发送"
	MsgAutomationStarted = "已加入流程"
)

const (
	ErrInvalidParam             = "参数不合法"
	ErrAccountNotFound          = "发信账号不存在"
	ErrTemplateNotFound         = "邮件模板不存在"
	ErrContactNotFound          = "联系人不存在"
	ErrCampaignNotFound         = "活动不存在"
	ErrAccountDisabled          = "发信账号已停用"
	ErrAccountIncomplete        = "发信账号配置不完整（缺少主机 / 端口 / 发件人）"
	ErrSuppressed               = "该地址在抑制名单中，不允许发送"
	ErrEmailRequired            = "邮箱不能为空"
	ErrEmailInvalid             = "邮箱格式不正确"
	ErrImportEmpty              = "导入内容为空"
	ErrImportTooLarge           = "导入内容过大"
	ErrCampaignNotDraft         = "只有草稿状态的活动可以修改"
	ErrCampaignNoRecipient      = "投递目标为 0 人，无法发送"
	ErrCipherSecretMissing      = "未配置敏感数据加密密钥（config.yaml 的 app.secret），无法保存邮箱密码"
	ErrCipherUnavailable        = "邮箱密码无法解密：加密密钥可能已变更，请重新填写密码"
	ErrTemplateSyntax           = "模板语法错误"
	ErrCampaignSending          = "活动正在发送中，无法删除"
	ErrAutomationNotFound       = "自动化流程不存在"
	ErrAutomationTriggerInvalid = "触发方式不合法"
	ErrAutomationGraphInvalid   = "流程定义不合法"
	ErrAutomationRunExists      = "该联系人已在此流程中"
	ErrAutomationRunNotFound    = "自动化实例不存在"
)

// 测试邮件内容（后台「测试发送」触发，用于验证 SMTP 配置）。
const (
	TestMailSubject = "go_wp 邮件配置测试"
	TestMailHTML    = `<div style="font-family:system-ui,sans-serif;line-height:1.6"><h2>邮件配置连通性测试</h2><p>如果你看到这封邮件，说明发信账号的配置可用：</p><ul><li>SMTP 连接与认证通过</li><li>中文主题编码正常</li><li>HTML 与纯文本正文正常</li></ul><p style="color:#888;font-size:13px">本邮件由后台「测试发送」触发。</p></div>`
	TestMailText    = "邮件配置连通性测试\n\n如果你看到这封邮件，说明发信账号的配置可用：\n- SMTP 连接与认证通过\n- 中文主题编码正常\n- 纯文本正文正常\n\n本邮件由后台「测试发送」触发。"
)
