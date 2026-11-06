package mailhttp

import (
	"context"
	"fmt"
	json "go_wp/pkg/jsonx"
	"html"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	ginbinding "github.com/gin-gonic/gin/binding"

	mailcontract "go_wp/internal/module/mail/contract"
	maildto "go_wp/internal/module/mail/dto"
	mailenums "go_wp/internal/module/mail/enums"
	mailmodel "go_wp/internal/module/mail/model"
	"go_wp/internal/templates"
	"go_wp/pkg/builder/core"
	"go_wp/pkg/facing"
	"go_wp/pkg/pagination"
	"go_wp/pkg/reqparams"
	"go_wp/pkg/response"
	"go_wp/pkg/utils"
	pageview "go_wp/pkg/view"
)

func (h *mailPageHandle) MailAutomationCanvas(c *gin.Context) {
	item, ok := h.automationForCanvas(c)
	if !ok {
		return
	}
	pageview.RenderTemplate(c, AutomationCanvasTemplate, h.automationCanvasView(c, item))
}

func automationJsVer() string {
	root := filepath.Join("internal", "templates", "static", "js", "automation")
	var latest int64
	_ = filepath.Walk(root, func(_ string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if fi.IsDir() || !strings.HasSuffix(fi.Name(), ".js") {
			return nil
		}
		if m := fi.ModTime().Unix(); m > latest {
			latest = m
		}
		return nil
	})
	if latest == 0 {
		return "0"
	}
	return strconv.FormatInt(latest, 10)
}

const maxAutomationNodes = 12

const mailAutomationPageSize = 50

type mailTr func(key, fallback string) string

func mailTranslate(c *gin.Context) mailTr {
	return templates.TranslateFunc(response.RequestLanguage(c))
}

func mailLabel(tr mailTr, pair mailenums.LabelPair) string {
	if strings.TrimSpace(pair.Key) == "" {
		return pair.Fallback
	}
	return tr(pair.Key, pair.Fallback)
}

type nodeTypeOption struct {
	Value string
	Label string
	Hint  string
}

func (h *mailPageHandle) MailAutomationPage(c *gin.Context) {
	ctx := c.Request.Context()
	tr := mailTranslate(c)
	page := int(reqparams.ParseUint(c.Query("page")))
	if page <= 0 {
		page = 1
	}
	automations, err := h.mail.ListAutomations(ctx, &maildto.AutomationListReq{Page: page, PageSize: mailAutomationPageSize})
	view := &AutomationListPageResp{
		Title:       mailLabel(tr, mailenums.PageTitleAutomation),
		Page:        page,
		ListQuery:   mailPageQuery(page),
		Automations: []AutomationRowResp{},
		BackPath:    c.Request.URL.RequestURI(),
	}
	if err != nil {

		view.Err = mailErrPageText(c, err)
		pageview.RenderTemplate(c, AutomationPageTemplate, view)
		return
	}

	autoRows := make([]AutomationRowResp, 0, len(automations.Items))
	for _, a := range automations.Items {
		autoRows = append(autoRows, AutomationRowResp{
			ID: a.ID, Name: a.Name, Description: a.Description,
			Status: a.Status, Version: a.Version,
			TriggerLabel: triggerLabelOf(tr, a.TriggerType),
		})
	}
	view.Automations = autoRows
	view.AutoTotal = automations.Total
	pageview.RenderTemplate(c, AutomationPageTemplate, view)
}

type automationFormValues struct {
	ID          uint64
	IsNew       bool
	Name        string
	Description string
	Trigger     string
	Entry       string
}

func triggerLabelOf(tr mailTr, value string) string {
	for _, o := range mailenums.AutomationTriggers {
		if o.Value == value {
			return mailLabel(tr, o.Label)
		}
	}
	return value
}

func triggerOptions(tr mailTr, selected string) []OptionResp {
	if selected == "" {
		selected = "manual"
	}
	opts := make([]OptionResp, 0, len(mailenums.AutomationTriggers))
	for _, o := range mailenums.AutomationTriggers {
		opts = append(opts, OptionResp{Value: o.Value, Label: mailLabel(tr, o.Label), Selected: o.Value == selected})
	}
	return opts
}

func (h *mailPageHandle) MailAutomationStatus(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var form maildto.MailAutomationStatusForm
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, mailErrScene, mailenums.Err(mailenums.ErrInvalidParam))
		return
	}
	status := strings.TrimSpace(form.Status)
	if err := h.mail.SetAutomationStatus(c.Request.Context(), &maildto.SetAutomationStatusReq{
		ID: mailIDOfText(form.ID), Status: status,
	}); err != nil {
		pageview.FailScene(c, mailErrScene, err)
		return
	}
	pageview.Success(c)
}

func (h *mailPageHandle) MailAutomationDelete(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var form maildto.MailIDForm
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, mailErrScene, mailenums.Err(mailenums.ErrInvalidParam))
		return
	}
	if err := h.mail.DeleteAutomation(c.Request.Context(), mailIDOfText(form.ID)); err != nil {
		pageview.FailScene(c, mailErrScene, err)
		return
	}
	pageview.Success(c)
}

