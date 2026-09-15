package mailcontract

// mail_service.go — 邮箱模块对外契约。
//
// 只放本模块对外暴露的能力，不定义外部依赖接口。
// 账号的密码永不经过这里（响应里没有密码字段），所以契约本身不涉及明文。

import (
	"context"

	maildto "go_wp/internal/module/mail/dto"
)

// MailService 邮箱域能力。
type MailService interface {
	// ---- 发信账号 ----
	CreateAccount(ctx context.Context, req *maildto.SaveAccountReq) (*maildto.AccountItem, error)
	UpdateAccount(ctx context.Context, req *maildto.SaveAccountReq) (*maildto.AccountItem, error)
	ListAccounts(ctx context.Context, purpose string) ([]*maildto.AccountItem, error)
	DeleteAccount(ctx context.Context, id uint64) error
	SetDefaultAccount(ctx context.Context, id uint64) error
	TestSend(ctx context.Context, req *maildto.TestSendReq) (*maildto.TestSendResp, error)

	// ---- 邮件模板 ----
	UpsertTemplate(ctx context.Context, req *maildto.SaveTemplateReq) (*maildto.TemplateItem, error)
	ListTemplates(ctx context.Context, key string) ([]*maildto.TemplateItem, error)
	DeleteTemplate(ctx context.Context, key, locale string) error

	// ---- 联系人 ----
	ImportContacts(ctx context.Context, req *maildto.ImportContactsReq) (*maildto.ImportContactsResp, error)
	ListContacts(ctx context.Context, req *maildto.ContactFilterReq) (*maildto.ContactListResp, error)
	UpdateContactStatus(ctx context.Context, req *maildto.UpdateContactStatusReq) error

	// ---- 群发活动 ----
	SaveCampaign(ctx context.Context, req *maildto.SaveCampaignReq) (*maildto.CampaignItem, error)
	ListCampaigns(ctx context.Context, req *maildto.CampaignListReq) (*maildto.CampaignListResp, error)
	GetCampaign(ctx context.Context, id uint64) (*maildto.CampaignItem, error)
	DeleteCampaign(ctx context.Context, id uint64) error
	StartCampaign(ctx context.Context, req *maildto.StartCampaignReq) (*maildto.StartCampaignResp, error)

	// ---- 报表（#38 P1）----
	CampaignReport(ctx context.Context, campaignID uint64, page, pageSize int) (*maildto.CampaignReport, error)

	// ---- 自动化（#38 P3）----
	SaveAutomation(ctx context.Context, req *maildto.SaveAutomationReq) (*maildto.AutomationItem, error)
	ListAutomations(ctx context.Context, req *maildto.AutomationListReq) (*maildto.AutomationListResp, error)
	GetAutomation(ctx context.Context, id uint64) (*maildto.AutomationItem, error)
	SetAutomationStatus(ctx context.Context, req *maildto.SetAutomationStatusReq) error
	// SaveAutomationLayout 保存画布位置（P4）。位置不是流程语义，**不推进版本号**。
	SaveAutomationLayout(ctx context.Context, req *maildto.SaveAutomationLayoutReq) error
	DeleteAutomation(ctx context.Context, id uint64) error
	// StartRun 启动实例；返回是否新启动（false = 该联系人已在此流程中）。
	StartRun(ctx context.Context, automationID, contactID uint64, triggerEvent string) (bool, error)
	// RunAutomation 推进实例（由队列任务调用）。
	RunAutomation(ctx context.Context, runID uint64) error
	// ListAutomationRuns / AutomationRunDetail 排障视图（目标 ⑥）。
	ListAutomationRuns(ctx context.Context, req *maildto.AutomationRunListReq) (*maildto.AutomationRunListResp, error)
	AutomationRunDetail(ctx context.Context, runID uint64) (*maildto.AutomationRunDetailResp, error)
	// EnqueueDueRuns 延时调度兜底：把到点的等待实例重新投递。
	EnqueueDueRuns(ctx context.Context, limit int) (int, error)

	// ---- 事务发送 ----
	SendTemplate(ctx context.Context, req *maildto.SendTemplateReq) (*maildto.SendResult, error)

	// ---- 追踪（#38 P1）----
	SignTrackToken(p maildto.TrackPayload) (string, error)
	ParseTrackToken(token string) (maildto.TrackPayload, error)
	InjectTracking(html string, p maildto.TrackPayload) (string, error)
	RecordTrackEvent(ctx context.Context, p maildto.TrackPayload, eventType, ip, ua string)
	UnsubscribeByToken(ctx context.Context, token, ip, ua string) (string, error)
}

