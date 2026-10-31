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

	"go_wp/internal/adminui"
	"go_wp/internal/builder/core"
	mailcontract "go_wp/internal/module/mail/contract"
	maildto "go_wp/internal/module/mail/dto"
	mailenums "go_wp/internal/module/mail/enums"
	mailmodel "go_wp/internal/module/mail/model"
	"go_wp/internal/templates"
	bulkids "go_wp/pkg/bulkids"
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
	id := reqparams.ParseUint(c.PostForm("id"))
	status := strings.TrimSpace(c.PostForm("status"))
	if err := h.mail.SetAutomationStatus(c.Request.Context(), &maildto.SetAutomationStatusReq{ID: id, Status: status}); err != nil {
		mailAutomationJump(c, false, mailErrMsg(c, err))
		return
	}
	mailAutomationJump(c, true, mailMsg{Fallback: mailAutomationStatusNotice(c, status)})
}

func (h *mailPageHandle) MailAutomationDelete(c *gin.Context) {
	if err := h.mail.DeleteAutomation(c.Request.Context(), reqparams.ParseUint(c.PostForm("id"))); err != nil {
		mailAutomationJump(c, false, mailErrMsg(c, err))
		return
	}
	mailAutomationJump(c, true, mailAutomationDeletedMsg)
}

func (h *mailPageHandle) MailAutomationsBulkDelete(c *gin.Context) {

	ids, berr := bulkids.Normalize(c.PostFormArray("ids"))
	if berr != nil {
		mailAutomationJump(c, false, mailBulkIDsMsg(c, berr))
		return
	}
	deleted, skipped := 0, 0
	for _, raw := range ids {
		id := reqparams.ParseUint(raw)
		if id == 0 {
			skipped++
			continue
		}
		if err := h.mail.DeleteAutomation(c.Request.Context(), id); err != nil {
			skipped++
			continue
		}
		deleted++
	}
	done, warn := mailBulkOutcome("删除", "自动化流程", deleted, skipped)
	mailBulkJump(c, done, warn, mailAutomationBack(c), mailAutomationBackMsg)
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
	n, err := h.mail.EnqueueDueRuns(c.Request.Context(), 500)
	if err != nil {
		mailAutomationRunsJump(c, false, mailErrMsg(c, err))
		return
	}

	mailAutomationRunsJump(c, true, mailAutomationTickMsg.WithArgs(strconv.Itoa(n)))
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
	req := &maildto.SaveCampaignReq{
		ID:         reqparams.ParseUint(c.PostForm("id")),
		Name:       c.PostForm("name"),
		AccountID:  reqparams.ParseUint(c.PostForm("account_id")),
		TemplateID: reqparams.ParseUint(c.PostForm("template_id")),
		Subject:    c.PostForm("subject"),
	}
	if tags := strings.TrimSpace(c.PostForm("target_tags")); tags != "" {
		for _, t := range strings.Split(tags, ",") {
			if v := strings.TrimSpace(t); v != "" {
				req.TargetTags = append(req.TargetTags, v)
			}
		}
	}
	if _, err := h.mail.SaveCampaign(c.Request.Context(), req); err != nil {
		mailCampaignsJump(c, false, mailErrMsg(c, err))
		return
	}
	mailCampaignsJump(c, true, mailDoneMsg)
}

func (h *mailPageHandle) MailCampaignStart(c *gin.Context) {
	res, err := h.mail.StartCampaign(c.Request.Context(), &maildto.StartCampaignReq{
		CampaignID: reqparams.ParseUint(c.PostForm("id")),
	})
	if err != nil {
		mailCampaignsJump(c, false, mailErrMsg(c, err))
		return
	}

	mailCampaignsJump(c, true, mailCampaignStartedMsg.WithArgs(strconv.FormatInt(res.Total, 10)))
}

func (h *mailPageHandle) MailCampaignDelete(c *gin.Context) {
	if err := h.mail.DeleteCampaign(c.Request.Context(), reqparams.ParseUint(c.PostForm("id"))); err != nil {
		mailCampaignsJump(c, false, mailErrMsg(c, err))
		return
	}
	mailCampaignsJump(c, true, mailDoneMsg)
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
		adminui.PageErrorBadRequest(c, "mail_campaigns", err)
		return
	}
	response.Success(c, report)
}