func (h *mailPageHandle) MailAutomationsBulkDelete(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var form maildto.MailIDsForm
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, mailErrScene, mailenums.Err(mailenums.ErrInvalidParam))
		return
	}
	ids, ok := mailBatchIDsOrFail(c, form.IDs)
	if !ok {
		return
	}
	if _, err := h.mail.DeleteAutomations(c.Request.Context(), &maildto.DeleteIDsReq{IDs: ids}); err != nil {
		pageview.FailScene(c, mailErrScene, err)
		return
	}
	pageview.Success(c)
}

func (h *mailPageHandle) MailAutomationRunDetail(c *gin.Context) {
	id, hasID := mailQueryID(c)
	if !hasID {

		mailAutomationRunsJump(c, false, mailRunIDRequiredMsg)
		return
	}
	detail, err := h.mail.AutomationRunDetail(c.Request.Context(), id)
	if err != nil {
		mailAutomationRunsJump(c, false, mailErrMsg(c, err))
		return
	}

	mailRunDetailTexts(mailTranslate(c), detail)
	pageview.RenderTemplate(c, AutomationRunPageTemplate, &AutomationRunPageResp{
		Title: mailenums.PageTitleAutomationRun.Key,
		D:     detail,
	})
}

func (h *mailPageHandle) MailAutomationTick(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var form maildto.MailAutomationTickForm
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, mailErrScene, mailenums.Err(mailenums.ErrInvalidParam))
		return
	}
	if _, err := h.mail.EnqueueDueRuns(c.Request.Context(), 500); err != nil {
		pageview.FailScene(c, mailErrScene, err)
		return
	}
	pageview.Success(c)
}

func splitFormList(raw string) []string {
	raw = strings.ReplaceAll(raw, "，", ",")
	raw = strings.ReplaceAll(raw, "、", ",")
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func strOf(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func strSlice(v any) []string {
	switch arr := v.(type) {
	case []string:
		return arr
	case []any:
		out := make([]string, 0, len(arr))
		for _, it := range arr {
			if s, ok := it.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

const mailAutomationRunsPageSize = 50

func (h *mailPageHandle) MailAutomationRunsPage(c *gin.Context) {
	ctx := c.Request.Context()
	tr := mailTranslate(c)
	page := mailPageNumber(c.Query("page"))
	automationID, runStatus := c.Query("automationId"), c.Query("runStatus")

	runs, err := h.mail.ListAutomationRuns(ctx, &maildto.AutomationRunListReq{
		AutomationID: reqparams.ParseUint(automationID),
		Status:       runStatus,
		Page:         page,
		PageSize:     mailAutomationRunsPageSize,
	})
	view := &AutomationRunsPageResp{
		Title:     mailLabel(tr, mailenums.PageTitleAutomationRuns),
		Page:      page,
		FilterID:  automationID,
		FilterRun: runStatus,

		RunsQuery: mailAutomationRunsListQuery(automationID, runStatus, page),

		Runs: []any{},
	}
	if err != nil {

		view.Err = mailErrPageText(c, err)
		pageview.RenderTemplate(c, AutomationRunsPageTemplate, view)
		return
	}

	mailRunTexts(tr, runs.Items)
	view.Runs = toAnyRows(runs.Items)
	view.RunTotal = runs.Total
	view.CountRunning = runs.Counts["running"]
	view.CountWaiting = runs.Counts["waiting"]
	view.CountCompleted = runs.Counts["completed"]
	view.CountFailed = runs.Counts["failed"]
	view.CountStopped = runs.Counts["stopped"]

	base := reqparams.FilterBaseURL("/admin/mail/automation/runs", map[string]string{
		"automationId": automationID,
		"runStatus":    runStatus,
	})

	view.Pagination = pagination.Page{
		Total: runs.Total, Page: page, Limit: mailAutomationRunsPageSize, Base: base,
	}
	pageview.RenderTemplate(c, AutomationRunsPageTemplate, view)
}

const mailCampaignsPageSize = 50

func mailCampaignsClampPage(page int, total int64) int {
	if page < 1 {
		return 1
	}
	if total <= 0 {
		return 1
	}
	maxPage := int((total + mailCampaignsPageSize - 1) / mailCampaignsPageSize)
	if maxPage < 1 {
		maxPage = 1
	}
	if page > maxPage {
		return maxPage
	}
	return page
}

func (h *mailPageHandle) MailCampaignsPage(c *gin.Context) {
	ctx := c.Request.Context()
	tr := mailTranslate(c)
	page := mailPageNumber(c.Query("page"))

	campaigns, err := h.mail.ListCampaigns(ctx, &maildto.CampaignListReq{Page: page, PageSize: mailCampaignsPageSize})
	if err != nil {
		pageview.RenderTemplate(c, CampaignsPageTemplate, mailCampaignsErrData(c, err))
		return
	}
	if fixed := mailCampaignsClampPage(page, campaigns.Total); fixed != page {
		page = fixed
		if campaigns, err = h.mail.ListCampaigns(ctx, &maildto.CampaignListReq{Page: page, PageSize: mailCampaignsPageSize}); err != nil {
			pageview.RenderTemplate(c, CampaignsPageTemplate, mailCampaignsErrData(c, err))
			return
		}
	}

	accounts, _ := h.mail.ListAccounts(ctx, "")
	templates, _ := h.mail.ListTemplates(ctx, "")

	view := &CampaignsPageResp{
		Title:         mailLabel(tr, mailenums.PageTitleCampaigns),
		Campaigns:     toAnySlice(campaigns.Items),
		CampaignTotal: campaigns.Total,
		Accounts:      toAnySlice(accounts),
		Templates:     toAnySlice(templates),
		Page:          page,

		ListQuery: mailPageQuery(page),
	}
	view.Pagination = pagination.Page{
		Total: campaigns.Total, Page: page, Limit: mailCampaignsPageSize, Base: "/admin/mail/campaigns",
	}
	pageview.RenderTemplate(c, CampaignsPageTemplate, view)
}

func mailCampaignsErrData(c *gin.Context, err error) *CampaignsPageResp {
	return &CampaignsPageResp{
		Title:     mailenums.PageTitleCampaigns.Key,
		Err:       mailErrPageText(c, err),
		Campaigns: []any{},
		Accounts:  []any{},
		Templates: []any{},
		Page:      1,
	}
}

func (h *mailPageHandle) MailCampaignSave(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var form maildto.MailCampaignSaveForm
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, mailErrScene, mailenums.Err(mailenums.ErrInvalidParam))
		return
	}
	req := &maildto.SaveCampaignReq{
		ID:         mailIDOfText(form.ID),
		Name:       form.Name,
		AccountID:  mailIDOfText(form.AccountID),
		TemplateID: mailIDOfText(form.TemplateID),
		Subject:    form.Subject,
		TargetTags: splitTagInput(form.TargetTags),
	}
	if _, err := h.mail.SaveCampaign(c.Request.Context(), req); err != nil {
		pageview.FailScene(c, mailErrScene, err)
		return
	}
	pageview.Success(c)
}

func (h *mailPageHandle) MailCampaignStart(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var form maildto.MailCampaignStartForm
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, mailErrScene, mailenums.Err(mailenums.ErrInvalidParam))
		return
	}
	if _, err := h.mail.StartCampaign(c.Request.Context(), &maildto.StartCampaignReq{
		CampaignID: mailIDOfText(form.ID),
	}); err != nil {
		pageview.FailScene(c, mailErrScene, err)
		return
	}
	pageview.Success(c)
}

func (h *mailPageHandle) MailCampaignDelete(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var form maildto.MailIDForm
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, mailErrScene, mailenums.Err(mailenums.ErrInvalidParam))
		return
	}
	if err := h.mail.DeleteCampaign(c.Request.Context(), mailIDOfText(form.ID)); err != nil {
		pageview.FailScene(c, mailErrScene, err)
		return
	}
	pageview.Success(c)
}

