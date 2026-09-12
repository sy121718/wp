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