func (h *mailPageHandle) MailCampaignsBulkDelete(c *gin.Context) {

	ids, berr := bulkids.Normalize(c.PostFormArray("ids"))
	if berr != nil {
		mailCampaignsJump(c, false, mailBulkIDsMsg(c, berr))
		return
	}
	deleted, skipped := 0, 0
	for _, raw := range ids {
		id := reqparams.ParseUint(raw)
		if id == 0 {
			skipped++
			continue
		}
		if err := h.mail.DeleteCampaign(c.Request.Context(), id); err != nil {
			skipped++
			continue
		}
		deleted++
	}
	done, warn := mailBulkOutcome("删除", "群发活动", deleted, skipped)
	mailBulkJump(c, done, warn, mailCampaignsBack(c), mailCampaignsBackMsg)
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
	req := &maildto.ImportContactsReq{
		Content:         []byte(c.PostForm("content")),
		ConsentDeclared: c.PostForm("consent_declared") == "on",
		ConsentSource:   c.PostForm("consent_source"),
		UpdateExisting:  c.PostForm("update_existing") == "on",
	}
	if tags := strings.TrimSpace(c.PostForm("tags")); tags != "" {
		for _, t := range strings.Split(tags, ",") {
			if v := strings.TrimSpace(t); v != "" {
				req.DefaultTags = append(req.DefaultTags, v)
			}
		}
	}
	res, err := h.mail.ImportContacts(c.Request.Context(), req)
	if err != nil {
		mailContactsJump(c, false, mailErrMsg(c, err))
		return
	}

	mailContactsJump(c, true, mailContactImportMsg.WithArgs(
		strconv.Itoa(res.Imported), strconv.Itoa(res.Updated), strconv.Itoa(res.Skipped),
		strconv.Itoa(res.Suppressed), strconv.Itoa(len(res.Errors))))
}

func (h *mailPageHandle) MailContactStatus(c *gin.Context) {
	req := &maildto.UpdateContactStatusReq{
		ID:     reqparams.ParseUint(c.PostForm("id")),
		Status: c.PostForm("status"),
		Note:   c.PostForm("note"),
	}
	if err := h.mail.UpdateContactStatus(c.Request.Context(), req); err != nil {
		mailContactsJump(c, false, mailErrMsg(c, err))
		return
	}
	mailContactsJump(c, true, mailDoneMsg)
}