func (h *mailPageHandle) MailCampaignPage(c *gin.Context) {
	ctx := c.Request.Context()
	id, hasID := mailQueryID(c)
	if !hasID {

		mailJump(c, false, mailCampaignIDRequiredMsg, mailCampaignsPath, mailCampaignsBackMsg)
		return
	}
	page := mailPageNumber(c.Query("page"))
	report, err := h.mail.CampaignReport(ctx, id, page, mailCampaignsPageSize)
	if err != nil {

		mailJump(c, false, mailErrMsg(c, err), mailCampaignsPath, mailCampaignsBackMsg)
		return
	}

	if report.Total > 0 {
		if maxPage := int((report.Total + mailCampaignsPageSize - 1) / mailCampaignsPageSize); page > maxPage {
			page = maxPage
			if report, err = h.mail.CampaignReport(ctx, id, page, mailCampaignsPageSize); err != nil {
				mailJump(c, false, mailErrMsg(c, err), mailCampaignsPath, mailCampaignsBackMsg)
				return
			}
		}
	}
	view := &CampaignReportPageResp{
		Title: mailenums.PageTitleCampaignReport.Key,
		R:     report,
		Page:  page,
	}

	view.Pagination = pagination.Page{
		Total: report.Total, Page: page, Limit: mailCampaignsPageSize,
		Base: fmt.Sprintf("/admin/mail/campaign?id=%d", id),
	}
	pageview.RenderTemplate(c, CampaignPageTemplate, view)
}

func (h *mailPageHandle) MailCampaignReportJSON(c *gin.Context) {
	page := int(reqparams.ParseUint(c.Query("page")))
	if page <= 0 {
		page = 1
	}
	report, err := h.mail.CampaignReport(c.Request.Context(), reqparams.ParseUint(c.Query("id")), page, mailCampaignsPageSize)
	if err != nil {
		response.BadRequestForPage(c, "mail_campaigns", err)
		return
	}
	response.Success(c, report)
}

func (h *mailPageHandle) MailCampaignsBulkDelete(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var form maildto.MailIDsForm
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, mailErrScene, mailenums.Err(mailenums.ErrInvalidParam))
		return
	}
	ids, ok := mailBatchIDsOrFail(c, form.IDs)
	if !ok {
		return
	}
	if _, err := h.mail.DeleteCampaigns(c.Request.Context(), &maildto.DeleteIDsReq{IDs: ids}); err != nil {
		pageview.FailScene(c, mailErrScene, err)
		return
	}
	pageview.Success(c)
}