// TransactionalSender 事务邮件发送端口 —— 按模板 key + 语言发**一封**事务邮件。
//
// 单独一个接口而不是并进 MailService：事务链路的调用方（注册验证 / 密码重置 / 访客开号）
// 一条发信账号、模板管理、群发、报表能力都用不上，拿到整个 MailService 只会扩大误用面。
// 与 TrackingService 同一手法 —— 越权防护靠**接口形状**，不靠调用方自觉。
//
// 入参用本契约自有类型（SendInput / SendOutcome）而**不借用 dto**：
// dto 服务 HTTP 层绑定，形状随绑定需求变；对外契约的形状只随语义变。
// 事务语义写死在类型里 —— 恒用事务用途的默认账号，不带群发活动 / 联系人 / 操作人，
// 那三个字段属于后台群发链路，事务调用方没有任何场景该填。
type TransactionalSender interface {
	// SendTransactional 按模板 key + 语言渲染并投递一封事务邮件。
	//
	// 模板不存在 / 变量缺失 / 收件人为空一律返回 error；
	// 收件人在抑制名单内不算失败 —— 返回 SendOutcome{Suppressed: true} 且 err 为 nil。
	SendTransactional(ctx context.Context, in *SendInput) (*SendOutcome, error)
}

// SendInput 事务邮件的发送请求（契约自有形状，非 dto 别名）。
//
// 刻意只有四个字段：事务链路需要的语义就是「哪套模板、什么语言、发给谁、变量是什么」。
// AccountID / CampaignID / ContactID / OperatorID 一律不暴露 ——
// 前者的缺省（事务默认账号）是模块内部决定，后三者属群发链路。
type SendInput struct {
	// TemplateKey 模板键。
	TemplateKey string
	// Locale 收件语言；为空时按模块既有的语言回退口径处理。
	Locale string
	// To 收件地址。
	To string
	// Vars 模板变量。
	Vars map[string]any
}

// SendOutcome 事务邮件的投递结论 —— 只暴露调用方能判定的两件事。
//
// 不发日志主键：那是 mail 模块内部的排障标识，调用方凭它做不了任何决定，
// 暴露出去只会诱使调用方把它当业务引用存下来（日志轮转后就失效了）。
type SendOutcome struct {
	// Queued 已受理入队。
	Queued bool
	// Suppressed 因退订 / 抑制名单未实际投递。
	Suppressed bool
}

// TrackingService 追踪端点需要的最小能力。
//
// 单独一个接口而不并进 MailService：公开路由不该拿到账号 / 模板 / 群发这些后台能力，
// 越权防护靠**接口形状**，而不是靠调用方自觉。
//
// 注意 UnsubscribeByToken 的语义：退订是反垃圾邮件法要求的能力，所以它不需要登录态与 csrf，
// 安全性由「token 只能由我们签发」保证，且操作幂等。
type TrackingService interface {
	ParseTrackToken(token string) (maildto.TrackPayload, error)
	InjectTracking(html string, p maildto.TrackPayload) (string, error)
	RecordTrackEvent(ctx context.Context, p maildto.TrackPayload, eventType, ip, ua string)
	UnsubscribeByToken(ctx context.Context, token, ip, ua string) (string, error)
}