func (h *mailPageHandle) MailContactsBulkStatus(c *gin.Context) {
	target := strings.TrimSpace(c.PostForm("status"))
	switch target {
	case "subscribed", "pending", "unsubscribed":
	default:
		mailContactsJump(c, false, mailContactStatusBadMsg)
		return
	}

	ids, berr := bulkids.Normalize(c.PostFormArray("ids"))
	if berr != nil {
		mailContactsJump(c, false, mailBulkIDsMsg(c, berr))
		return
	}
	changed, skipped := 0, 0
	for _, raw := range ids {
		id := reqparams.ParseUint(raw)
		if id == 0 {
			skipped++
			continue
		}
		if err := h.mail.UpdateContactStatus(c.Request.Context(), &maildto.UpdateContactStatusReq{
			ID: id, Status: target,
		}); err != nil {
			skipped++
			continue
		}
		changed++
	}
	done, warn := mailBulkOutcome("更新", "联系人", changed, skipped)
	mailBulkJump(c, done, warn, mailContactsBack(c), mailContactsBackMsg)
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

func parseFormIDs(raw []string) (ids []uint64, invalid int) {
	ids = make([]uint64, 0, len(raw))
	for _, s := range raw {
		if v := reqparams.ParseUint(s); v > 0 {
			ids = append(ids, v)
			continue
		}
		invalid++
	}
	return ids, invalid
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
	req := &maildto.SaveAccountReq{
		ID:          reqparams.ParseUint(c.PostForm("id")),
		Name:        c.PostForm("name"),
		Purpose:     c.PostForm("purpose"),
		FromName:    c.PostForm("from_name"),
		FromEmail:   c.PostForm("from_email"),
		ReplyTo:     c.PostForm("reply_to"),
		Provider:    c.PostForm("provider"),
		Host:        c.PostForm("host"),
		Port:        int(reqparams.ParseUint(c.PostForm("port"))),
		Username:    c.PostForm("username"),
		Password:    c.PostForm("password"),
		Encryption:  c.PostForm("encryption"),
		RatePerHour: int(reqparams.ParseUint(c.PostForm("rate_per_hour"))),
	}
	var err error
	if req.ID > 0 {
		_, err = h.mail.UpdateAccount(c.Request.Context(), req)
	} else {
		_, err = h.mail.CreateAccount(c.Request.Context(), req)
	}
	if err != nil {
		mailAccountsJump(c, false, mailErrMsg(c, err))
		return
	}
	mailAccountsJump(c, true, mailDoneMsg)
}

func (h *mailPageHandle) MailAccountDelete(c *gin.Context) {
	if err := h.mail.DeleteAccount(c.Request.Context(), reqparams.ParseUint(c.PostForm("id"))); err != nil {
		mailAccountsJump(c, false, mailErrMsg(c, err))
		return
	}
	mailAccountsJump(c, true, mailDoneMsg)
}

func (h *mailPageHandle) MailAccountDefault(c *gin.Context) {
	if err := h.mail.SetDefaultAccount(c.Request.Context(), reqparams.ParseUint(c.PostForm("id"))); err != nil {
		mailAccountsJump(c, false, mailErrMsg(c, err))
		return
	}
	mailAccountsJump(c, true, mailDoneMsg)
}

func (h *mailPageHandle) MailAccountTest(c *gin.Context) {
	res, err := h.mail.TestSend(c.Request.Context(), &maildto.TestSendReq{
		AccountID: reqparams.ParseUint(c.PostForm("id")),
		ToEmail:   c.PostForm("to_email"),
		Lang:      response.RequestLanguage(c),
	})
	if err != nil {
		mailAccountsJump(c, false, mailErrMsg(c, err))
		return
	}
	if !res.OK {

		mailAccountsJump(c, false, mailTestSendFailedMsg(c, res.ErrorKind, res.Error))
		return
	}
	mailAccountsJump(c, true, mailDoneMsg)
}

func (h *mailPageHandle) MailTemplateSave(c *gin.Context) {
	req := &maildto.SaveTemplateReq{
		TemplateKey: c.PostForm("template_key"),
		Locale:      c.PostForm("locale"),
		Name:        c.PostForm("name"),
		Subject:     c.PostForm("subject"),
		BodyHTML:    c.PostForm("body_html"),
		BodyText:    c.PostForm("body_text"),
	}
	if vars := strings.TrimSpace(c.PostForm("variables")); vars != "" {
		for _, v := range strings.Split(vars, ",") {
			if s := strings.TrimSpace(v); s != "" {
				req.Variables = append(req.Variables, s)
			}
		}
	}
	if _, err := h.mail.UpsertTemplate(c.Request.Context(), req); err != nil {
		mailTemplatesJump(c, false, mailErrMsg(c, err))
		return
	}
	mailTemplatesJump(c, true, mailDoneMsg)
}

func (h *mailPageHandle) MailTemplateDelete(c *gin.Context) {
	if err := h.mail.DeleteTemplate(c.Request.Context(), c.PostForm("template_key"), c.PostForm("locale")); err != nil {
		mailTemplatesJump(c, false, mailErrMsg(c, err))
		return
	}
	mailTemplatesJump(c, true, mailDoneMsg)
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

	ids, berr := bulkids.Normalize(c.PostFormArray("ids"))
	if berr != nil {
		mailAccountsJump(c, false, mailBulkIDsMsg(c, berr))
		return
	}
	deleted, skipped := 0, 0
	for _, raw := range ids {
		id := reqparams.ParseUint(raw)
		if id == 0 {
			skipped++
			continue
		}
		if err := h.mail.DeleteAccount(c.Request.Context(), id); err != nil {
			skipped++
			continue
		}
		deleted++
	}
	done, warn := mailBulkOutcome("删除", "发信账号", deleted, skipped)
	mailBulkJump(c, done, warn, mailAccountsPath, mailAccountsBackMsg)
}

func (h *mailPageHandle) MailTemplatesBulkDelete(c *gin.Context) {
	ctx := c.Request.Context()
	list, err := h.mail.ListTemplates(ctx, "")
	if err != nil {
		mailTemplatesJump(c, false, mailTemplateListFailedMsg)
		return
	}
	type templateRef struct{ key, locale string }
	byID := make(map[uint64]templateRef, len(list))
	for _, t := range list {
		if t != nil {
			byID[t.ID] = templateRef{key: t.TemplateKey, locale: t.Locale}
		}
	}

	ids, berr := bulkids.Normalize(c.PostFormArray("ids"))
	if berr != nil {
		mailTemplatesJump(c, false, mailBulkIDsMsg(c, berr))
		return
	}
	deleted, skipped := 0, 0
	for _, raw := range ids {
		ref, ok := byID[reqparams.ParseUint(raw)]
		if !ok {
			skipped++
			continue
		}
		if err := h.mail.DeleteTemplate(ctx, ref.key, ref.locale); err != nil {
			skipped++
			continue
		}
		deleted++
	}
	done, warn := mailBulkOutcome("删除", "邮件模板", deleted, skipped)
	mailBulkJump(c, done, warn, mailTemplatesPath, mailTemplatesBackMsg)
}

func mailBulkOutcome(verb, noun string, done, skipped int) (doneMsg, warnMsg mailMsg) {
	switch {
	case done == 0 && skipped == 0:
		return mailMsg{}, mailMsg{}
	case skipped == 0:
		return mailBulkAllDoneMsg.WithArgs(verb, strconv.Itoa(done), noun), mailMsg{}
	case done == 0:
		return mailMsg{}, mailBulkAllSkippedMsg.WithArgs(noun, verb, strconv.Itoa(skipped))
	default:
		return mailMsg{}, mailBulkPartialMsg.WithArgs(verb, strconv.Itoa(done), noun, strconv.Itoa(skipped))
	}
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