const mailContactsPageSize = 50

func mailContactsClampPage(page int, total int64) int {
	if page < 1 {
		return 1
	}
	if total <= 0 {

		return 1
	}
	maxPage := int((total + mailContactsPageSize - 1) / mailContactsPageSize)
	if maxPage < 1 {
		maxPage = 1
	}
	if page > maxPage {
		return maxPage
	}
	return page
}

func (h *mailPageHandle) MailContactsPage(c *gin.Context) {
	ctx := c.Request.Context()
	tr := mailTranslate(c)
	page := mailPageNumber(c.Query("page"))
	keyword, status := c.Query("keyword"), c.Query("status")
	tags := c.Query("tags")
	tagList := splitTagInput(tags)

	filter := func() (*maildto.ContactListResp, error) {
		return h.mail.ListContacts(ctx, &maildto.ContactFilterReq{
			Keyword:  keyword,
			Status:   status,
			Tags:     tagList,
			Page:     page,
			PageSize: mailContactsPageSize,
		})
	}
	contacts, err := filter()
	if err != nil {
		pageview.RenderTemplate(c, ContactsPageTemplate, mailContactsErrData(c, err))
		return
	}

	if fixed := mailContactsClampPage(page, contacts.Total); fixed != page {
		page = fixed
		if contacts, err = filter(); err != nil {
			pageview.RenderTemplate(c, ContactsPageTemplate, mailContactsErrData(c, err))
			return
		}
	}

	tagOptions, tagErr := h.mail.ListContactTags(ctx)
	if tagErr != nil {
		tagOptions = nil
	}

	view := &ContactsPageResp{
		Title:        mailLabel(tr, mailenums.PageTitleContacts),
		Contacts:     toAnyRows(contacts.Items),
		ContactTotal: contacts.Total,
		Page:         page,
		Keyword:      keyword,
		Status:       status,
		Tags:         tags,
		TagOptions:   toAnyRows(tagOptions),

		ListQuery: mailContactsListQuery(keyword, status, tags, page),
	}

	view.Pagination = mailContactsPagination(contacts.Total, page, keyword, status, tags)
	pageview.RenderTemplate(c, ContactsPageTemplate, view)
}

const mailContactsPath = "/admin/mail/contacts"

func mailContactsPagination(total int64, page int, keyword, status, tags string) pagination.Page {
	return pagination.Page{
		Total: total, Page: page, Limit: mailContactsPageSize,
		Base: reqparams.FilterBaseURL(mailContactsPath, map[string]string{
			"keyword": keyword, "status": status, "tags": tags,
		}),
	}
}

func mailContactsListQuery(keyword, status, tags string, page int) string {
	params := map[string]string{"keyword": keyword, "status": status, "tags": tags}
	if page > 1 {
		params["page"] = strconv.Itoa(page)
	}
	return mailListQuery(params)
}

func mailContactsErrData(c *gin.Context, err error) *ContactsPageResp {
	return &ContactsPageResp{
		Title:      mailenums.PageTitleContacts.Key,
		Err:        mailErrPageText(c, err),
		Contacts:   []any{},
		Page:       1,
		TagOptions: []any{},

		ListQuery: "",
	}
}

func (h *mailPageHandle) MailContactImport(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var form maildto.MailContactImportForm
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, mailErrScene, mailenums.Err(mailenums.ErrInvalidParam))
		return
	}
	req := &maildto.ImportContactsReq{
		Content:         []byte(form.Content),
		ConsentDeclared: form.ConsentDeclared == "1" || form.ConsentDeclared == "on" || form.ConsentDeclared == "true",
		ConsentSource:   form.ConsentSource,
		UpdateExisting:  form.UpdateExisting == "1" || form.UpdateExisting == "on" || form.UpdateExisting == "true",
		DefaultTags:     splitTagInput(form.Tags),
	}
	if _, err := h.mail.ImportContacts(c.Request.Context(), req); err != nil {
		pageview.FailScene(c, mailErrScene, err)
		return
	}
	pageview.Success(c)
}

func (h *mailPageHandle) MailContactStatus(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var form maildto.MailContactStatusForm
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, mailErrScene, mailenums.Err(mailenums.ErrInvalidParam))
		return
	}
	req := &maildto.UpdateContactStatusReq{
		ID:     mailIDOfText(form.ID),
		Status: form.Status,
		Note:   form.Note,
	}
	if err := h.mail.UpdateContactStatus(c.Request.Context(), req); err != nil {
		pageview.FailScene(c, mailErrScene, err)
		return
	}
	pageview.Success(c)
}

func (h *mailPageHandle) MailContactsBulkStatus(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var form maildto.MailContactBulkStatusForm
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, mailErrScene, mailenums.Err(mailenums.ErrInvalidParam))
		return
	}
	target := strings.TrimSpace(form.Status)
	switch target {
	case "subscribed", "pending", "unsubscribed":
	default:
		pageview.FailStatus(c, http.StatusBadRequest, mailErrScene, facing.New(mailenums.NoticeContactStatusBad))
		return
	}

	ids, ok := mailBatchIDsOrFail(c, form.IDs)
	if !ok {
		return
	}
	// 一次 service 调用：整批同一事务，任一条不存在或写库失败都整批回滚。
	if err := h.mail.UpdateContactsStatus(c.Request.Context(), &maildto.UpdateContactsStatusReq{
		IDs: ids, Status: target,
	}); err != nil {
		pageview.FailScene(c, mailErrScene, err)
		return
	}
	pageview.Success(c)
}

