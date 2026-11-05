package mailenums

import "go_wp/pkg/facing"

const (
	MsgSendSuccess       = "mail.msg.sendSuccess"
	MsgSaveSuccess       = "mail.msg.saveSuccess"
	MsgDeleteSuccess     = "mail.msg.deleteSuccess"
	MsgImportSuccess     = "mail.msg.importSuccess"
	MsgTestSent          = "mail.msg.testSent"
	MsgCampaignStarted   = "mail.msg.campaignStarted"
	MsgAutomationStarted = "mail.msg.automationStarted"
)

const (
	ErrInvalidParam       = "mail.err.invalidParam"
	ErrAccountNotFound    = "mail.err.accountNotFound"
	ErrTemplateNotFound   = "mail.err.templateNotFound"
	ErrContactNotFound    = "mail.err.contactNotFound"
	ErrCampaignNotFound   = "mail.err.campaignNotFound"
	ErrAccountDisabled    = "mail.err.accountDisabled"
	ErrAccountIncomplete  = "mail.err.accountIncomplete"
	ErrSuppressed         = "mail.err.suppressed"
	ErrEmailRequired      = "mail.err.emailRequired"
	ErrEmailInvalid       = "mail.err.emailInvalid"
	ErrContactEmailExists = "mail.err.contactEmailExists"
	ErrContactTagEmpty    = "mail.err.contactTagEmpty"

	ErrConsentSourceRequired    = "mail.err.consentSourceRequired"
	ErrImportEmpty              = "mail.err.importEmpty"
	ErrImportTooLarge           = "mail.err.importTooLarge"
	ErrCampaignNotDraft         = "mail.err.campaignNotDraft"
	ErrCampaignNoRecipient      = "mail.err.campaignNoRecipient"
	ErrCipherSecretMissing      = "mail.err.cipherSecretMissing"
	ErrCipherUnavailable        = "mail.err.cipherUnavailable"
	ErrTemplateSyntax           = "mail.err.templateSyntax"
	ErrCampaignSending          = "mail.err.campaignSending"
	ErrAutomationNotFound       = "mail.err.automationNotFound"
	ErrAutomationTriggerInvalid = "mail.err.automationTriggerInvalid"
	ErrAutomationGraphInvalid   = "mail.err.automationGraphInvalid"
	ErrAutomationRunExists      = "mail.err.automationRunExists"
	ErrAutomationRunNotFound    = "mail.err.automationRunNotFound"

	ErrTrackLinkInvalid       = "mail.err.trackLinkInvalid"
	ErrUnsubscribeLinkInvalid = "mail.err.unsubscribeLinkInvalid"
	MsgUnsubscribeDoneTitle   = "mail.msg.unsubscribeDoneTitle"
	MsgUnsubscribeDoneBody    = "mail.msg.unsubscribeDoneBody"
	MsgUnsubscribeDoneNote    = "mail.msg.unsubscribeDoneNote"

	ErrInternal = "mail.err.internal"
)

const (
	NoticeBulkAllDone    = "mail.bulk.allDone"
	NoticeBulkAllSkipped = "mail.bulk.allSkipped"
	NoticeBulkPartial    = "mail.bulk.partial"

	NoticeAutomationTick = "mail.msg.automationTick"

	NoticeTemplateListFailed = "mail.page.templateListFailed"

	NoticeContactStatusBad = "mail.page.contactStatusBad"

	NoticeCampaignIDRequired = "mail.page.campaignIDRequired"
	NoticeRunIDRequired      = "mail.page.runIDRequired"
)

const (
	TestMailSubject = "mail.test.mailSubject"
	TestMailText    = "mail.test.mailText"
)

func Err(key string) error { return facing.New(key) }
