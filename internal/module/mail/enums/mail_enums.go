package mailenums

import (
	"net/http"

	"go_wp/pkg/facing"
)

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
	ErrImportCsvBad             = "mailCsvBad"
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

	ErrBatchReject = "mailBatchReject"

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

// errStatuses 是业务错误的 HTTP 语义声明：缺失 404、冲突 409、参数 400、内部 500。
// 未登记的 key 按参数错误处理（与共享出口的默认档一致）。
var errStatuses = map[string]int{
	ErrAccountNotFound:  http.StatusNotFound,
	ErrTemplateNotFound: http.StatusNotFound,
	ErrContactNotFound:  http.StatusNotFound,
	ErrCampaignNotFound: http.StatusNotFound,
	ErrAutomationNotFound: http.StatusNotFound,
	ErrAutomationRunNotFound: http.StatusNotFound,

	ErrAccountDisabled:        http.StatusForbidden,
	ErrContactEmailExists:     http.StatusConflict,
	ErrCampaignNotDraft:       http.StatusConflict,
	ErrCampaignNoRecipient:    http.StatusConflict,
	ErrCampaignSending:        http.StatusConflict,
	ErrAutomationRunExists:    http.StatusConflict,
	ErrSuppressed:             http.StatusConflict,

	ErrImportTooLarge: http.StatusRequestEntityTooLarge,

	ErrCipherSecretMissing: http.StatusInternalServerError,
	ErrCipherUnavailable:   http.StatusInternalServerError,
	ErrInternal:            http.StatusInternalServerError,
}

// StatusFor 返回业务错误声明的 HTTP 状态；未登记按参数错误处理。
func StatusFor(key string) int {
	if status, ok := errStatuses[key]; ok {
		return status
	}
	return http.StatusBadRequest
}

// Err 只声明 key 与状态码；文案由词库统一取词。
func Err(key string) error { return facing.WithStatus(facing.New(key), StatusFor(key)) }