func splitTagInput(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == ';' || r == '|' || r == ',' })
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

type mailPageHandle struct {
	mail mailcontract.MailService
}

const mailListPageSize = 20

func mailPageNumber(raw string) int {
	page := int(reqparams.ParseUint(raw))
	if page < 1 {
		return 1
	}
	return page
}

func mailListPaginationFacts(total int64, page int, param, base string) pagination.Page {
	pg := pagination.Page{Total: total, Page: page, Limit: mailListPageSize}
	pg.Base = base
	pg.PageParam = param
	pg.DropLimit = true
	return pg
}

func NewMailPageHandle(mail mailcontract.MailService) *mailPageHandle {
	return &mailPageHandle{mail: mail}
}

func (h *mailPageHandle) MailPage(c *gin.Context) {
	ctx := c.Request.Context()

	accountRows, accountTotal, accountPage, err := h.mail.ListAccountsPage(ctx, "", mailPageNumber(c.Query("account_page")), mailListPageSize)
	if err != nil {
		pageview.RenderTemplate(c, AccountsPageTemplate, mailPageErrData(c, err))
		return
	}

	view := &AccountsPageResp{

		Title:    mailLabel(mailTranslate(c), mailenums.PageTitleAccounts),
		Accounts: toAnyRows(accountRows),
	}

	view.Pagination = mailListPaginationFacts(accountTotal, accountPage, "account_page", "/admin/mail")
	pageview.RenderTemplate(c, AccountsPageTemplate, view)
}

func mailPageErrData(c *gin.Context, err error) *AccountsPageResp {
	return &AccountsPageResp{
		Title:    mailLabel(mailTranslate(c), mailenums.PageTitleAccounts),
		Err:      mailErrPageText(c, err),
		Accounts: []any{},
	}
}

func (h *mailPageHandle) MailAccountSave(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var form maildto.MailAccountSaveForm
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, mailErrScene, mailenums.Err(mailenums.ErrInvalidParam))
		return
	}
	req := &maildto.SaveAccountReq{
		ID:          mailIDOfText(form.ID),
		Name:        form.Name,
		Purpose:     form.Purpose,
		FromName:    form.FromName,
		FromEmail:   form.FromEmail,
		ReplyTo:     form.ReplyTo,
		Provider:    form.Provider,
		Host:        form.Host,
		Port:        int(mailIDOfText(form.Port)),
		Username:    form.Username,
		Password:    form.Password,
		Encryption:  form.Encryption,
		RatePerHour: int(mailIDOfText(form.RatePerHour)),
	}
	var err error
	if req.ID > 0 {
		_, err = h.mail.UpdateAccount(c.Request.Context(), req)
	} else {
		_, err = h.mail.CreateAccount(c.Request.Context(), req)
	}
	if err != nil {
		pageview.FailScene(c, mailErrScene, err)
		return
	}
	pageview.Success(c)
}

func (h *mailPageHandle) MailAccountDelete(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var form maildto.MailIDForm
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, mailErrScene, mailenums.Err(mailenums.ErrInvalidParam))
		return
	}
	if err := h.mail.DeleteAccount(c.Request.Context(), mailIDOfText(form.ID)); err != nil {
		pageview.FailScene(c, mailErrScene, err)
		return
	}
	pageview.Success(c)
}

func (h *mailPageHandle) MailAccountDefault(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var form maildto.MailIDForm
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, mailErrScene, mailenums.Err(mailenums.ErrInvalidParam))
		return
	}
	if err := h.mail.SetDefaultAccount(c.Request.Context(), mailIDOfText(form.ID)); err != nil {
		pageview.FailScene(c, mailErrScene, err)
		return
	}
	pageview.Success(c)
}

func (h *mailPageHandle) MailAccountTest(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var form maildto.MailAccountTestForm
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, mailErrScene, mailenums.Err(mailenums.ErrInvalidParam))
		return
	}
	res, err := h.mail.TestSend(c.Request.Context(), &maildto.TestSendReq{
		AccountID: mailIDOfText(form.ID),
		ToEmail:   form.ToEmail,
		Lang:      response.RequestLanguage(c),
	})
	if err != nil {
		pageview.FailScene(c, mailErrScene, err)
		return
	}
	if !res.OK {
		if msg := mailTestSendFailedMsg(c, res.ErrorKind, res.Error); !msg.Empty() {
			pageview.FailStatus(c, http.StatusBadRequest, mailErrScene,
				facing.WithStatus(facing.NewMessage(msg.Key, msg.Fallback), http.StatusBadRequest))
		} else {
			pageview.FailStatus(c, http.StatusBadRequest, mailErrScene, mailenums.Err(mailenums.ErrInternal))
		}
		return
	}
	pageview.Success(c)
}

