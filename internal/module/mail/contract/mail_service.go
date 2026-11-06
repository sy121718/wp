package mailcontract

import (
	"context"

	maildto "go_wp/internal/module/mail/dto"
)

type MailService interface {
	CreateAccount(ctx context.Context, req *maildto.SaveAccountReq) (*maildto.AccountItem, error)
	UpdateAccount(ctx context.Context, req *maildto.SaveAccountReq) (*maildto.AccountItem, error)
	ListAccounts(ctx context.Context, purpose string) ([]*maildto.AccountItem, error)
	ListAccountsPage(ctx context.Context, purpose string, page, limit int) ([]*maildto.AccountItem, int64, int, error)
	DeleteAccount(ctx context.Context, id uint64) error
	DeleteAccounts(ctx context.Context, req *maildto.DeleteIDsReq) (deleted int64, err error)
	SetDefaultAccount(ctx context.Context, id uint64) error
	TestSend(ctx context.Context, req *maildto.TestSendReq) (*maildto.TestSendResp, error)

	UpsertTemplate(ctx context.Context, req *maildto.SaveTemplateReq) (*maildto.TemplateItem, error)

	GetTemplate(ctx context.Context, key, locale string) (*maildto.TemplateItem, error)
	ListTemplates(ctx context.Context, key string) ([]*maildto.TemplateItem, error)
	ListTemplatesPage(ctx context.Context, key string, page, limit int) ([]*maildto.TemplateItem, int64, int, error)
	DeleteTemplate(ctx context.Context, key, locale string) error
	DeleteTemplates(ctx context.Context, req *maildto.DeleteIDsReq) (deleted int64, err error)

	ImportContacts(ctx context.Context, req *maildto.ImportContactsReq) (*maildto.ImportContactsResp, error)
	ListContacts(ctx context.Context, req *maildto.ContactFilterReq) (*maildto.ContactListResp, error)
	UpdateContactStatus(ctx context.Context, req *maildto.UpdateContactStatusReq) error
	UpdateContactsStatus(ctx context.Context, req *maildto.UpdateContactsStatusReq) error

	CreateContact(ctx context.Context, req *maildto.SaveContactReq) (id uint64, err error)

	UpdateContact(ctx context.Context, req *maildto.SaveContactReq) (err error)

	DeleteContacts(ctx context.Context, req *maildto.DeleteContactsReq) (deleted int64, err error)

	TagContacts(ctx context.Context, req *maildto.TagContactsReq) (changed int, err error)

	ListContactTags(ctx context.Context) (tags []string, err error)

	SaveCampaign(ctx context.Context, req *maildto.SaveCampaignReq) (*maildto.CampaignItem, error)
	ListCampaigns(ctx context.Context, req *maildto.CampaignListReq) (*maildto.CampaignListResp, error)
	GetCampaign(ctx context.Context, id uint64) (*maildto.CampaignItem, error)
	DeleteCampaign(ctx context.Context, id uint64) error
	DeleteCampaigns(ctx context.Context, req *maildto.DeleteIDsReq) (deleted int64, err error)
	StartCampaign(ctx context.Context, req *maildto.StartCampaignReq) (*maildto.StartCampaignResp, error)

	CampaignReport(ctx context.Context, campaignID uint64, page, pageSize int) (*maildto.CampaignReport, error)

	SaveAutomation(ctx context.Context, req *maildto.SaveAutomationReq) (*maildto.AutomationItem, error)
	ListAutomations(ctx context.Context, req *maildto.AutomationListReq) (*maildto.AutomationListResp, error)
	GetAutomation(ctx context.Context, id uint64) (*maildto.AutomationItem, error)
	SetAutomationStatus(ctx context.Context, req *maildto.SetAutomationStatusReq) error

	SaveAutomationLayout(ctx context.Context, req *maildto.SaveAutomationLayoutReq) error
	DeleteAutomation(ctx context.Context, id uint64) error
	DeleteAutomations(ctx context.Context, req *maildto.DeleteIDsReq) (deleted int64, err error)

	StartRun(ctx context.Context, automationID, contactID uint64, triggerEvent string) (bool, error)

	RunAutomation(ctx context.Context, runID uint64) error

	ListAutomationRuns(ctx context.Context, req *maildto.AutomationRunListReq) (*maildto.AutomationRunListResp, error)
	AutomationRunDetail(ctx context.Context, runID uint64) (*maildto.AutomationRunDetailResp, error)

	EnqueueDueRuns(ctx context.Context, limit int) (int, error)

	SendTemplate(ctx context.Context, req *maildto.SendTemplateReq) (*maildto.SendResult, error)

	SignTrackToken(p maildto.TrackPayload) (string, error)
	ParseTrackToken(token string) (maildto.TrackPayload, error)
	InjectTracking(html string, p maildto.TrackPayload) (string, error)
	RecordTrackEvent(ctx context.Context, p maildto.TrackPayload, eventType, ip, ua string)
	UnsubscribeByToken(ctx context.Context, token, ip, ua string) (string, error)
}

type MailQueryReader interface {
	ListContacts(ctx context.Context, req *maildto.ContactFilterReq) (res *maildto.ContactListResp, err error)

	ListCampaigns(ctx context.Context, req *maildto.CampaignListReq) (res *maildto.CampaignListResp, err error)

	CampaignReport(ctx context.Context, campaignID uint64, page, pageSize int) (res *maildto.CampaignReport, err error)

	ListTemplates(ctx context.Context, key string) (res []*maildto.TemplateItem, err error)

	ListAccounts(ctx context.Context, purpose string) (res []*maildto.AccountItem, err error)
}

type CampaignWriter interface {
	SaveCampaign(ctx context.Context, req *maildto.SaveCampaignReq) (res *maildto.CampaignItem, err error)

	StartCampaign(ctx context.Context, req *maildto.StartCampaignReq) (res *maildto.StartCampaignResp, err error)

	DeleteCampaign(ctx context.Context, id uint64) (err error)
}

type ContactWriter interface {
	CreateContact(ctx context.Context, req *maildto.SaveContactReq) (id uint64, err error)

	UpdateContact(ctx context.Context, req *maildto.SaveContactReq) (err error)

	UpdateContactStatus(ctx context.Context, req *maildto.UpdateContactStatusReq) (err error)

	DeleteContacts(ctx context.Context, req *maildto.DeleteContactsReq) (deleted int64, err error)

	TagContacts(ctx context.Context, req *maildto.TagContactsReq) (changed int, err error)
}

type TransactionalSender interface {
	SendTransactional(ctx context.Context, in *SendInput) (*SendOutcome, error)
}

type SendInput struct {
	TemplateKey string

	Locale string

	To string

	Vars map[string]any
}

type SendOutcome struct {
	Queued bool

	Suppressed bool
}

type TrackingService interface {
	ParseTrackToken(token string) (maildto.TrackPayload, error)
	InjectTracking(html string, p maildto.TrackPayload) (string, error)
	RecordTrackEvent(ctx context.Context, p maildto.TrackPayload, eventType, ip, ua string)
	UnsubscribeByToken(ctx context.Context, token, ip, ua string) (string, error)
}
