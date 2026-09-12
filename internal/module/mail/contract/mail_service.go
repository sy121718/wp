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

	// ---- 事务发送 ----
	SendTemplate(ctx context.Context, req *maildto.SendTemplateReq) (*maildto.SendResult, error)
}