func (h *mailPageHandle) MailTemplateSave(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var form maildto.MailTemplateSaveForm
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, mailErrScene, mailenums.Err(mailenums.ErrInvalidParam))
		return
	}
	req := &maildto.SaveTemplateReq{
		TemplateKey: form.TemplateKey,
		Locale:      form.Locale,
		Name:        form.Name,
		Subject:     form.Subject,
		Variables:   splitTagInput(form.Variables),
		BodyHTML:    form.BodyHTML,
		BodyText:    form.BodyText,
	}
	if _, err := h.mail.UpsertTemplate(c.Request.Context(), req); err != nil {
		pageview.FailScene(c, mailErrScene, err)
		return
	}
	pageview.Success(c)
}

func (h *mailPageHandle) MailTemplateDelete(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var form maildto.MailTemplateDeleteForm
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, mailErrScene, mailenums.Err(mailenums.ErrInvalidParam))
		return
	}
	if err := h.mail.DeleteTemplate(c.Request.Context(), form.TemplateKey, form.Locale); err != nil {
		pageview.FailScene(c, mailErrScene, err)
		return
	}
	pageview.Success(c)
}

func (h *mailPageHandle) MailMarketingRedirect(c *gin.Context) {
	query := url.Values{}
	for _, key := range []string{"keyword", "status", "page"} {
		if value := strings.TrimSpace(c.Query(key)); value != "" {
			query.Set(key, value)
		}
	}
	target := "/admin/mail/contacts"
	if encoded := query.Encode(); encoded != "" {
		target += "?" + encoded
	}
	c.Redirect(http.StatusFound, target)
}

func (h *mailPageHandle) MailAccountsBulkDelete(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var form maildto.MailIDsForm
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, mailErrScene, mailenums.Err(mailenums.ErrInvalidParam))
		return
	}
	ids, ok := mailBatchIDsOrFail(c, form.IDs)
	if !ok {
		return
	}
	if _, err := h.mail.DeleteAccounts(c.Request.Context(), &maildto.DeleteIDsReq{IDs: ids}); err != nil {
		pageview.FailScene(c, mailErrScene, err)
		return
	}
	pageview.Success(c)
}

func (h *mailPageHandle) MailTemplatesBulkDelete(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var form maildto.MailIDsForm
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, mailErrScene, mailenums.Err(mailenums.ErrInvalidParam))
		return
	}
	ids, ok := mailBatchIDsOrFail(c, form.IDs)
	if !ok {
		return
	}
	if _, err := h.mail.DeleteTemplates(c.Request.Context(), &maildto.DeleteIDsReq{IDs: ids}); err != nil {
		pageview.FailScene(c, mailErrScene, err)
		return
	}
	pageview.Success(c)
}

func (h *mailPageHandle) MailTemplatesPage(c *gin.Context) {
	ctx := c.Request.Context()
	rows, total, page, err := h.mail.ListTemplatesPage(ctx, "", mailPageNumber(c.Query("page")), mailListPageSize)
	if err != nil {

		pageview.RenderTemplate(c, TemplatesPageTemplate, mailTemplatesErrData(c, err))
		return
	}
	view := &TemplatesPageResp{

		Title:         mailLabel(mailTranslate(c), mailenums.PageTitleTemplates),
		Templates:     toAnyRows(rows),
		TemplateTotal: total,
	}

	view.Pagination = mailListPaginationFacts(total, page, "page", "/admin/mail/templates")
	pageview.RenderTemplate(c, TemplatesPageTemplate, view)
}

func mailTemplatesErrData(c *gin.Context, err error) *TemplatesPageResp {
	return &TemplatesPageResp{
		Title:     mailLabel(mailTranslate(c), mailenums.PageTitleTemplates),
		Err:       mailErrPageText(c, err),
		Templates: []any{},
	}
}

const (
	mailTrackLinkInvalidText       = "链接无效或已过期"
	mailUnsubscribeLinkInvalidText = "退订链接无效或已过期"
	mailUnsubscribeDoneTitleText   = "已退订"
	mailUnsubscribeDoneBodyText    = "%s 不会再收到我们的营销邮件。"
	mailUnsubscribeDoneNoteText    = "事务类邮件（如密码重置、订单通知）不受影响。"
)

type trackingHandle struct {
	svc mailcontract.TrackingService
}

func NewTrackingHandle(svc mailcontract.TrackingService) *trackingHandle {
	return &trackingHandle{svc: svc}
}

var transparentGIF = []byte{
	0x47, 0x49, 0x46, 0x38, 0x39, 0x61, 0x01, 0x00, 0x01, 0x00, 0x80, 0x00, 0x00,
	0xff, 0xff, 0xff, 0x00, 0x00, 0x00, 0x21, 0xf9, 0x04, 0x01, 0x00, 0x00, 0x00,
	0x00, 0x2c, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x02, 0x02,
	0x44, 0x01, 0x00, 0x3b,
}

func (h *trackingHandle) Open(c *gin.Context) {
	token := strings.TrimSuffix(c.Param("token"), ".gif")
	if p, err := h.svc.ParseTrackToken(token); err == nil {
		h.svc.RecordTrackEvent(c.Request.Context(), p, mailmodel.EventTypeOpen, c.ClientIP(), c.Request.UserAgent())
	}
	c.Header("Cache-Control", "no-store, no-cache, must-revalidate, private")
	c.Data(http.StatusOK, "image/gif", transparentGIF)
}

func (h *trackingHandle) Click(c *gin.Context) {
	p, err := h.svc.ParseTrackToken(c.Param("token"))
	if err != nil || strings.TrimSpace(p.URL) == "" {

		mailVisitorLog(c, err, "邮件点击链路处理失败")
		c.String(http.StatusBadRequest,
			mailVisitorText(c, mailenums.ErrTrackLinkInvalid, mailTrackLinkInvalidText))
		return
	}
	h.svc.RecordTrackEvent(c.Request.Context(), p, mailmodel.EventTypeClick, c.ClientIP(), c.Request.UserAgent())
	c.Redirect(http.StatusFound, p.URL)
}

func (h *trackingHandle) Unsubscribe(c *gin.Context) {
	email, err := h.svc.UnsubscribeByToken(c.Request.Context(), c.Param("token"), c.ClientIP(), c.Request.UserAgent())
	if err != nil {

		mailVisitorLogError(c, err, "邮件退订链路处理失败")
		c.String(http.StatusBadRequest,
			mailVisitorText(c, mailenums.ErrUnsubscribeLinkInvalid, mailUnsubscribeLinkInvalidText))
		return
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, unsubscribeDonePage(c, email))
}

func unsubscribeDonePage(c *gin.Context, email string) string {
	lang := response.RequestLanguage(c)
	title := html.EscapeString(mailVisitorText(c, mailenums.MsgUnsubscribeDoneTitle, mailUnsubscribeDoneTitleText))
	body := strings.ReplaceAll(
		html.EscapeString(mailVisitorText(c, mailenums.MsgUnsubscribeDoneBody, mailUnsubscribeDoneBodyText)),
		"%s", html.EscapeString(email))
	note := html.EscapeString(mailVisitorText(c, mailenums.MsgUnsubscribeDoneNote, mailUnsubscribeDoneNoteText))

	return "<!doctype html><html lang=\"" + html.EscapeString(lang) + "\"><head><meta charset=\"utf-8\">" +
		"<meta name=\"viewport\" content=\"width=device-width,initial-scale=1\">" +
		"<title>" + title + "</title></head><body style=\"font-family:system-ui,sans-serif;max-width:520px;margin:80px auto;padding:0 20px;line-height:1.7\">" +
		"<h2>" + title + "</h2><p>" + body + "</p>" +
		"<p>" + note + "</p>" +
		"</body></html>"
}

const automationEntryKeyDefault = "n1"

const automationEndKeyBase = "end"

type automationStep struct {
	Index int

	Key string

	Type string

	Param string

	Tag string

	Unit  string
	Value string

	Yes string
	No  string
}

type automationStepAction struct {
	Kind string
	Step int
}

func mailRunTexts(tr mailTr, items []maildto.AutomationRunItem) {
	for i := range items {
		items[i].ErrorMessage = mailenums.FormatRunText(tr, items[i].ErrorMessage)
	}
}

func mailRunDetailTexts(tr mailTr, d *maildto.AutomationRunDetailResp) {
	if d == nil {
		return
	}
	d.Explain = mailenums.FormatRunText(tr, d.Explain)
	d.Run.ErrorMessage = mailenums.FormatRunText(tr, d.Run.ErrorMessage)
	for i := range d.Timeline {
		d.Timeline[i].Detail = mailenums.FormatRunText(tr, d.Timeline[i].Detail)
	}
}

func toAnySlice[T any](in []T) []any {
	out := make([]any, 0, len(in))
	for i := range in {
		out = append(out, in[i])
	}
	return out
}

func toAnyRows[T any](in []T) []any {
	out := make([]any, 0, len(in))
	for i := range in {
		out = append(out, in[i])
	}
	return out
}

func (h *mailPageHandle) automationCanvasView(c *gin.Context, item *maildto.AutomationItem) *AutomationCanvasPageResp {

	nodesJSON, merr := json.Marshal(item.Nodes)
	if merr != nil {
		nodesJSON = []byte("[]")
	}
	metaJSON, _ := json.Marshal(h.automationCanvasMeta(c, item))
	return &AutomationCanvasPageResp{
		Title:        mailenums.PageTitleAutomationCanvas.Key,
		A:            item,
		NodesJSON:    utils.JsonSafe(string(nodesJSON)),
		MetaJSON:     utils.JsonSafe(string(metaJSON)),
		JSVer:        automationJsVer(),
		KeyframesCSS: core.KeyframeCSS([]string{"sky-fade-in-up"}),
		Triggers:     triggerOptions(mailTranslate(c), item.TriggerType),
		NodeTypes:    mapListOf(paletteFromMeta(h.automationCanvasMeta(c, item)["nodeTypes"])),
		IsNew:        item.ID == 0,

		FrequencyEveryTime: item.TriggerFrequency != mailmodel.TriggerFrequencyOnce,
		FrequencyOnce:      item.TriggerFrequency == mailmodel.TriggerFrequencyOnce,
		ExitOnReply:        item.ExitOnReply,
	}
}

func (h *mailPageHandle) automationForCanvas(c *gin.Context) (*maildto.AutomationItem, bool) {
	id, hasID := mailQueryID(c)
	if !hasID {
		return newAutomationDraft(), true
	}
	item, err := h.mail.GetAutomation(c.Request.Context(), id)
	if err != nil {
		mailAutomationJump(c, false, mailErrMsg(c, err))
		return nil, false
	}
	return item, true
}

func newAutomationDraft() *maildto.AutomationItem {
	return &maildto.AutomationItem{
		Name:        "",
		TriggerType: mailmodel.TriggerManual,
		Status:      mailmodel.AutomationStatusDraft,
		Version:     0,
		Entry:       automationEntryKeyDefault,
		Nodes: []maildto.AutomationNodeItem{
			{Key: automationEntryKeyDefault, Type: "trigger"},
		},
	}
}

func (h *mailPageHandle) automationCanvasMeta(c *gin.Context, item *maildto.AutomationItem) map[string]any {
	tr := mailTranslate(c)

	emailDisabled, emailReason := h.emailNodeDisabled(c)
	nodeTypes := make([]gin.H, 0, len(mailenums.AutomationNodeTypes))
	for _, o := range mailenums.AutomationNodeTypes {
		entry := gin.H{
			"value":   o.Value,
			"label":   mailLabel(tr, o.Label),
			"hint":    mailLabel(tr, o.Hint),
			"outlets": mailenums.AutomationNodeTypeOutlets(o.Value),
		}
		if o.Value == "email" && emailDisabled {
			entry["disabled"] = true
			entry["disabledReason"] = emailReason
		}
		nodeTypes = append(nodeTypes, entry)
	}
	return map[string]any{
		"automationId":     item.ID,
		"name":             item.Name,
		"description":      item.Description,
		"triggerType":      item.TriggerType,
		"triggerParams":    item.TriggerParams,
		"triggerFrequency": item.TriggerFrequency,
		"exitOnReply":      item.ExitOnReply,
		"entry":            item.Entry,
		"status":           item.Status,
		"version":          item.Version,
		"isNew":            item.ID == 0,

		"structureEditable": item.Status != mailmodel.AutomationStatusActive,
		"nodeTypes":         nodeTypes,
	}
}

func paletteFromMeta(raw any) []gin.H {
	list, _ := raw.([]gin.H)
	out := make([]gin.H, 0, len(list))
	for _, t := range list {
		out = append(out, gin.H{
			"Value":          t["value"],
			"Hint":           t["hint"],
			"Label":          t["label"],
			"Disabled":       t["disabled"] == true,
			"DisabledReason": t["disabledReason"],
		})
	}
	return out
}

func (h *mailPageHandle) emailNodeDisabled(c *gin.Context) (bool, string) {
	accounts, err := h.mail.ListAccounts(c.Request.Context(), "marketing")
	if err != nil || len(accounts) > 0 {
		return false, ""
	}
	return true, mailLabel(mailTranslate(c), mailenums.AutomationCanvasEmailDisabled)
}

func (h *mailPageHandle) MailTemplateEditor(c *gin.Context) {
	item := h.templateForEditor(c.Request.Context(),
		strings.TrimSpace(c.Query("templateKey")), strings.TrimSpace(c.Query("locale")))
	metaJSON, _ := json.Marshal(map[string]any{
		"tree":         templateTreeOf(item),
		"compiledHtml": item.BodyCompiledHTML,
		"compiledText": item.BodyCompiledText,
	})
	pageview.RenderTemplate(c, TemplateEditorPageTemplate, &TemplateEditorPageResp{
		Title:    mailenums.PageTitleTemplateEditor.Key,
		T:        item,
		MetaJSON: utils.JsonSafe(string(metaJSON)),
		JSVer:    templateEditorJsVer(),
	})
}

func (h *mailPageHandle) templateForEditor(ctx context.Context, key, locale string) *maildto.TemplateItem {
	if key == "" {
		return &maildto.TemplateItem{}
	}
	item, err := h.mail.GetTemplate(ctx, key, locale)
	if err != nil {
		return &maildto.TemplateItem{TemplateKey: key, Locale: locale}
	}
	return item
}

func templateTreeOf(item *maildto.TemplateItem) map[string]any {
	if item != nil && len(item.BodyJSON) > 0 {
		var tree map[string]any
		if err := json.Unmarshal(item.BodyJSON, &tree); err == nil && tree != nil {
			return tree
		}
	}
	return map[string]any{
		"schema": 1,
		"root":   map[string]any{"key": "root", "type": "body", "children": []any{}},
	}
}

func templateEditorJsVer() string {
	root := filepath.Join("internal", "templates", "static", "js", "template")
	var latest int64
	_ = filepath.Walk(root, func(_ string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil || fi.IsDir() || !strings.HasSuffix(fi.Name(), ".js") {
			return nil
		}
		if m := fi.ModTime().Unix(); m > latest {
			latest = m
		}
		return nil
	})
	if latest == 0 {
		return "0"
	}
	return strconv.FormatInt(latest, 10)
}

func mapListOf(in []gin.H) []map[string]any {
	out := make([]map[string]any, 0, len(in))
	for _, h := range in {
		out = append(out, map[string]any(h))
	}
	return out
}
